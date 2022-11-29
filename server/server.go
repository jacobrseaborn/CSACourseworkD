package main

import (
	"flag"
	"fmt"
	"math"
	"math/rand"
	"net"
	"net/rpc"
	"os"
	"sync"
	"time"
	"uk.ac.bris.cs/gameoflife/stubs"
)

// GoL
var world func(x, y int) uint8
var returnWorld [][]uint8
var dim int
var turn int

var pause *sync.WaitGroup
var paused bool = false

func deepCopy(w [][]uint8) [][]uint8 {
	l := len(w)

	iW := make([][]uint8, l)
	for i := 0; i < l; i++ {
		iW[i] = make([]uint8, len(w[i]))
	}

	for y := 0; y < l; y++ {
		for x := 0; x < len(w[0]); x++ {
			iW[y][x] = w[y][x]
		}
	}
	return iW
}

func makeImmutableWorld(w [][]uint8) func(x, y int) uint8 {
	iW := deepCopy(w)

	return func(x, y int) uint8 {
		return iW[y][x]
	}
}

func getOutboundIP() string {
	conn, _ := net.Dial("udp", "8.8.8.8:80")
	defer conn.Close()
	localAddr := conn.LocalAddr().(*net.UDPAddr).IP.String()
	return localAddr
}

func golLogic(world func(x, y int) uint8, worldSlice [][]uint8, height, width int, sY, eY, sX, eX int) {

	for y := sY; y < eY; y++ {
		for x := sX; x < eX; x++ {
			pause.Wait()

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
					worldSlice[y-sY][x] = 255
				} else {
					worldSlice[y-sY][x] = 0
					//fmt.Println("new world ", x, y, " flipped to dead. Turn:", turn)
				}

			} else { // this cell is dead
				if sum == 3 {
					worldSlice[y-sY][x] = 255
					//fmt.Println("new world ", x, y, " flipped to alive. Turn:", turn)
				} else {
					worldSlice[y-sY][x] = 0
				}

			}
		}
	}
}

// RPC things

type ServerOperation struct{}

func (s *ServerOperation) KillServer(req stubs.ResetRequest, res *stubs.StatusReport) (err error) {
	fmt.Println("Server Killed!")
	os.Exit(0)
	return
}

func (s *ServerOperation) GameOfLife(req stubs.Job, res *stubs.Response) (err error) {
	pause = new(sync.WaitGroup)
	immutableWorld := makeImmutableWorld(req.World)
	//returnWorld = req.World
	world = immutableWorld
	dim = len(req.World)

	h := req.E - req.S
	w := dim

	worldSlice := make([][]uint8, h)
	for i := 0; i < h; i++ {
		worldSlice[i] = make([]uint8, w)
	}

	golLogic(world, worldSlice, dim, dim, req.S, req.E, 0, w)
	//world = makeImmutableWorld(worldSlice)
	//returnWorld = deepCopy(worldSlice)

	res.World = deepCopy(worldSlice)
	res.Turn = 0 // TODO
	return
}

func (s *ServerOperation) Pause(req stubs.PauseRequest, res *stubs.PausedCallback) (err error) {
	if req.NewState && !paused {
		fmt.Println("Pausing Server")
		pause.Add(1)
		paused = true
	} else if !req.NewState && paused {
		fmt.Println("Unpaused Server")
		pause.Done()
		paused = false
	}

	res.Paused = req.NewState
	return
}

func main() {
	pAddr := flag.String("port", "8080", "Port to listen on")
	flag.Parse()
	rand.Seed(time.Now().UnixNano())
	err := rpc.Register(&ServerOperation{})
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	listener, err := net.Listen("tcp", ":"+*pAddr)

	if err != nil {
		fmt.Println(err.Error())
		return
	}

	client, _ := rpc.Dial("tcp", "127.0.0.1:8030")

	request := stubs.Subscription{
		WorkerAddress: getOutboundIP() + ":" + *pAddr,
		Callback:      "ServerOperation.GameOfLife",
	}
	response := new(stubs.StatusReport)

	fmt.Println(request.WorkerAddress)

	err = client.Call(stubs.Subscribe, request, response)
	if err != nil {
		fmt.Println(err.Error())
		return
	}

	defer func(listener net.Listener) {
		err := listener.Close()
		if err != nil {
			fmt.Println(err.Error())
			return
		}
	}(listener)
	rpc.Accept(listener)
}
