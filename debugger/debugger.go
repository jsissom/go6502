package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/beevik/go6502"
	"github.com/beevik/go6502/asm"
	"github.com/beevik/go6502/disasm"
	"github.com/beevik/prefixtree"
)

var signature = "56og"

var cmds = newCommands("Debugger", []command{
	{name: "assemble", description: "Assemble a file", handler: (*host).onAssemble},
	{name: "load", description: "Load a binary", handler: (*host).onLoad},
	{name: "registers", shortcut: "r", description: "Display register contents", handler: (*host).onRegisters},
	{name: "step", shortcut: "s", description: "Step the CPU", handler: (*host).onStep},
	{name: "run", description: "Run the CPU", handler: (*host).onRun},
	{name: "exports", description: "List exported addresses", handler: (*host).onExports},
	{name: "breakpoint", shortcut: "b", description: "Breakpoint commands", commands: newCommands("Breakpoint", []command{
		{name: "list", description: "List breakpoints", handler: (*host).onBreakpointList},
		{name: "add", description: "Add a breakpoint", handler: (*host).onBreakpointAdd},
		{name: "remove", description: "Remove a breakpoint", handler: (*host).onBreakpointRemove},
		{name: "enable", description: "Enable a breakpoint", handler: (*host).onBreakpointEnable},
		{name: "disable", description: "Disable a breakpoint", handler: (*host).onBreakpointDisable},
	})},
	{name: "databreakpoint", shortcut: "db", description: "Data breakpoint commands", commands: newCommands("Data breakpoint", []command{
		{name: "list", description: "List data breakpoints", handler: (*host).onDataBreakpointList},
		{name: "add", description: "Add a data breakpoint", handler: (*host).onDataBreakpointAdd},
		{name: "remove", description: "Remove a data breakpoint", handler: (*host).onDataBreakpointRemove},
		{name: "enable", description: "Enable a data breakpoint", handler: (*host).onDataBreakpointEnable},
		{name: "disable", description: "Disable a data breakpoint", handler: (*host).onDataBreakpointDisable},
	})},
	{name: "quit", description: "Quit the program", handler: (*host).onQuit},

	// Shortcuts to nested commands
	{name: "ba", handler: (*host).onBreakpointAdd},
	{name: "br", handler: (*host).onBreakpointRemove},
	{name: "bl", handler: (*host).onBreakpointList},
	{name: "be", handler: (*host).onBreakpointEnable},
	{name: "bd", handler: (*host).onBreakpointDisable},
	{name: "dbl", handler: (*host).onDataBreakpointList},
	{name: "dba", handler: (*host).onDataBreakpointAdd},
	{name: "dbr", handler: (*host).onDataBreakpointRemove},
	{name: "dbe", handler: (*host).onDataBreakpointEnable},
	{name: "dbd", handler: (*host).onDataBreakpointDisable},
})

func main() {
	h := newHost()

	args := os.Args[1:]

	h.load("monitor.bin", 0xf800)

	if len(args) > 0 {
		for _, filename := range args {
			h.Exec(filename)
		}
	}

	h.Repl()
}

type host struct {
	input       *bufio.Scanner
	output      *bufio.Writer
	interactive bool
	stopped     bool
	mem         *go6502.FlatMemory
	cpu         *go6502.CPU
	debugger    *go6502.Debugger
	sourceMap   asm.SourceMap
}

func newHost() *host {
	h := new(host)

	h.output = bufio.NewWriter(os.Stdout)

	h.mem = go6502.NewFlatMemory()
	h.cpu = go6502.NewCPU(go6502.CMOS, h.mem)
	h.debugger = go6502.NewDebugger(h)
	h.cpu.AttachDebugger(h.debugger)

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		for {
			<-c
			h.stopped = true
		}
	}()

	return h
}

func (h *host) Print(args ...interface{}) {
	fmt.Fprint(h.output, args...)
}

func (h *host) Printf(format string, args ...interface{}) {
	fmt.Fprintf(h.output, format, args...)
	h.Flush()
}

func (h *host) Println(args ...interface{}) {
	fmt.Fprintln(h.output, args...)
	h.Flush()
}

