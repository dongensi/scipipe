package scipipe

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	re "regexp"
	str "strings"
)

type ShellProcess struct {
	process
	InPorts          map[string]chan *FileTarget
	OutPorts         map[string]chan *FileTarget
	OutPortsDoStream map[string]bool
	PathFormatters   map[string]func(*ShellTask) string
	ParamPorts       map[string]chan string
	CustomExecute    func(*ShellTask)
	Prepend          string
	CommandPattern   string
	Spawn            bool
}

func NewShellProcess(command string) *ShellProcess {
	return &ShellProcess{
		CommandPattern:   command,
		InPorts:          make(map[string]chan *FileTarget),
		OutPorts:         make(map[string]chan *FileTarget),
		OutPortsDoStream: make(map[string]bool),
		PathFormatters:   make(map[string]func(*ShellTask) string),
		ParamPorts:       make(map[string]chan string),
		Spawn:            true,
	}
}

func Shell(cmd string) *ShellProcess {
	if !LogExists {
		InitLogInfo()
	}
	p := NewShellProcess(cmd)
	p.initPortsFromCmdPattern(cmd, nil)
	return p
}

func Sh(cmd string) *ShellProcess {
	return Shell(cmd)
}

func ShellExpand(cmd string, inPaths map[string]string, outPaths map[string]string, params map[string]string) *ShellProcess {
	cmdExp := expandCommandParamsAndPaths(cmd, params, inPaths, outPaths)
	p := NewShellProcess(cmdExp)
	p.initPortsFromCmdPattern(cmdExp, params)
	return p
}

func ShExp(cmd string, inPaths map[string]string, outPaths map[string]string, params map[string]string) *ShellProcess {
	return ShellExpand(cmd, inPaths, outPaths, params)
}

func expandCommandParamsAndPaths(cmd string, params map[string]string, inPaths map[string]string, outPaths map[string]string) (cmdExp string) {
	r := getPlaceHolderRegex()
	ms := r.FindAllStringSubmatch(cmd, -1)
	if params != nil {
		Debug.Println("Params:", params)
	}
	if inPaths != nil {
		Debug.Println("inPaths:", inPaths)
	}
	if outPaths != nil {
		Debug.Println("outPaths:", outPaths)
	}
	cmdExp = cmd
	for _, m := range ms {
		whole := m[0]
		typ := m[1]
		name := m[2]
		var newstr string
		if typ == "p" {
			if params != nil {
				if val, ok := params[name]; ok {
					Debug.Println("Found param:", val)
					newstr = val
					Debug.Println("Replacing:", whole, "->", newstr)
					cmdExp = str.Replace(cmdExp, whole, newstr, -1)
				}
			}
		} else if typ == "i" {
			if inPaths != nil {
				if val, ok := inPaths[name]; ok {
					Debug.Println("Found inPath:", val)
					newstr = val
					Debug.Println("Replacing:", whole, "->", newstr)
					cmdExp = str.Replace(cmdExp, whole, newstr, -1)
				}
			}
		} else if typ == "o" || typ == "os" {
			if outPaths != nil {
				if val, ok := outPaths[name]; ok {
					Debug.Println("Found outPath:", val)
					newstr = val
					Debug.Println("Replacing:", whole, "->", newstr)
					cmdExp = str.Replace(cmdExp, whole, newstr, -1)
				}
			}
		}
	}
	if cmd != cmdExp {
		Debug.Printf("Expanded command '%s' into '%s'\n", cmd, cmdExp)
	}
	return
}

func (p *ShellProcess) Run() {
	defer p.closeOutPorts()

	tasks := []*ShellTask{}
	Debug.Printf("[ShellProcess: %s] Starting to create and schedule tasks\n", p.CommandPattern)
	for t := range p.createTasks() {
		tasks = append(tasks, t)

		Debug.Printf("[ShellProcess: %s] Now processing task %s ...", p.CommandPattern, t.Command)

		anyPreviousFifosExists := t.anyFifosExist()
		if !anyPreviousFifosExists {
			t.createFifos()
		}

		Debug.Printf("[ShellProcess: %s] Now sending fifos for task [%s] ...\n", p.CommandPattern, t.Command)
		// Sending FIFOs for the task
		for oname, otgt := range t.OutTargets {
			if otgt.doStream {
				Debug.Printf("[ShellProcess: %s] Sending FIFO target on outport %s for task [%s] ...\n", p.CommandPattern, oname, t.Command)
				p.OutPorts[oname] <- otgt
			}
		}

		if !anyPreviousFifosExists {
			Debug.Printf("[ShellProcess: %s] firing off task in go-routine: [%s] ...\n", p.CommandPattern, t.Command)
			// Run the task
			go t.Execute()
			Debug.Printf("[ShellProcess: %s] Done firing off task in go-routine: [%s] ...\n", p.CommandPattern, t.Command)
		} else {
			go func() {
				defer close(t.Done)
				t.Done <- 1
			}()
		}
	}

	Debug.Printf("[ShellProcess: %s] Starting to loop over %d tasks to send out targets ...\n", p.CommandPattern, len(tasks))
	// Wait for finish, and send out targets in arrival order
	for _, t := range tasks {
		Debug.Printf("[ShellProcess: %s] Waiting for Done from task: %s\n", p.CommandPattern, t.Command)
		<-t.Done
		Debug.Printf("[ShellProcess: %s] Receiving Done from task: %s\n", p.CommandPattern, t.Command)
		for oname, otgt := range t.OutTargets {
			if !otgt.doStream {
				Debug.Printf("[ShellProcess: %s] Sending target on outport %s, for task [%s] ...\n", p.CommandPattern, oname, t.Command)
				p.OutPorts[oname] <- otgt
			}
		}
	}
}

