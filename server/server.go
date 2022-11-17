package main

import (
	"flag"
	"fmt"
	"math"
	"math/rand"
	"net"
	"net/rpc"
	"sync"
	"time"
	"uk.ac.bris.cs/gameoflife/stubs"
)

// GoL

func makeImmutableWorld(w [][]uint8) func(x, y int) uint8 {
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

	return func(x, y int) uint8 {
		return iW[y][x]
	}
}

func getAlive(world func(x, y int) uint8, dim int) int {
	var total = 0
	for y := 0; y < dim; y++ {
		for x := 0; x < dim; x++ {
			if world(x, y) == 255 {
				total += 1
			}
		}
	}
	return total

}

func worker(world func(x, y int) uint8, sY, eY, sX, eX int, shared [][]uint8, dim int, wg *sync.WaitGroup) {
	golLogic(world, shared, dim, dim, sY, eY, sX, eX)
	wg.Done()
}

func golLogic(world func(x, y int) uint8, sharedWorld [][]uint8, height, width int, sY, eY, sX, eX int) {

	for y := sY; y < eY; y++ {
		for x := sX; x < eX; x++ {
			sum := world(int(math.Mod(float64(x+width-1), float64(width))), int(math.Mod(float64(y+height-1), float64(height))))/255 +
				world(int(math.Mod(float64(x+width), float64(width))), int(math.Mod(float64(y+height-1), float64(height))))/255 +
				world(int(math.Mod(float64(x+width+1), float64(width))), int(math.Mod(float64(y+height-1), float64(height))))/255 +
				world(int(math.Mod(float64(x+width-1), float64(width))), int(math.Mod(float64(y+height), float64(height))))/255 +
				world(int(math.Mod(float64(x+width+1), float64(width))), int(math.Mod(float64(y+height), float64(height))))/255 +
				world(int(math.Mod(float64(x+width-1), float64(width))), int(math.Mod(float64(y+height+1), float64(height))))/255 +
				world(int(math.Mod(float64(x+width), float64(width))), int(math.Mod(float64(y+height+1), float64(height))))/255 +
				world(int(math.Mod(float64(x+width+1), float64(width))), int(math.Mod(float64(y+height+1), float64(height))))/255

			if world(x, y) == 255 { // this cell is alive
				if sum == 2 || sum == 3 {
					sharedWorld[y][x] = 255
				} else {
					sharedWorld[y][x] = 0
					//fmt.Println("new world ", x, y, " flipped to dead. Turn:", turn)
				}

			} else { // this cell is dead
				if sum == 3 {
					sharedWorld[y][x] = 255
					//fmt.Println("new world ", x, y, " flipped to alive. Turn:", turn)
				} else {
					sharedWorld[y][x] = 0
				}

			}
		}
	}
}

// RPC things

type ServerOperation struct{}

func SendEvent(conn *net.Conn, e []int) {
	fmt.Println("event sent", e)
	eventStr := fmt.Sprintf("%d,%d,%d,%d", e[0], e[1], e[2], e[3])
	_, err := fmt.Fprintln(*conn, eventStr)
	if err != nil {
		fmt.Println(err)
		return
	}
}

func (s *ServerOperation) GameOfLife(req stubs.Request, res *stubs.Response) (err error) {
	fmt.Println("started engine")

	conn, _ := net.Dial("tcp", req.Address)

	immutableWorld := makeImmutableWorld(req.World)

	h, w := req.Dim, req.Dim
	turns := req.Turns
	threads := req.Threads

	sharedWorld := make([][]uint8, h)
	for i := 0; i < h; i++ {
		sharedWorld[i] = make([]uint8, w)
	}

	wg := &sync.WaitGroup{}

	exit := make(chan bool)
	ticker := time.NewTicker(2 * time.Second)
	completedTurns := 0

	go func() {
		for {

			select {
			case <-exit:
				return
			case <-ticker.C:
				count := getAlive(immutableWorld, req.Dim)
				turns := completedTurns + 1

				SendEvent(&conn, []int{0, turns, count, 0}) // send event AliveCellsCount with CompletedTurns: turns and CellCount: count
			}
		}
	}()

	for turn := 0; turn < turns; turn++ {
		wg.Add(threads)

		for w := 0; w < threads-1; w++ {
			go worker(immutableWorld, w*(h/threads), (w+1)*(h/threads), 0, req.Dim, sharedWorld, req.Dim, wg)
		}
		go worker(immutableWorld, (threads-1)*(h/threads), h, 0, w, sharedWorld, w, wg)

		// block here until done
		wg.Wait()
		immutableWorld = makeImmutableWorld(sharedWorld)
		completedTurns = turn
		SendEvent(&conn, []int{2, turn + 1, 0, 0})
	}

	if turns == 0 {
		sharedWorld = req.World
	}

	fmt.Println("finished engine")
	ticker.Stop()
	exit <- true

	SendEvent(&conn, []int{3, 0, 0, 0})
	conn.Close()

	res.World = sharedWorld
	return
}

func main() {
	pAddr := flag.String("port", "8030", "Port to listen on")
	flag.Parse()
	rand.Seed(time.Now().UnixNano())
	rpc.Register(&ServerOperation{})
	listener, err := net.Listen("tcp", ":"+*pAddr)

	if err != nil {
		fmt.Println(err)
		return
	}

	defer listener.Close()
	rpc.Accept(listener)
}