func (h *host) Flush() {
	h.output.Flush()
}

func (h *host) GetLine() (string, error) {
	if h.input.Scan() {
		return h.input.Text(), nil
	}
	if h.input.Err() != nil {
		return "", h.input.Err()
	}
	return "", io.EOF
}

func (h *host) OnBreakpoint(cpu *go6502.CPU, addr uint16) {
	h.Printf("Breakpoint hit at $%04X.\n", addr)
	h.stopped = true
}

func (h *host) OnDataBreakpoint(cpu *go6502.CPU, addr uint16, v byte) {
	h.Printf("Data breakpoint hit on address $%04X.\n", addr)
	h.stopped = true
}

func (h *host) Load(code []byte, origin uint16) {
	h.mem.StoreBytes(origin, code)
}

func (h *host) SetStart(addr uint16) {
	h.cpu.SetPC(addr)
}

func (h *host) Repl() {
	h.input = bufio.NewScanner(os.Stdin)
	h.output = bufio.NewWriter(os.Stdout)
	h.interactive = true
	h.RunCommands()
}

func (h *host) Exec(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		exitOnError(err)
	}

	h.input = bufio.NewScanner(file)
	h.output = bufio.NewWriter(os.Stdout)
	return h.RunCommands()
}

func (h *host) RunCommands() error {
	var r commandResult
	for {
		if h.interactive {
			h.Printf("%04X* ", h.cpu.Reg.PC)
			h.Flush()
		}

		line, err := h.GetLine()
		if err != nil {
			break
		}

		if !h.interactive {
			h.Printf("* %s\n", line)
		}

		if line != "" {
			r, err = cmds.find(line)
			switch {
			case err == prefixtree.ErrPrefixNotFound:
				h.Println("command not found.")
				continue
			case err == prefixtree.ErrPrefixAmbiguous:
				h.Println("command ambiguous.")
				continue
			case err != nil:
				h.Printf("%v.\n", err)
				continue
			case r.helpText != "":
				h.Print(r.helpText)
				continue
			}
		}
		if r.cmd == nil {
			continue
		}

		args := splitArgs(r.args)
		err = r.cmd.handler(h, args)
		if err != nil {
			break
		}
	}

	return nil
}

func (h *host) onAssemble(args []string) error {
	if len(args) < 1 {
		h.Println("Syntax: assemble [filename]")
		return nil
	}

	filename := args[0]
	if filepath.Ext(filename) == "" {
		filename += ".asm"
	}

	file, err := os.Open(filename)
	if err != nil {
		h.Printf("Failed to open '%s': %v\n", filepath.Base(filename), err)
		return nil
	}
	defer file.Close()

	r, err := asm.Assemble(file, filename, false)
	if err != nil {
		h.Printf("Failed to assemble: %s\n%v\n", filepath.Base(filename), err)
		return nil
	}

	file.Close()

	ext := filepath.Ext(filename)
	filePrefix := filename[0 : len(filename)-len(ext)]
	binFilename := filePrefix + ".bin"
	file, err = os.OpenFile(binFilename, os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		h.Printf("Failed to create '%s': %v\n", filepath.Base(binFilename), err)
		return nil
	}

	var hdr [6]byte
	copy(hdr[:4], []byte(signature))
	hdr[4] = byte(r.Origin)
	hdr[5] = byte(r.Origin >> 8)
	_, err = file.Write(hdr[:])
	if err != nil {
		h.Printf("Failed to write '%s': %v\n", filepath.Base(binFilename), err)
		return nil
	}

	_, err = file.Write(r.Code)
	if err != nil {
		h.Printf("Failed to write '%s': %v\n", filepath.Base(binFilename), err)
		return nil
	}

	file.Close()

	mapFilename := filePrefix + ".map"
	file, err = os.OpenFile(mapFilename, os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		h.Printf("Failed to create '%s': %v\n", filepath.Base(mapFilename), err)
		return nil
	}

	_, err = r.SourceMap.WriteTo(file)
	if err != nil {
		h.Printf("Failed to write '%s': %v\n", filepath.Base(mapFilename), err)
		return nil
	}

	file.Close()

	h.Printf("Assembled '%s' to '%s'.\n", filepath.Base(filename), filepath.Base(binFilename))
	return nil
}

