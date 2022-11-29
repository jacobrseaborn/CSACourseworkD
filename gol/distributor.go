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

func deepCopy(w [][]uint8) [][]uint8 {
	l := len(w)

	iW := make([][]uint8, l)
	for i := 0; i < l; i++ {
		iW[i] = make([]uint8, l)
	}

	for y := 0; y < l; y++ {
		for x := 0; x < l; x++ {
			iW[y][x] = w[y][x]
		}
	}
	return iW
}

// distributor divides the work between workers and interacts with other goroutines.
func distributor(p Params, c distributorChannels, keyPresses <-chan rune) {
	// TODO: Create a 2D slice to store the world.

	c.ioCommand <- ioInput
	c.ioFilename <- strconv.Itoa(p.ImageWidth) + "x" + strconv.Itoa(p.ImageHeight)

	world := make([][]uint8, p.ImageHeight)
	for i := 0; i < p.ImageHeight; i++ {
		world[i] = make([]uint8, p.ImageWidth)
		for j := 0; j < p.ImageWidth; j++ {
			world[i][j] = <-c.ioInput
		}
	}

	c.ioCommand <- ioCheckIdle
	<-c.ioIdle

	// TODO: Start workers and piece together GoL

	broker := "127.0.0.1:8030"
	client, _ := rpc.Dial("tcp", broker)
	defer func(client *rpc.Client) {
		err := client.Close()
		if err != nil {
			fmt.Println(err.Error())
			os.Exit(0)
		}
	}(client)

	request := stubs.PublishRequest{
		World:   world,
		Turns:   p.Turns,
		Threads: p.Threads,
	}
	response := new(stubs.Response)

	done := make(chan *rpc.Call, 100)
	running := true

	ticker := time.NewTicker(2 * time.Second)
	paused := false
	closing := false

	client.Go(stubs.Publish, request, response, done)
	for running {
		select {
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
		case key := <-keyPresses:
			switch key {
			case 'p':
				callback := new(stubs.PausedCallback)
				client.Call(stubs.Pause, stubs.PauseRequest{Paused: paused}, callback)
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
			case 'k', 'q':
				fmt.Println("Key pressed: Quit and shutdown nodes")
				client.Call(stubs.KillServer, stubs.KillCallback{}, new(stubs.KillCallback))
				if key == 'k' {
					closing = true
				}
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
		case <-done:
			running = false
		}
	}

	ticker.Stop()
	res := new(stubs.Response)
	client.Call(stubs.RetrieveWorld, stubs.Request{}, res)
	sharedWorld := res.World

	if closing {
		client.Call(stubs.CloseServer, stubs.Request{}, new(stubs.Response))
	}

	// TODO: Report the final state using FinalTurnCompleteEvent.

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
		CompletedTurns: p.Turns,
		Alive:          alive,
	}

	c.ioCommand <- ioOutput
	filename := strconv.Itoa(p.ImageWidth) + "x" + strconv.Itoa(p.ImageHeight) + "x" + strconv.Itoa(p.Turns)
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
		CompletedTurns: res.Turn,
		Filename:       filename,
	}

	c.events <- StateChange{p.Turns, Quitting}
	// Close the channel to stop the SDL goroutine gracefully. Removing may cause deadlock.
	close(c.events)
}
