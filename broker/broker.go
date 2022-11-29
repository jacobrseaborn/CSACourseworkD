package main

import (
	"flag"
	"fmt"
	"net"
	"net/rpc"
	"os"
	"sync"
	"uk.ac.bris.cs/gameoflife/stubs"
)

var workers [16]chan stubs.Job
var clients [16]*rpc.Client
var numWorkers = 0

var world, newWorld [][]uint8
var turn = 0

var wg = new(sync.WaitGroup)
var workMutex = new(sync.Mutex)

var running = true
var workerCount = new(sync.WaitGroup)

var interrupt = false
var closing = false

var paused = false
var pausedMutex = new(sync.Mutex)
var serverPauseWg = new(sync.WaitGroup)

// ========================================= HELPER FUNCTIONS ==========================================

func deepCopy(w [][]uint8) [][]uint8 {
	l := len(w)

	c := make([][]uint8, l)
	for i := 0; i < l; i++ {
		c[i] = make([]uint8, l)
	}

	for y := 0; y < l; y++ {
		for x := 0; x < l; x++ {
			c[y][x] = w[y][x]
		}
	}
	return c
}

func getAlive(world [][]uint8, dim int) int {
	var total = 0
	for y := 0; y < dim; y++ {
		for x := 0; x < dim; x++ {
			if world[y][x] == 255 {
				total += 1
			}
		}
	}
	return total

}
func (b *Broker) GetAlive(req *stubs.AliveCellsCountRequest, res *stubs.AliveCellsCount) (err error) {
	res.Turn = turn
	res.Count = getAlive(world, len(world))
	return
}

func (b *Broker) RetrieveWorld(nil, res *stubs.Response) (err error) {
	res.World = deepCopy(world)
	res.Turn = turn

	return
}

func (b *Broker) KillBroker(req stubs.Request, res *stubs.StatusReport) (err error) {
	defer os.Exit(0)
	return
}

func reset(kill bool) (err error) {
	fmt.Println("Resetting!")

	if paused {
		pause(false)
		fmt.Println("unpaused all paused servers")
	}

	fmt.Println("passed pause")

	for w := 0; w < numWorkers; w++ {
		for len(workers[w]) > 0 {
			<-workers[w]
		}
		fmt.Println("Emptied work for worker", w)

		if kill {
			workers[w] <- stubs.Job{} // add work to flush subscriber_loop through
		}
	}

	pausedMutex = new(sync.Mutex)
	paused = false

	turn = 0
	interrupt = true
	if kill {
		running = false
	}

	if kill {
		workerCount.Wait()
		fmt.Println("All servers shutdown. Returning publish!")

		for !closing {

		}
		fmt.Println("Publish returned. Exiting broker")
	}

	fmt.Println("Work remaining (0,1,2): ", len(workers[0]), len(workers[1]), len(workers[2]))
	fmt.Println("AliveCellCount: ", getAlive(world, len(world)), turn)

	return
}

func pause(pausing bool) (err error) {
	serverPauseWg.Wait()
	serverPauseWg.Add(numWorkers)

	if !paused && pausing {
		pausedMutex.Lock()
		paused = true
	} else if paused && !pausing {
		pausedMutex.Unlock()
		paused = false
	}

	//serverPauseWg.Add(numWorkers)
	for w := 0; w < numWorkers; w++ {
		err := clients[w].Call(stubs.PauseServer, stubs.PauseRequest{NewState: pausing}, new(stubs.PausedCallback))
		if err != nil {
			fmt.Println("Server had issue pausing.", err.Error())
		}
		serverPauseWg.Done()
	}
	return err
}

// ========================================= SUBSCRIBE & PUBLISH ============================================

func subscribe(addr string, callback string) (err error) {
	client, _ := rpc.Dial("tcp", addr)
	work := make(chan stubs.Job, 100)
	workers[numWorkers] = work
	clients[numWorkers] = client
	numWorkers++
	workerCount.Add(1)
	go subscriber_loop(client, work, callback)

	return
}

func subscriber_loop(client *rpc.Client, work chan stubs.Job, callback string) {
	for running {
		job := <-work
		if job.World == nil {
			break
		}

		response := new(stubs.Response)
		err := client.Call(callback, job, response)
		if err != nil {
			fmt.Println("Error: ", err.Error())
			work <- job

			break
		}

		// return
		updateWorld(response.World, job.S)
		wg.Done()
	}
	client.Call(stubs.KillServer, stubs.ResetRequest{Kill: true}, new(stubs.StatusReport))
	workerCount.Done()
}

func updateWorld(w [][]uint8, startY int) {

	for y := 0; y < len(w); y++ {
		for x := 0; x < len(w[y]); x++ {
			newWorld[y+startY][x] = w[y][x]
		}
	}
}

func publish(w [][]uint8, threads int, turns int) (err error) {
	reset(false)
	wg.Wait()
	interrupt = false

	world = w
	newWorld = deepCopy(world)

	if threads > numWorkers {
		threads = numWorkers
	}

	fmt.Println("AliveCellCount: ", getAlive(world, len(world)), turn)
	for t := 0; t < turns && !interrupt; t++ {
		pausedMutex.Lock()
		pausedMutex.Unlock()
		wg.Add(threads)

		for worker := 0; worker < threads-1; worker++ {
			workers[worker] <- stubs.Job{
				World: world,
				S:     (len(world) / threads) * worker,
				E:     (len(world) / threads) * (worker + 1),
			}
		}
		workers[threads-1] <- stubs.Job{
			World: world,
			S:     (len(world) / threads) * (threads - 1),
			E:     len(world),
		}

		wg.Wait()
		turn++
		world = deepCopy(newWorld)

	}

	return
}

type Broker struct{}

func (b *Broker) Subscribe(req stubs.Subscription, res *stubs.StatusReport) (err error) {
	err = subscribe(req.WorkerAddress, req.Callback)
	if err != nil {
		fmt.Println("Error: ", err)
		return err
	}
	return err
}

func (b *Broker) Publish(req stubs.PublishRequest, res *stubs.Response) (err error) {
	err = publish(req.World, req.Threads, req.Turns)
	if err != nil {
		fmt.Println("Error: ", err)
		return err
	}
	res.World = world
	res.Turn = turn

	// Reset here as well
	defer func() { closing = true }()

	return err
}

func (b *Broker) Reset(req stubs.ResetRequest, res *stubs.StatusReport) (err error) {
	err = reset(req.Kill)
	//interrupt = true
	return err
}

func (b *Broker) Pause(req stubs.PauseRequest, res *stubs.PausedCallback) (err error) {
	pause(req.NewState)

	res.Paused = req.NewState
	res.Turn = turn
	return
}

func main() {
	pAddr := flag.String("port", "8030", "Port to listen on")
	flag.Parse()
	rpc.Register(&Broker{})
	listener, _ := net.Listen("tcp", ":"+*pAddr)
	defer listener.Close()
	rpc.Accept(listener)
}
