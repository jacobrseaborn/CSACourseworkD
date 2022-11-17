package gol

import (
	"bufio"
	"fmt"
	"net"
	"net/rpc"
	"strconv"
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

func handleEvent(conn *net.Conn, e chan<- Event) {
	fmt.Println("handling connection")
	reader := bufio.NewReader(*conn)
	for {
		eventStr, _ := reader.ReadString('\n')
		var event [4]int
		fmt.Sscanf(eventStr, "%d,%d,%d,%d", &event[0], &event[1], &event[2], &event[3])

		switch event[0] {
		case 0: // AliveCellsCount
			e <- AliveCellsCount{
				CompletedTurns: event[1],
				CellsCount:     event[2],
			}
		case 1: // CellFlipped
			e <- CellFlipped{
				CompletedTurns: event[1],
				Cell: util.Cell{
					X: event[2],
					Y: event[3],
				},
			}
		case 2: // TurnComplete
			e <- TurnComplete{
				CompletedTurns: event[1],
			}
		default:
			return
		}

	}
}

// distributor divides the work between workers and interacts with other goroutines.
func distributor(p Params, c distributorChannels) {
	// TODO: Create a 2D slice to store the world.

	listener, err := net.Listen("tcp", ":8080")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer listener.Close()
	addr := listener.Addr().String()

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

	server := "127.0.0.1:8030"
	client, _ := rpc.Dial("tcp", server)
	defer client.Close()

	request := stubs.Request{
		World:   world,
		Dim:     p.ImageWidth,
		Turns:   p.Turns,
		Threads: p.Threads,
		Address: addr,
	}
	response := new(stubs.Response)

	fmt.Println("connection made")

	go func() {
		fmt.Println("waiting to accept")
		eventConn, _ := listener.Accept()
		fmt.Println("accepted")
		handleEvent(&eventConn, c.events)
	}()

	client.Call(stubs.GoL, request, response)

	sharedWorld := response.World

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
	c.ioFilename <- strconv.Itoa(p.ImageWidth) + "x" + strconv.Itoa(p.ImageHeight) + "x" + strconv.Itoa(p.Turns)

	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			c.ioOutput <- sharedWorld[y][x]
		}
	}

	// Make sure that the Io has finished any output before exiting.
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle

	c.events <- StateChange{p.Turns, Quitting}
	// Close the channel to stop the SDL goroutine gracefully. Removing may cause deadlock.
	close(c.events)
}
