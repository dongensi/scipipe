package scipipe

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"os"
	"os/exec"
	"sync"
	"time"
)

// ======= InformationPacket ========

// InformationPacket contains information and helper methods for a physical file on a
// normal disk.
type InformationPacket struct {
	path      string
	buffer    *bytes.Buffer
	doStream  bool
	lock      *sync.Mutex
	auditInfo *AuditInfo
	SubStream *FilePort
}

// Create new InformationPacket "object"
func NewInformationPacket(path string) *InformationPacket {
	ip := new(InformationPacket)
	ip.path = path
	ip.lock = new(sync.Mutex)
	ip.SubStream = NewFilePort()
	//Don't init buffer if not needed?
	//buf := make([]byte, 0, 128)
	//ip.buffer = bytes.NewBuffer(buf)
	return ip
}

// Get the (final) path of the physical file
func (ip *InformationPacket) GetPath() string {
	return ip.path
}

// Get the temporary path of the physical file
func (ip *InformationPacket) GetTempPath() string {
	return ip.path + ".tmp"
}

// Get the path to use when a FIFO file is used instead of a normal file
func (ip *InformationPacket) GetFifoPath() string {
	return ip.path + ".fifo"
}

// Get the size of an existing file, in bytes
func (ip *InformationPacket) GetSize() int64 {
	fi, err := os.Stat(ip.path)
	CheckErr(err)
	return fi.Size()
}

// Open the file and return a file handle (*os.File)
func (ip *InformationPacket) Open() *os.File {
	f, err := os.Open(ip.GetPath())
	Check(err, "Could not open file: "+ip.GetPath())
	return f
}

// Open the temp file and return a file handle (*os.File)
func (ip *InformationPacket) OpenTemp() *os.File {
	f, err := os.Open(ip.GetTempPath())
	Check(err, "Could not open temp file: "+ip.GetTempPath())
	return f
}

// Open the file for writing return a file handle (*os.File)
func (ip *InformationPacket) OpenWriteTemp() *os.File {
	f, err := os.Create(ip.GetTempPath())
	Check(err, "Could not open temp file for writing: "+ip.GetTempPath())
	return f
}

// Read the whole content of the file and return as a byte array ([]byte)
func (ip *InformationPacket) Read() []byte {
	dat, err := ioutil.ReadFile(ip.GetPath())
	Check(err, "Could not open file for reading: "+ip.GetPath())
	return dat
}

// Read the whole content of the file and return as a byte array ([]byte)
func (ip *InformationPacket) ReadAuditFile() []byte {
	dat, err := ioutil.ReadFile(ip.GetAuditFilePath())
	Check(err, "Could not open file for reading: "+ip.GetAuditFilePath())
	return dat
}

// Write a byte array ([]byte) to the file (first to its temp path, and then atomize)
func (ip *InformationPacket) WriteTempFile(dat []byte) {
	err := ioutil.WriteFile(ip.GetTempPath(), dat, 0644)
	Check(err, "Could not write to temp file: "+ip.GetTempPath())
}

const (
	sleepDurationSec = 1
)

// Change from the temporary file name to the final file name
func (ip *InformationPacket) Atomize() {
	Debug.Println("InformationPacket: Atomizing", ip.GetTempPath(), "->", ip.GetPath())
	doneAtomizing := false
	for !doneAtomizing {
		if ip.TempFileExists() {
			ip.lock.Lock()
			err := os.Rename(ip.GetTempPath(), ip.path)
			Check(err, "Could not rename file: "+ip.GetTempPath())
			ip.lock.Unlock()
			doneAtomizing = true
			Debug.Println("InformationPacket: Done atomizing", ip.GetTempPath(), "->", ip.GetPath())
		} else {
			Debug.Printf("Sleeping for %d seconds before atomizing ...\n", sleepDurationSec)
			time.Sleep(time.Duration(sleepDurationSec) * time.Second)
		}
	}
}

// Create FIFO file for the InformationPacket
func (ip *InformationPacket) CreateFifo() {
	ip.lock.Lock()
	cmd := "mkfifo " + ip.GetFifoPath()
	Debug.Println("Now creating FIFO with command:", cmd)

	if _, err := os.Stat(ip.GetFifoPath()); err == nil {
		Warning.Println("FIFO already exists, so not creating a new one:", ip.GetFifoPath())
	} else {
		_, err := exec.Command("bash", "-c", cmd).Output()
		Check(err, "Could not execute command: "+cmd)
	}

	ip.lock.Unlock()
}

// Remove the FIFO file, if it exists
func (ip *InformationPacket) RemoveFifo() {
	// FIXME: Shouldn't we check first whether the fifo exists?
	ip.lock.Lock()
	output, err := exec.Command("bash", "-c", "rm "+ip.GetFifoPath()).Output()
	Check(err, "Could not delete fifo file: "+ip.GetFifoPath())
	Debug.Println("Removed FIFO output: ", output)
	ip.lock.Unlock()
}

