package main

import (
	"flag"
	"fmt"
	"net"
	"net/rpc"
	"sync"
	"uk.ac.bris.cs/gameoflife/stubs"
)

var workers [16]chan stubs.Job
var numWorkers = 0

var world, newWorld [][]uint8
var turn = 0

var wg = new(sync.WaitGroup)

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

// ========================================= SUBSCRIBE & PUBLISH ============================================

func subscribe(addr string, callback string) (err error) {
	client, _ := rpc.Dial("tcp", addr)
	work := make(chan stubs.Job, 100)
	workers[numWorkers] = work
	numWorkers++
	go subscriber_loop(client, work, callback)

	return
}

func subscriber_loop(client *rpc.Client, work chan stubs.Job, callback string) {
	for {
		job := <-work
		response := new(stubs.Response)
		err := client.Call(callback, job, response)
		if err != nil {
			fmt.Println("Error: ", err.Error())
			work <- job
			break
		}

		// return
		updateWorld(response.World, job)
		wg.Done()

	}
}

func updateWorld(w [][]uint8, job stubs.Job) {

	for y := 0; y < len(w); y++ {
		for x := 0; x < len(w[y]); x++ {
			newWorld[y+job.S][x] = w[y][x]
		}
	}
}

func publish(w [][]uint8, threads int, turns int) (err error) {
	wg = new(sync.WaitGroup)
	turn = 0

	world = w
	newWorld = deepCopy(world)

	if threads > numWorkers {
		threads = numWorkers
	}

	for t := 0; t < turns; t++ {
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
	return err
}

func main() {
	pAddr := flag.String("port", "8030", "Port to listen on")
	flag.Parse()
	rpc.Register(&Broker{})
	listener, _ := net.Listen("tcp", ":"+*pAddr)
	defer listener.Close()
	rpc.Accept(listener)
}
