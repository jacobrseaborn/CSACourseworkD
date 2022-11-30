package stubs

var GoL = "ServerOperation.GameOfLife"
var PauseServer = "ServerOperation.Pause"
var KillServer = "ServerOperation.KillServer"

var PauseBroker = "Broker.Pause"
var RetrieveWorld = "Broker.RetrieveWorld"
var GetAlive = "Broker.GetAlive"
var Publish = "Broker.Publish"
var Subscribe = "Broker.Subscribe"
var Reset = "Broker.Reset"
var KillBroker = "Broker.KillBroker"

// Empty request
type Empty struct{}

// ResetRequest is used when distributor resets broker. Contains 'kill' flag, if true then kill broker and all servers. Else keep running
type ResetRequest struct {
	// Kill flag. If 'true' then kill broker and all servers.
	Kill bool
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

type AliveCellsCount struct {
	Turn  int
	Count int
}

// PauseRequest NewState True for paused, false for unpaused
type PauseRequest struct {
	NewState bool
}

type PausedCallback struct {
	Paused bool
	Turn   int
}