// Check if the file exists (at its final file name)
func (ip *InformationPacket) Exists() bool {
	exists := false
	ip.lock.Lock()
	if _, err := os.Stat(ip.GetPath()); err == nil {
		exists = true
	}
	ip.lock.Unlock()
	return exists
}

// Check if the temp-file exists
func (ip *InformationPacket) TempFileExists() bool {
	tempFileExists := false
	ip.lock.Lock()
	if _, err := os.Stat(ip.GetTempPath()); err == nil {
		tempFileExists = true
	}
	ip.lock.Unlock()
	return tempFileExists
}

// FifoFileExists checks if the FIFO-file (named pipe file) exists
func (ip *InformationPacket) FifoFileExists() bool {
	fifoFileExists := false
	ip.lock.Lock()
	if _, err := os.Stat(ip.GetFifoPath()); err == nil {
		fifoFileExists = true
	}
	ip.lock.Unlock()
	return fifoFileExists
}

func (ip *InformationPacket) GetParam(key string) string {
	val, ok := ip.GetAuditInfo().Params[key]
	if !ok {
		Error.Fatalf("Could not find parameter %s in ip with path: %s\n", key, ip.GetPath())
	}
	return val
}

func (ip *InformationPacket) GetKey(k string) string {
	v, ok := ip.GetAuditInfo().Keys[k]
	if !ok {
		Error.Fatalf("Could not find key %s in ip with path: %s\n", k, ip.GetPath())
	}
	return v
}

func (ip *InformationPacket) GetKeys() map[string]string {
	return ip.GetAuditInfo().Keys
}

func (ip *InformationPacket) AddKey(k string, v string) {
	ai := ip.GetAuditInfo()
	if ai.Keys[k] != "" && ai.Keys[k] != v {
		Error.Fatalf("Can not add value %s to existing key %s with different value %s\n", v, k, ai.Keys[k])
	}
	ai.Keys[k] = v
}

func (ip *InformationPacket) AddKeys(keys map[string]string) {
	for k, v := range keys {
		ip.AddKey(k, v)
	}
}

func (ip *InformationPacket) UnMarshalJson(v interface{}) {
	d := ip.Read()
	err := json.Unmarshal(d, v)
	Check(err, "Could not unmarshal content of file: "+ip.GetPath())
}

func (ip *InformationPacket) GetAuditInfo() *AuditInfo {
	defer ip.lock.Unlock()
	ip.lock.Lock()
	if ip.auditInfo == nil {
		ip.auditInfo = NewAuditInfo()
		auditFileData, err := ioutil.ReadFile(ip.GetAuditFilePath())
		if err == nil {
			unmarshalErr := json.Unmarshal(auditFileData, ip.auditInfo)
			Check(unmarshalErr, "Could not unmarshal audit log file content: "+ip.GetAuditFilePath())
		}
	}
	return ip.auditInfo
}

func (ip *InformationPacket) SetAuditInfo(ai *AuditInfo) {
	ip.lock.Lock()
	ip.auditInfo = ai
	ip.lock.Unlock()
}

func (ip *InformationPacket) GetAuditFilePath() string {
	return ip.GetPath() + ".audit.json"
}

func (ip *InformationPacket) WriteAuditLogToFile() {
	auditInfo := ip.GetAuditInfo()
	auditInfoJson, jsonErr := json.MarshalIndent(auditInfo, "", "    ")
	Check(jsonErr, "Could not marshall JSON")
	writeErr := ioutil.WriteFile(ip.GetAuditFilePath(), auditInfoJson, 0644)
	Check(writeErr, "Could not write audit file: "+ip.GetPath())
}

// ======= IPGen=======

// IPGen is initialized by a set of strings with file paths, and from that will
// return instantiated (generated) InformationPacket on its Out-port, when run.
type IPGen struct {
	Process
	name      string
	Out       *FilePort
	FilePaths []string
}

// Initialize a new IPGen component from a list of file paths
func NewIPGen(workflow *Workflow, name string, filePaths ...string) (fq *IPGen) {
	fq = &IPGen{
		name:      name,
		Out:       NewFilePort(),
		FilePaths: filePaths,
	}
	workflow.AddProc(fq)
	return
}

// Execute the IPGen, returning instantiated InformationPacket
func (ipg *IPGen) Run() {
	defer ipg.Out.Close()
	for _, fp := range ipg.FilePaths {
		ipg.Out.Send(NewInformationPacket(fp))
	}
}

func (ipg *IPGen) Name() string {
	return ipg.name
}

// Check if the IPGen outport is connected
func (ipg *IPGen) IsConnected() bool {
	return ipg.Out.IsConnected()
}