func (p *ShellProcess) initPortsFromCmdPattern(cmd string, params map[string]string) {
	// Find in/out port names and Params and set up in struct fields
	r := getPlaceHolderRegex()
	ms := r.FindAllStringSubmatch(cmd, -1)
	for _, m := range ms {
		if len(m) < 3 {
			Check(errors.New("Too few matches"))
		}
		typ := m[1]
		name := m[2]
		if typ == "o" || typ == "os" {
			p.OutPorts[name] = make(chan *FileTarget, BUFSIZE)
			if typ == "os" {
				p.OutPortsDoStream[name] = true
			}
		} else if typ == "i" {
			// Set up a channel on the inports, even though this is
			// often replaced by another processes output port channel.
			// It might be nice to have it init'ed with a channel
			// anyways, for use cases when we want to send FileTargets
			// on the inport manually.
			p.InPorts[name] = make(chan *FileTarget, BUFSIZE)
		} else if typ == "p" {
			if params == nil || params[name] == "" {
				p.ParamPorts[name] = make(chan string, BUFSIZE)
			}
		}
	}
}

func (p *ShellProcess) createTasks() (ch chan *ShellTask) {
	ch = make(chan *ShellTask)
	go func() {
		defer close(ch)
		for {
			inTargets, inPortsOpen := p.receiveInputs()
			Debug.Printf("[ShellProcess.createTasks: %s] Got inTargets: %s", p.CommandPattern, inTargets)
			params, paramPortsOpen := p.receiveParams()
			Debug.Printf("[ShellProcess.createTasks: %s] Got params: %s", p.CommandPattern, params)
			if !inPortsOpen && !paramPortsOpen {
				Debug.Printf("[ShellProcess.createTasks: %s] Breaking: Both inPorts and paramPorts closed", p.CommandPattern)
				break
			}
			if len(p.InPorts) == 0 && !paramPortsOpen {
				Debug.Printf("[ShellProcess.createTasks: %s] Breaking: No inports, and params closed", p.CommandPattern)
				break
			}
			if len(p.ParamPorts) == 0 && !inPortsOpen {
				Debug.Printf("[ShellProcess.createTasks: %s] Breaking: No params, and inPorts closed", p.CommandPattern)
				break
			}
			t := NewShellTask(p.CommandPattern, inTargets, p.PathFormatters, p.OutPortsDoStream, params, p.Prepend)
			if p.CustomExecute != nil {
				t.CustomExecute = p.CustomExecute
			}
			ch <- t
			if len(p.InPorts) == 0 && len(p.ParamPorts) == 0 {
				Debug.Printf("[ShellProcess.createTasks: %s] Breaking: No inports nor params", p.CommandPattern)
				break
			}
		}
	}()
	return ch
}

func (p *ShellProcess) receiveInputs() (inTargets map[string]*FileTarget, inPortsOpen bool) {
	inPortsOpen = true
	inTargets = make(map[string]*FileTarget)
	// Read input targets on in-ports and set up path mappings
	for iname, ichan := range p.InPorts {
		Debug.Printf("[ShellProcess: %s] Receieving on inPort %s ...", p.CommandPattern, iname)
		inTarget, open := <-ichan
		if !open {
			inPortsOpen = false
			continue
		}
		Debug.Printf("[ShellProcess: %s] Got inTarget %s ...", p.CommandPattern, inTarget.GetPath())
		inTargets[iname] = inTarget
	}
	return
}