func (h *host) onLoad(args []string) error {
	origin := -1
	if len(args) < 1 {
		h.Println("Syntax: load [filename] [addr]")
		return nil
	}

	filename := args[0]
	if filepath.Ext(filename) == "" {
		filename += ".bin"
	}

	if len(args) >= 2 {
		origin = h.parseAddr(args[1])
		if origin < 0 {
			h.Printf("Unable to parse address '%s'\n", args[1])
			return nil
		}
	}

	return h.load(filename, origin)
}

func (h *host) onRegisters(args []string) error {
	reg := disasm.GetRegisterString(&h.cpu.Reg)
	fmt.Printf("%s\n", reg)
	return nil
}

func (h *host) onStep(args []string) error {
	cpu := h.cpu

	buf := make([]byte, 3)
	start := cpu.Reg.PC
	line, next := disasm.Disassemble(cpu.Mem, start)

	cpu.Step()

	b := buf[:next-start]
	cpu.Mem.LoadBytes(start, b)

	regStr := disasm.GetRegisterString(&cpu.Reg)
	fmt.Printf("%04X- %-8s  %-11s  %s C=%d\n",
		start, codeString(b), line, regStr, cpu.Cycles)

	return nil
}

func (h *host) onRun(args []string) error {
	if len(args) > 0 {
		pc := h.parseAddr(args[0])
		if pc < 0 {
			h.Printf("Unable to parse address '%s'\n", args[0])
			return nil
		}
		h.cpu.SetPC(uint16(pc))
	}

	h.Printf("Running from $%04X. Press ctrl-C to break.\n", h.cpu.Reg.PC)

	for !h.stopped {
		h.cpu.Step()
	}
	h.stopped = false

	return nil
}

func (h *host) onExports(args []string) error {
	for _, e := range h.sourceMap.Exports {
		h.Printf("%-16s $%04X\n", e.Label, e.Addr)
	}
	return nil
}

func (h *host) onBreakpointList(args []string) error {
	h.Println("Addr  Enabled")
	h.Println("----- -------")
	for _, b := range h.debugger.GetBreakpoints() {
		h.Printf("$%04X %v\n", b.Address, b.Enabled)
	}
	return nil
}

func (h *host) onBreakpointAdd(args []string) error {
	if len(args) < 1 {
		h.Printf("Syntax: breakpoint add [addr]\n")
		return nil
	}

	addr := h.parseAddr(args[0])
	if addr < 0 {
		h.Printf("Invalid breakpoint address '%v'\n", args[0])
		return nil
	}

	h.debugger.AddBreakpoint(uint16(addr))
	h.Printf("Breakpoint added at $%04x.\n", addr)
	return nil
}

func (h *host) onBreakpointRemove(args []string) error {
	if len(args) < 1 {
		h.Printf("Syntax: breakpoint remove [addr]\n")
		return nil
	}

	addr := h.parseAddr(args[0])
	if addr < 0 {
		h.Printf("Invalid breakpoint address '%v'\n", args[0])
		return nil
	}

	if !h.debugger.HasBreakpoint(uint16(addr)) {
		h.Printf("No breakpoint was set on $%04X.\n", addr)
		return nil
	}

	h.debugger.RemoveBreakpoint(uint16(addr))
	h.Printf("Breakpoint at $%04x removed.\n", addr)
	return nil
}

func (h *host) onBreakpointEnable(args []string) error {
	if len(args) < 1 {
		h.Printf("Syntax: breakpoint enable [addr]\n")
		return nil
	}

	addr := h.parseAddr(args[0])
	if addr < 0 {
		h.Printf("Invalid breakpoint address '%v'\n", args[0])
		return nil
	}

	if !h.debugger.HasBreakpoint(uint16(addr)) {
		h.Printf("No breakpoint was set on $%04X.\n", addr)
		return nil
	}

	h.debugger.EnableBreakpoint(uint16(addr))
	h.Printf("Breakpoint at $%04x enabled.\n", addr)
	return nil
}

