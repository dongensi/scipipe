package components

import "github.com/scipipe/scipipe"

// SciPipe component that converts packets of string type to byte
type strToByte struct {
	scipipe.Process
	In  chan string
	Out chan []byte
}

func NewStrToByte(wf *scipipe.Workflow) *strToByte {
	stb := &strToByte{
		In:  make(chan string, scipipe.BUFSIZE),
		Out: make(chan []byte, scipipe.BUFSIZE),
	}
	wf.AddProc(stb)
	return stb
}

func (proc *strToByte) Run() {
	defer close(proc.Out)
	for line := range proc.In {
		proc.Out <- []byte(line)
	}
}