func (p *ShellProcess) receiveParams() (params map[string]string, paramPortsOpen bool) {
	paramPortsOpen = true
	params = make(map[string]string)
	// Read input targets on in-ports and set up path mappings
	for pname, pchan := range p.ParamPorts {
		pval, open := <-pchan
		if !open {
			paramPortsOpen = false
			continue
		}
		Debug.Println("Receiving param:", pname, "with value", pval)
		params[pname] = pval
	}
	return
}

func (p *ShellProcess) closeOutPorts() {
	for oname, oport := range p.OutPorts {
		Debug.Printf("[ShellProcess: %s] Closing port %s ...\n", p.CommandPattern, oname)
		close(oport)
	}
}

// Convenience method to create an (output) path formatter returning a static string
func (p *ShellProcess) SetPathFormatterString(outPort string, path string) {
	p.PathFormatters[outPort] = func(t *ShellTask) string {
		return path
	}
}

// Convenience method to create an (output) path formatter that extends the path of
// and input FileTarget
func (p *ShellProcess) SetPathFormatterExtend(outPort string, inPort string, extension string) {
	p.PathFormatters[outPort] = func(t *ShellTask) string {
		return t.InTargets[inPort].GetPath() + extension
	}
}

// Convenience method to create an (output) path formatter that uses an input's path
// but replaces parts of it.
func (p *ShellProcess) SetPathFormatterReplace(outPort string, inPort string, old string, new string) {
	p.PathFormatters[outPort] = func(t *ShellTask) string {
		return str.Replace(t.InTargets[inPort].GetPath(), old, new, -1)
	}
}

func getPlaceHolderRegex() *re.Regexp {
	r, err := re.Compile("{(o|os|i|is|p):([^{}:]+)}")
	Check(err)
	return r
}

// ------- ShellTask -------

type ShellTask struct {
	InTargets     map[string]*FileTarget
	OutTargets    map[string]*FileTarget
	Params        map[string]string
	Command       string
	CustomExecute func(*ShellTask)
	Done          chan int
}

func NewShellTask(cmdPat string, inTargets map[string]*FileTarget, outPathFuncs map[string]func(*ShellTask) string, outPortsDoStream map[string]bool, params map[string]string, prepend string) *ShellTask {
	t := &ShellTask{
		InTargets:  inTargets,
		OutTargets: make(map[string]*FileTarget),
		Params:     params,
		Command:    "",
		Done:       make(chan int),
	}
	// Create out targets
	Debug.Printf("[ShellTask: %s] Creating outTargets now ...", cmdPat)
	outTargets := make(map[string]*FileTarget)
	for oname, ofun := range outPathFuncs {
		opath := ofun(t)
		otgt := NewFileTarget(opath)
		if outPortsDoStream[oname] {
			otgt.doStream = true
		}
		Debug.Printf("[ShellTask: %s] Creating outTarget with path %s ...", cmdPat, opath)
		outTargets[oname] = otgt
	}
	t.OutTargets = outTargets
	t.Command = formatCommand(cmdPat, inTargets, outTargets, params, prepend)
	Debug.Printf("[ShellTask: %s] Created formatted command: %s", cmdPat, t.Command)
	return t
}

func (t *ShellTask) Execute() {
	defer close(t.Done)
	if !t.anyOutputExists() && !t.fifosInOutTargetsMissing() {
		if t.CustomExecute != nil {
			t.CustomExecute(t)
		} else {
			t.executeCommand(t.Command)
		}
		t.atomizeTargets()
	}
	Debug.Printf("[ShellTask: %s] Starting to send Done in t.Execute() ...)\n", t.Command)
	t.Done <- 1
	Debug.Printf("[ShellTask: %s] Done sending Done, in t.Execute()\n", t.Command)
}

func (t *ShellTask) executeCommand(cmd string) {
	Info.Printf("[ShellTask: %s] Executing command: %s \n", t.Command, cmd)
	_, err := exec.Command("bash", "-c", cmd).Output()
	Check(err)
}

func (t *ShellTask) GetInPath(inPort string) string {
	return t.InTargets[inPort].GetPath()
}