func (h *host) onBreakpointDisable(args []string) error {
	if len(args) < 1 {
		h.Printf("Syntax: breakpoint disable [addr]\n")
		return nil
	}

	addr := h.parseAddr(args[0])
	if addr < 0 {
		h.Printf("Invalid breakpoint address '%v'\n", args[0])
		return nil
	}

	if !h.debugger.HasBreakpoint(uint16(addr)) {
		h.Printf("No breakpoint was set on $%04X.\n", addr)
		return nil
	}

	h.debugger.DisableBreakpoint(uint16(addr))
	h.Printf("Breakpoint at $%04x disabled.\n", addr)
	return nil
}

func (h *host) onDataBreakpointList(args []string) error {
	h.Println("Addr  Enabled  Value")
	h.Println("----- -------  -----")
	for _, b := range h.debugger.GetDataBreakpoints() {
		if b.Conditional {
			h.Printf("$%04X %-5v    $%02X\n", b.Address, b.Enabled, b.Value)
		} else {
			h.Printf("$%04X %-5v    <none>\n", b.Address, b.Enabled)
		}
	}
	return nil
}

func (h *host) onDataBreakpointAdd(args []string) error {
	if len(args) < 1 {
		h.Printf("Syntax: databreakpoint add [addr] [value]\n")
		return nil
	}

	addr := h.parseAddr(args[0])
	if addr < 0 {
		h.Printf("Invalid data breakpoint address '%v'\n", args[0])
		return nil
	}

	if len(args) > 1 {
		value := h.parseByte(args[1])
		if value < 0 {
			h.Printf("Invalid conditional value '%v'\n", args[1])
			return nil
		}
		h.debugger.AddConditionalDataBreakpoint(uint16(addr), byte(value))
		h.Printf("Conditional data Breakpoint added at $%04x for value $%02X.\n", addr, value)
	} else {
		h.debugger.AddDataBreakpoint(uint16(addr))
		h.Printf("Data breakpoint added at $%04x.\n", addr)
	}

	return nil
}

func (h *host) onDataBreakpointRemove(args []string) error {
	if len(args) < 1 {
		h.Printf("Syntax: databreakpoint remove [addr]\n")
		return nil
	}

	addr := h.parseAddr(args[0])
	if addr < 0 {
		h.Printf("Invalid data breakpoint address '%v'\n", args[0])
		return nil
	}

	if !h.debugger.HasDataBreakpoint(uint16(addr)) {
		h.Printf("No data breakpoint was set on $%04X.\n", addr)
		return nil
	}

	h.debugger.RemoveDataBreakpoint(uint16(addr))
	h.Printf("Data breakpoint at $%04x removed.\n", addr)
	return nil
}

func (h *host) onDataBreakpointEnable(args []string) error {
	if len(args) < 1 {
		h.Printf("Syntax: databreakpoint enable [addr]\n")
		return nil
	}

	addr := h.parseAddr(args[0])
	if addr < 0 {
		h.Printf("Invalid data breakpoint address '%v'\n", args[0])
		return nil
	}

	if !h.debugger.HasDataBreakpoint(uint16(addr)) {
		h.Printf("No data breakpoint was set on $%04X.\n", addr)
		return nil
	}

	h.debugger.EnableDataBreakpoint(uint16(addr))
	h.Printf("Data breakpoint at $%04x enabled.\n", addr)
	return nil
}

func (h *host) onDataBreakpointDisable(args []string) error {
	if len(args) < 1 {
		h.Printf("Syntax: databreakpoint disable [addr]\n")
		return nil
	}

	addr := h.parseAddr(args[0])
	if addr < 0 {
		h.Printf("Invalid data breakpoint address '%v'\n", args[0])
		return nil
	}

	if !h.debugger.HasDataBreakpoint(uint16(addr)) {
		h.Printf("No data breakpoint was set on $%04X.\n", addr)
		return nil
	}

	h.debugger.DisableDataBreakpoint(uint16(addr))
	h.Printf("Data breakpoint at $%04x disabled.\n", addr)
	return nil
}

