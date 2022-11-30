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

// deepCopy copies the 2d list element by element to prevent 2 lists pointing to the same space in memory
func deepCopy(w [][]uint8) [][]uint8 {
	l := len(w)

	c := make([][]uint8, l)
	for i := 0; i < l; i++ {
		c[i] = make([]uint8, l) // deep copies square worlds only
	}

	for y := 0; y < l; y++ {
		for x := 0; x < l; x++ {
			c[y][x] = w[y][x]
		}
	}
	return c
}

// getAlive calculates the number of alive cells in a given world
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

// getOutboundIP returns the address of this broker.
func getOutboundIP() string {
	conn, _ := net.Dial("udp", "8.8.8.8:80")
	defer conn.Close()
	localAddr := conn.LocalAddr().(*net.UDPAddr).IP.String()
	return localAddr
}

// GetAlive rpc endpoint for AliveCellsCount events. Called every 2 seconds by distributor
func (b *Broker) GetAlive(nil, res *stubs.AliveCellsCount) (err error) {
	res.Turn = turn
	res.Count = getAlive(world, len(world))
	return
}

// RetrieveWorld used by distributor to retrieve a Response before the non-blocking rpc is complete.
func (b *Broker) RetrieveWorld(nil, res *stubs.Response) (err error) {
	res.World = deepCopy(world)
	res.Turn = turn

	return
}

// KillBroker called by Distributor when 'k' is pressed to shut down Broker.
func (b *Broker) KillBroker(nil, res *stubs.StatusReport) (err error) {
	defer os.Exit(0)
	return
}

// reset is called when resetting the broker (if a new task is published etc.)
func reset(kill bool) (err error) {
	fmt.Println("Resetting!")

	// unpause all servers if paused
	if paused {
		pause(false)
		fmt.Println("unpaused all paused servers")
	}

	// empty all worker channels
	for w := 0; w < numWorkers; w++ {
		for len(workers[w]) > 0 {
			<-workers[w]
		}
		fmt.Println("Emptied work for worker", w)

		// if killing server - add empty job to unblock subscriber_loop
		if kill {
			workers[w] <- stubs.Job{} // add work to flush subscriber_loop through
		}
	}

	// unpause
	pausedMutex = new(sync.Mutex)
	paused = false

	turn = 0         // re-initalise turn
	interrupt = true // interrupt interrupts the loop adding work in publish. Stops more work being added
	if kill {
		running = false // exit all subscriber loops. Effectively closing workers
	}

	if kill {
		workerCount.Wait() // wait until all servers are shutdown
		fmt.Println("All servers shutdown. Returning publish!")

		for !closing {

		}
		fmt.Println("Publish returned. Exiting broker")
	}

	return
}

func pause(pausing bool) (err error) {
	if !paused && pausing { // if running and pausing
		pausedMutex.Lock() // prevent processing
		paused = true
	} else if paused && !pausing { // if paused and unpausing
		pausedMutex.Unlock() // allow processing
		paused = false
	}
	for w := 0; w < numWorkers; w++ {
		// toggle each server as well
		err := clients[w].Call(stubs.PauseServer, stubs.PauseRequest{NewState: pausing}, new(stubs.PausedCallback)) // make rpc calls to pause processing on each individual server
		if err != nil {
			fmt.Println("Server had issue with (un)pausing.", err.Error())
		}
	}
	return err
}

// ========================================= SUBSCRIBE & PUBLISH ============================================

func subscribe(addr string, callback string) (err error) {
	client, _ := rpc.Dial("tcp", addr) // dial into server
	work := make(chan stubs.Job, 100)  // channel for jobs
	workers[numWorkers] = work         // add work channel to global list
	clients[numWorkers] = client
	numWorkers++
	workerCount.Add(1)
	go subscriber_loop(client, work, callback) // start looping to process jobs

	return
}

func subscriber_loop(client *rpc.Client, work chan stubs.Job, callback string) {
	for running {
		job := <-work // take next job from work channel
		if job.World == nil {
			break
		}

		response := new(stubs.Response)
		err := client.Call(callback, job, response) // call GameOfLife method on server
		if err != nil {
			fmt.Println("Error: ", err.Error())
			work <- job

			break
		}

		// return
		updateWorld(response.World, job.S) // update global world with slice
		wg.Done()                          // mark work done for the turn
	}
	client.Call(stubs.KillServer, stubs.ResetRequest{Kill: true}, new(stubs.StatusReport)) // once exiting subscriber_loop, call KillServer on this server
	workerCount.Done()
}

func updateWorld(w [][]uint8, startY int) {

	for y := 0; y < len(w); y++ {
		for x := 0; x < len(w[y]); x++ {
			newWorld[y+startY][x] = w[y][x] // updates shared newWorld with individual slice returned by server
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
		fmt.Println("Capping number of threads to number of servers. Using", numWorkers, "servers (100%)")
	}

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
			E:     len(world), // if not to the power of 2. Give the last worker the remainder
		}

		wg.Wait() // wait for all servers to process one turn
		turn++
		world = deepCopy(newWorld)

	}

	return
}

type Broker struct{}

func (b *Broker) Subscribe(req stubs.Subscription, res *stubs.StatusReport) (err error) {
	fmt.Println("RCP worked!", req.WorkerAddress)
	err = subscribe(req.WorkerAddress, req.Callback)
	if err != nil {
		fmt.Println("Error: ", err)
		return err
	}
	fmt.Println("Added client:", req.WorkerAddress)
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

	fmt.Println(getOutboundIP())

	rpc.Accept(listener)
}
