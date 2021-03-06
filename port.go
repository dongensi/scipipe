package scipipe

import (
	"os"
)

type Port interface {
	Connect(Port)
	IsConnected() bool
	SetConnectedStatus(bool)
}

func Connect(port1 *FilePort, port2 *FilePort) {
	port1.Connect(port2)
}

// FilePort
type FilePort struct {
	Port
	InChan    chan *InformationPacket
	inChans   []chan *InformationPacket
	outChans  []chan *InformationPacket
	connected bool
}

func NewFilePort() *FilePort {
	fp := &FilePort{
		InChan:    make(chan *InformationPacket, BUFSIZE), // This one will contain merged inputs from inChans
		inChans:   []chan *InformationPacket{},
		outChans:  []chan *InformationPacket{},
		connected: false,
	}
	return fp
}

func (localPort *FilePort) Connect(remotePort *FilePort) {
	// If localPort is an in-port
	inBoundChan := make(chan *InformationPacket, BUFSIZE)
	localPort.AddInChan(inBoundChan)
	remotePort.AddOutChan(inBoundChan)

	// If localPort is an out-port
	outBoundChan := make(chan *InformationPacket, BUFSIZE)
	localPort.AddOutChan(outBoundChan)
	remotePort.AddInChan(outBoundChan)

	localPort.SetConnectedStatus(true)
	remotePort.SetConnectedStatus(true)
}

// RunMerge merges (multiple) inputs on pt.inChans into pt.InChan. This has to
// start running when the owning process runs, in order to merge in-ports
func (pt *FilePort) RunMergeInputs() {
	defer close(pt.InChan)
	for len(pt.inChans) > 0 {
		for i, ich := range pt.inChans {
			ip, ok := <-ich
			if !ok {
				// Delete in-channel at position i
				pt.inChans = append(pt.inChans[:i], pt.inChans[i+1:]...)
				break
			}
			pt.InChan <- ip
		}
	}
}

func (pt *FilePort) AddOutChan(outChan chan *InformationPacket) {
	pt.outChans = append(pt.outChans, outChan)
}

func (pt *FilePort) AddInChan(inChan chan *InformationPacket) {
	pt.inChans = append(pt.inChans, inChan)
}

func (pt *FilePort) SetConnectedStatus(connected bool) {
	pt.connected = connected
}

func (pt *FilePort) IsConnected() bool {
	return pt.connected
}

func (pt *FilePort) Send(ip *InformationPacket) {
	for i, outChan := range pt.outChans {
		Debug.Printf("Sending on outchan %d in port\n", i)
		outChan <- ip
	}
}

func (pt *FilePort) Recv() *InformationPacket {
	return <-pt.InChan
}

func (pt *FilePort) Close() {
	for i, outChan := range pt.outChans {
		Debug.Printf("Closing outchan %d in port\n", i)
		close(outChan)
	}
}

// ParamPort
type ParamPort struct {
	Chan      chan string
	connected bool
}

func NewParamPort() *ParamPort {
	return &ParamPort{}
}

func (pp *ParamPort) Connect(otherParamPort *ParamPort) {
	if pp.Chan != nil && otherParamPort.Chan != nil {
		Error.Println("Both paramports already have initialized channels, so can't choose which to use!")
		os.Exit(1)
	} else if pp.Chan != nil && otherParamPort.Chan == nil {
		Debug.Println("Local param port, but not the other one, initialized, so connecting local to other")
		otherParamPort.Chan = pp.Chan
	} else if otherParamPort.Chan != nil && pp.Chan == nil {
		Debug.Println("The other, but not the local param port initialized, so connecting other to local")
		pp.Chan = otherParamPort.Chan
	} else if pp.Chan == nil && otherParamPort.Chan == nil {
		Debug.Println("Neither local nor other param port initialized, so creating new channel and connecting both")
		ch := make(chan string, BUFSIZE)
		pp.Chan = ch
		otherParamPort.Chan = ch
	}
	pp.SetConnectedStatus(true)
	otherParamPort.SetConnectedStatus(true)
}

func (pp *ParamPort) ConnectStr(strings ...string) {
	pp.Chan = make(chan string, BUFSIZE)
	pp.SetConnectedStatus(true)
	go func() {
		defer pp.Close()
		for _, str := range strings {
			pp.Chan <- str
		}
	}()
}

func (pp *ParamPort) SetConnectedStatus(connected bool) {
	pp.connected = connected
}

func (pp *ParamPort) IsConnected() bool {
	return pp.connected
}

func (pp *ParamPort) Send(param string) {
	pp.Chan <- param
}

func (pp *ParamPort) Recv() string {
	return <-pp.Chan
}

func (pp *ParamPort) Close() {
	close(pp.Chan)
}
