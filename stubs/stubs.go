package stubs

var GoL = "ServerOperation.GameOfLife"
var Pause = "ServerOperation.Pause"
var KillServer = "ServerOperation.KillServer"
var CloseServer = "ServerOperation.CloseServer"

var RetrieveWorld = "Broker.RetrieveWorld"
var GetAlive = "Broker.GetAlive"
var Publish = "Broker.Publish"
var Subscribe = "Broker.Subscribe"
var InitBroker = "Broker.Init"

type BrokerRequest struct {
	Threads int
}

type Subscription struct {
	WorkerAddress string
	Callback      string
}
type PublishRequest struct {
	World   [][]uint8
	Threads int
	Turns   int
}
type StatusReport struct {
	Message string
}

type Response struct {
	World [][]uint8
	Turn  int
}

type Request struct {
	World [][]uint8
	Dim   int
	Turns int
	Slice [2]int
}

type Job struct {
	World [][]uint8
	S, E  int
}

type AliveCellsCountRequest struct{}

type AliveCellsCount struct {
	Turn  int
	Count int
}

type PauseRequest struct {
	Paused bool
}

type PausedCallback struct {
	Paused bool
	Turn   int
}

type KillCallback struct{}
