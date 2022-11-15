package stubs

var GoL = "ServerOperation.GameOfLife"

type Response struct {
	World [][]uint8
}

type Request struct {
	World   [][]uint8
	Dim     int
	Turns   int
	Threads int
}