func (h *host) onQuit(args []string) error {
	return errors.New("Exiting program")
}

func (h *host) parseAddr(s string) int {
	for _, e := range h.sourceMap.Exports {
		if e.Label == s {
			return int(e.Addr)
		}
	}

	base := 10
	if startsWith(s, "0x") {
		s, base = s[2:], 16
	} else if startsWith(s, "$") {
		s, base = s[1:], 16
	}

	o, err := strconv.ParseInt(s, base, 32)
	if err != nil || o < 0 || o > 0xffff {
		return -1
	}

	return int(o)
}

func (h *host) parseByte(s string) int {
	base := 10
	if startsWith(s, "0x") {
		s, base = s[2:], 16
	} else if startsWith(s, "$") {
		s, base = s[1:], 16
	}

	n, err := strconv.ParseInt(s, base, 32)
	if err != nil || n < -128 || n > 255 {
		return -1
	}
	if n < 0 {
		n = 256 + n
	}

	return int(n)
}

func (h *host) load(filename string, origin int) error {
	filename, err := filepath.Abs(filename)
	if err != nil {
		h.Printf("Failed to open '%s': %v\n", filepath.Base(filename), err)
		return nil
	}

	file, err := os.Open(filename)
	if err != nil {
		h.Printf("Failed to open '%s': %v\n", filepath.Base(filename), err)
		return nil
	}
	defer file.Close()

	b, err := ioutil.ReadAll(file)
	if err != nil {
		h.Printf("Failed to read '%s': %v\n", filepath.Base(filename), err)
		return nil
	}

	file.Close()

	code := b
	if len(b) >= 6 && string(b[:4]) == signature {
		origin = int(b[4]) | int(b[5])<<8
		code = b[6:]
	}
	if origin == -1 {
		h.Printf("File '%s' has no signature and requires an address\n", filepath.Base(filename))
		return nil
	}

	if origin+len(code) > 0x10000 {
		h.Printf("File '%s' exceeded 64K memory bounds\n", filepath.Base(filename))
		return nil
	}

	cpu := h.cpu
	cpu.Mem.StoreBytes(uint16(origin), code)
	h.Printf("Loaded '%s' to $%04X..$%04X\n", filepath.Base(filename), origin, int(origin)+len(code)-1)

	cpu.SetPC(uint16(origin))

	ext := filepath.Ext(filename)
	filePrefix := filename[:len(filename)-len(ext)]
	filename = filePrefix + ".map"

	file, err = os.Open(filename)
	if err == nil {
		_, err = h.sourceMap.ReadFrom(file)
		if err != nil {
			h.Printf("Failled to read '%s': %v\n", filepath.Base(filename), err)
		} else {
			h.Printf("Loaded '%s' source map\n", filepath.Base(filename))
		}
	}

	file.Close()
	return nil
}

func codeString(bc []byte) string {
	switch len(bc) {
	case 1:
		return fmt.Sprintf("%02X", bc[0])
	case 2:
		return fmt.Sprintf("%02X %02X", bc[0], bc[1])
	case 3:
		return fmt.Sprintf("%02X %02X %02X", bc[0], bc[1], bc[2])
	default:
		return ""
	}
}

func splitArgs(args string) []string {
	ss := make([]string, 0)
	for len(args) > 0 {
		i := strings.IndexAny(args, " \t")
		if i == -1 {
			if len(args) > 0 {
				ss = append(ss, args)
			}
			break
		}

		if i > 0 {
			arg := args[:i]
			ss = append(ss, arg)
		}

		for i < len(args) && (args[i] == ' ' || args[i] == '\t') {
			i++
		}
		args = args[i:]
	}
	return ss
}

func startsWith(s, m string) bool {
	if len(s) < len(m) {
		return false
	}
	return s[:len(m)] == m
}

func endsWith(s, m string) bool {
	if len(s) < len(m) {
		return false
	}
	return s[len(s)-len(m):] == m
}

func exitOnError(err error) {
	fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
	os.Exit(1)
}