package gol

import (
	"fmt"
	"net/rpc"
	"os"
	"strconv"
	"time"
	"uk.ac.bris.cs/gameoflife/stubs"
	"uk.ac.bris.cs/gameoflife/util"
)

type distributorChannels struct {
	events     chan<- Event
	ioCommand  chan<- ioCommand
	ioIdle     <-chan bool
	ioFilename chan<- string
	ioOutput   chan<- uint8
	ioInput    <-chan uint8
}

// distributor divides the work between workers and interacts with other goroutines.
func distributor(p Params, c distributorChannels, keyPresses <-chan rune) {
	//Create a 2D slice to store the world.

	c.ioCommand <- ioInput
	c.ioFilename <- strconv.Itoa(p.ImageWidth) + "x" + strconv.Itoa(p.ImageHeight)

	// read from file to create initial world
	world := make([][]uint8, p.ImageHeight)
	for i := 0; i < p.ImageHeight; i++ {
		world[i] = make([]uint8, p.ImageWidth)
		for j := 0; j < p.ImageWidth; j++ {
			world[i][j] = <-c.ioInput
		}
	}

	// ensure input is finished before continuing
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle

	// dial broker
	broker := "127.0.0.1:8030"
	client, _ := rpc.Dial("tcp", broker)
	defer func(client *rpc.Client) {
		err := client.Close()
		if err != nil {
			fmt.Println(err.Error())
			os.Exit(0)
		}
	}(client)

	// formulate request for broker (initial world, number of turns, number of workers/threads)
	request := stubs.PublishRequest{
		World:   world,
		Turns:   p.Turns,
		Threads: p.Threads,
	}
	response := new(stubs.Response)

	// since non-blocking rpc call. We need a done channel
	done := make(chan *rpc.Call, 100)
	running := true
	closing := false

	// ticker ticks every 2 seconds to prompt AliveCells event
	ticker := time.NewTicker(2 * time.Second)
	paused := false

	// sharedWorld and turns complete for sending the final state of the board
	var sharedWorld [][]uint8
	turnsComplete := 0

	// make non-blocking rpc to broker, publishing work.
	client.Go(stubs.Publish, request, response, done)
	for running {
		select {
		// ticker ticks
		case <-ticker.C:
			if !paused {
				aliveCells := new(stubs.AliveCellsCount)
				err := client.Call(stubs.GetAlive, new(stubs.AliveCellsCountRequest), aliveCells)

				if err != nil {
					fmt.Println(err.Error())
					return
				}

				c.events <- AliveCellsCount{
					CellsCount:     aliveCells.Count,
					CompletedTurns: aliveCells.Turn,
				}
			}
		// a key is pressed
		case key := <-keyPresses:
			switch key {
			// 'p' pressed. Pause processing
			case 'p':
				callback := new(stubs.PausedCallback)
				err := client.Call(stubs.PauseBroker, stubs.PauseRequest{NewState: !paused}, callback)
				if err != nil {
					fmt.Println(err.Error())
					return
				}
				paused = callback.Paused

				if paused {
					c.events <- StateChange{
						CompletedTurns: callback.Turn,
						NewState:       Paused,
					}
				} else {
					fmt.Println("Continuing")
					c.events <- StateChange{
						CompletedTurns: callback.Turn,
						NewState:       Executing,
					}
				}
			// 'k' or 'q' pressed. Shutdown different components of the system depending.
			case 'k', 'q':
				fmt.Println("Key pressed: Quit and shutdown nodes.", key)
				ticker.Stop()

				res := new(stubs.Response)
				err := client.Call(stubs.RetrieveWorld, stubs.Request{}, res)
				if err != nil {
					fmt.Println(err.Error())
					return
				}
				sharedWorld = res.World
				turnsComplete = res.Turn

				fmt.Println("about to kill")
				if key == 'k' {
					closing = true
				}

				response := new(stubs.StatusReport)
				err = client.Call(stubs.Reset, stubs.ResetRequest{Kill: key == 'k'}, response)
				if err != nil {
					fmt.Println("panic: ", err.Error())
					return
				}
				running = false
			// 's' pressed. Save the current state of the board as a pgm.
			case 's':
				fmt.Println("Key pressed: Save pgm")

				res := new(stubs.Response)
				client.Call(stubs.RetrieveWorld, stubs.Request{}, res)

				world2write := res.World

				c.ioCommand <- ioOutput
				filename := strconv.Itoa(p.ImageWidth) + "x" + strconv.Itoa(p.ImageHeight) + "x" + strconv.Itoa(res.Turn)
				c.ioFilename <- filename

				for y := 0; y < p.ImageHeight; y++ {
					for x := 0; x < p.ImageWidth; x++ {
						c.ioOutput <- world2write[y][x]
					}
				}

				c.events <- ImageOutputComplete{
					CompletedTurns: res.Turn,
					Filename:       filename,
				}
			}
		// non-blocking rpc call is complete. Exit processing loop
		case <-done:
			ticker.Stop()

			res := new(stubs.Response)
			client.Call(stubs.RetrieveWorld, stubs.Request{}, res)
			sharedWorld = res.World
			turnsComplete = res.Turn

			client.Call(stubs.Reset, stubs.ResetRequest{Kill: false}, new(stubs.StatusReport))

			running = false
		}
	}

	// Report the final state using FinalTurnCompleteEvent.
	fmt.Println("Outside main loop")
	if closing {
		client.Call(stubs.KillBroker, stubs.Request{}, new(stubs.StatusReport))
	}

	// get a list of all the alive cells in final state
	var alive []util.Cell
	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			if sharedWorld[y][x] == 255 {
				alive = append(alive, util.Cell{X: x, Y: y})
			}
		}
	}

	// send all events
	c.events <- FinalTurnComplete{
		CompletedTurns: turnsComplete,
		Alive:          alive,
	}

	// output final result as pgm image
	c.ioCommand <- ioOutput
	filename := strconv.Itoa(p.ImageWidth) + "x" + strconv.Itoa(p.ImageHeight) + "x" + strconv.Itoa(turnsComplete)
	c.ioFilename <- filename

	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			c.ioOutput <- sharedWorld[y][x]
		}
	}

	// Make sure that the Io has finished any output before exiting.
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle
	c.events <- ImageOutputComplete{
		CompletedTurns: turnsComplete,
		Filename:       filename,
	}

	c.events <- StateChange{turnsComplete, Quitting}
	// Close the channel to stop the SDL goroutine gracefully. Removing may cause deadlock.
	close(c.events)
}