func formatCommand(cmd string, inTargets map[string]*FileTarget, outTargets map[string]*FileTarget, params map[string]string, prepend string) string {

	// Debug.Println("Formatting command with the following data:")
	// Debug.Println("prepend:", prepend)
	// Debug.Println("cmd:", cmd)
	// Debug.Println("inTargets:", inTargets)
	// Debug.Println("outTargets:", outTargets)
	// Debug.Println("params:", params)

	r := getPlaceHolderRegex()
	ms := r.FindAllStringSubmatch(cmd, -1)
	for _, m := range ms {
		whole := m[0]
		typ := m[1]
		name := m[2]
		var newstr string
		if typ == "o" || typ == "os" {
			if outTargets[name] == nil {
				msg := fmt.Sprint("Missing outpath for outport '", name, "' for command '", cmd, "'")
				Check(errors.New(msg))
			} else {
				if typ == "o" {
					newstr = outTargets[name].GetTempPath() // Means important to Atomize afterwards!
				} else if typ == "os" {
					newstr = outTargets[name].GetFifoPath()
				}
			}
		} else if typ == "i" {
			if inTargets[name] == nil {
				msg := fmt.Sprint("Missing intarget for inport '", name, "' for command '", cmd, "'")
				Check(errors.New(msg))
			} else if inTargets[name].GetPath() == "" {
				msg := fmt.Sprint("Missing inpath for inport '", name, "' for command '", cmd, "'")
				Check(errors.New(msg))
			} else {
				if typ == "i" {
					if inTargets[name].doStream {
						newstr = inTargets[name].GetFifoPath()
					} else {
						newstr = inTargets[name].GetPath()
					}
				}
			}
		} else if typ == "p" {
			if params[name] == "" {
				msg := fmt.Sprint("Missing param value param '", name, "' for command '", cmd, "'")
				Check(errors.New(msg))
			} else {
				newstr = params[name]
			}
		}
		if newstr == "" {
			msg := fmt.Sprint("Replace failed for port ", name, " forcommand '", cmd, "'")
			Check(errors.New(msg))
		}
		cmd = str.Replace(cmd, whole, newstr, -1)
	}
	// Add prepend string to the command
	if prepend != "" {
		cmd = fmt.Sprintf("%s %s", prepend, cmd)
	}
	return cmd
}

func (t *ShellTask) atomizeTargets() {
	for _, tgt := range t.OutTargets {
		if !tgt.doStream {
			Debug.Printf("Atomizing file: %s -> %s", tgt.GetTempPath(), tgt.GetPath())
			tgt.Atomize()
			Debug.Printf("Done atomizing file: %s -> %s", tgt.GetTempPath(), tgt.GetPath())
		} else {
			Debug.Printf("Target is streaming, so not atomizing: %s", tgt.GetPath())
		}
	}
}

func (t *ShellTask) cleanUpFifos() {
	for _, tgt := range t.OutTargets {
		if tgt.doStream {
			Debug.Printf("[ShellTask: %s] Cleaning up FIFO for output target: %s\n", t.Command, tgt.GetFifoPath())
			tgt.RemoveFifo()
		} else {
			Debug.Printf("[ShellTask: %s] output target is not FIFO, so not removing any FIFO: %s\n", t.Command, tgt.GetPath())
		}
	}
}

func (t *ShellTask) anyOutputExists() (anyFileExists bool) {
	anyFileExists = false
	for _, tgt := range t.OutTargets {
		opath := tgt.GetPath()
		otmpPath := tgt.GetTempPath()
		if !tgt.doStream {
			if _, err := os.Stat(opath); err == nil {
				Warn.Printf("[ShellTask: %s] Output file already exists: %s. Check your workflow for correctness!\n", t.Command, opath)
				anyFileExists = true
			}
			if _, err := os.Stat(otmpPath); err == nil {
				Warn.Printf("[ShellTask: %s] Temporary Output file already exists: %s. Check your workflow for correctness!\n", t.Command, otmpPath)
				anyFileExists = true
			}
		}
	}
	return
}

func (t *ShellTask) anyFifosExist() (anyFifosExist bool) {
	anyFifosExist = false
	for _, tgt := range t.OutTargets {
		ofifoPath := tgt.GetFifoPath()
		if tgt.doStream {
			if _, err := os.Stat(ofifoPath); err == nil {
				Warn.Printf("[ShellTask: %s] Output FIFO already exists: %s. Check your workflow for correctness!\n", t.Command, ofifoPath)
				anyFifosExist = true
			}
		}
	}
	return
}

func (t *ShellTask) fifosInOutTargetsMissing() (fifosInOutTargetsMissing bool) {
	fifosInOutTargetsMissing = false
	for _, tgt := range t.OutTargets {
		if tgt.doStream {
			ofifoPath := tgt.GetFifoPath()
			if _, err := os.Stat(ofifoPath); err != nil {
				Warn.Printf("[ShellTask: %s] FIFO Output file missing, for streaming output: %s. Check your workflow for correctness!\n", t.Command, ofifoPath)
				fifosInOutTargetsMissing = true
			}
		}
	}
	return
}

func (t *ShellTask) createFifos() {
	Debug.Printf("[ShellTask: %s] Now creating fifos for task\n", t.Command)
	for _, otgt := range t.OutTargets {
		if otgt.doStream {
			otgt.CreateFifo()
		}
	}
}
