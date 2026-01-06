package vm

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"surge/internal/mir"
	"surge/internal/source"
)

// Debugger provides interactive debugging capabilities for the VM.
type Debugger struct {
	vm          *VM
	breakpoints *Breakpoints
	inspector   *Inspector
	fmt         *Tracer

	in          *bufio.Scanner
	out         io.Writer
	interactive bool

	quit bool
}

// DebuggerResult contains the result of a debugger session.
type DebuggerResult struct {
	ExitCode int
	Quit     bool
}

// NewDebugger creates a new Debugger instance.
func NewDebugger(vm *VM, in io.Reader, out io.Writer, interactive bool) *Debugger {
	if in == nil {
		in = strings.NewReader("")
	}
	if out == nil {
		out = io.Discard
	}
	d := &Debugger{
		vm:          vm,
		breakpoints: NewBreakpoints(),
		out:         out,
		interactive: interactive,
	}
	d.in = bufio.NewScanner(in)
	d.fmt = NewFormatter(vm, vm.Files)
	d.inspector = NewInspector(vm, out)
	return d
}

// Breakpoints returns the breakpoints collection.
func (d *Debugger) Breakpoints() *Breakpoints {
	if d == nil {
		return nil
	}
	return d.breakpoints
}

// Run executes the debugger session.
func (d *Debugger) Run() (DebuggerResult, *VMError) {
	if d == nil || d.vm == nil {
		return DebuggerResult{}, nil
	}
	if vmErr := d.vm.Start(); vmErr != nil {
		return DebuggerResult{}, vmErr
	}

	for {
		if d.vm.Halted || len(d.vm.Stack) == 0 {
			return DebuggerResult{ExitCode: d.vm.ExitCode}, nil
		}
		if d.quit {
			return DebuggerResult{ExitCode: 125, Quit: true}, nil
		}

		if d.interactive {
			fmt.Fprint(d.out, "(vmdb) ") //nolint:errcheck
		}
		if !d.in.Scan() {
			break
		}
		line := strings.TrimSpace(d.in.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if res, vmErr := d.execCommand(line); vmErr != nil || res.Quit || res.ExitCode != 0 || d.vm.Halted || len(d.vm.Stack) == 0 {
			if vmErr != nil {
				return DebuggerResult{}, vmErr
			}
			if res.Quit {
				return DebuggerResult{ExitCode: 125, Quit: true}, nil
			}
			if d.vm.Halted || len(d.vm.Stack) == 0 {
				return DebuggerResult{ExitCode: d.vm.ExitCode}, nil
			}
		}
	}

	// Script mode: when input ends, continue to completion (ignoring breakpoints).
	if !d.interactive {
		for !d.vm.Halted && len(d.vm.Stack) > 0 {
			if vmErr := d.vm.Step(); vmErr != nil {
				return DebuggerResult{}, vmErr
			}
		}
	}

	if d.quit {
		return DebuggerResult{ExitCode: 125, Quit: true}, nil
	}
	return DebuggerResult{ExitCode: d.vm.ExitCode}, nil
}

func (d *Debugger) execCommand(line string) (DebuggerResult, *VMError) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return DebuggerResult{}, nil
	}
	cmd := fields[0]
	args := fields[1:]

	switch cmd {
	case "help":
		d.help()
	case "step", "s":
		return d.cmdStep()
	case "next", "n":
		return d.cmdNext()
	case "continue", "c":
		return d.cmdContinue()
	case "break":
		if len(args) != 1 {
			fmt.Fprintln(d.out, "error: break expects <file:line>") //nolint:errcheck
			return DebuggerResult{}, nil
		}
		if err := d.cmdBreak(args[0]); err != nil {
			fmt.Fprintf(d.out, "error: %s\n", err.Error()) //nolint:errcheck
		}
	case "break-fn":
		if len(args) != 1 {
			fmt.Fprintln(d.out, "error: break-fn expects <name>") //nolint:errcheck
			return DebuggerResult{}, nil
		}
		if err := d.cmdBreakFn(args[0]); err != nil {
			fmt.Fprintf(d.out, "error: %s\n", err.Error()) //nolint:errcheck
		}
	case "delete":
		if len(args) != 1 {
			fmt.Fprintln(d.out, "error: delete expects <id>") //nolint:errcheck
			return DebuggerResult{}, nil
		}
		id, err := strconv.Atoi(args[0])
		if err != nil || id <= 0 {
			fmt.Fprintln(d.out, "error: invalid breakpoint id") //nolint:errcheck
			return DebuggerResult{}, nil
		}
		if !d.breakpoints.Delete(id) {
			fmt.Fprintln(d.out, "error: unknown breakpoint id") //nolint:errcheck
		}
	case "list":
		d.cmdList()
	case "locals":
		d.inspector.Locals()
	case "stack":
		d.inspector.Stack()
	case "heap":
		d.inspector.Heap()
	case "print":
		if len(args) != 1 {
			fmt.Fprintln(d.out, "error: print expects <name|Lk>") //nolint:errcheck
			return DebuggerResult{}, nil
		}
		d.inspector.PrintLocal(args[0])
	case "quit":
		d.quit = true
		return DebuggerResult{ExitCode: 125, Quit: true}, nil
	default:
		fmt.Fprintln(d.out, "error: unknown command") //nolint:errcheck
	}

	return DebuggerResult{}, nil
}

func (d *Debugger) cmdStep() (DebuggerResult, *VMError) {
	if vmErr := d.vm.Step(); vmErr != nil {
		return DebuggerResult{}, vmErr
	}
	if d.vm.Halted || len(d.vm.Stack) == 0 {
		return DebuggerResult{ExitCode: d.vm.ExitCode}, nil
	}
	sp, ok := d.vm.StopPoint()
	if ok {
		d.printStepLine(sp)
	}
	return DebuggerResult{}, nil
}

func (d *Debugger) cmdContinue() (DebuggerResult, *VMError) {
	// If we're already sitting on a breakpoint location, advance once.
	if sp, ok := d.vm.StopPoint(); ok {
		if _, hit := d.breakpoints.Match(d.vm, sp); hit {
			if vmErr := d.vm.Step(); vmErr != nil {
				return DebuggerResult{}, vmErr
			}
		}
	}

	for !d.vm.Halted && len(d.vm.Stack) > 0 {
		sp, ok := d.vm.StopPoint()
		if !ok {
			break
		}
		if bp, hit := d.breakpoints.Match(d.vm, sp); hit {
			d.printBreakpointStop(bp, sp)
			return DebuggerResult{}, nil
		}
		if vmErr := d.vm.Step(); vmErr != nil {
			return DebuggerResult{}, vmErr
		}
	}
	return DebuggerResult{ExitCode: d.vm.ExitCode}, nil
}

func (d *Debugger) cmdNext() (DebuggerResult, *VMError) {
	sp0, ok := d.vm.StopPoint()
	if !ok {
		return DebuggerResult{ExitCode: d.vm.ExitCode}, nil
	}
	isCall := sp0.Instr != nil && sp0.Instr.Kind == mir.InstrCall
	if !isCall {
		return d.cmdStep()
	}

	origDepth := len(d.vm.Stack)
	caller := &d.vm.Stack[origDepth-1]
	callerFunc := caller.Func
	callerBB := caller.BB
	callerIP := caller.IP
	targetIP := callerIP + 1

	// Execute the call instruction.
	if vmErr := d.vm.Step(); vmErr != nil {
		return DebuggerResult{}, vmErr
	}

	for !d.vm.Halted && len(d.vm.Stack) > 0 {
		sp, ok := d.vm.StopPoint()
		if !ok {
			break
		}
		if bp, hit := d.breakpoints.Match(d.vm, sp); hit {
			d.printBreakpointStop(bp, sp)
			return DebuggerResult{}, nil
		}

		if len(d.vm.Stack) == origDepth {
			top := &d.vm.Stack[origDepth-1]
			if top.Func == callerFunc && top.BB == callerBB && top.IP == targetIP {
				d.printStepLine(sp)
				return DebuggerResult{}, nil
			}
		}

		if vmErr := d.vm.Step(); vmErr != nil {
			return DebuggerResult{}, vmErr
		}
	}

	return DebuggerResult{ExitCode: d.vm.ExitCode}, nil
}

func (d *Debugger) cmdBreak(spec string) error {
	file, line, err := ParseFileLineSpec(spec)
	if err != nil {
		return err
	}
	_, err = d.breakpoints.AddFileLine(file, line)
	return err
}

func (d *Debugger) cmdBreakFn(name string) error {
	_, err := d.breakpoints.AddFuncEntry(name)
	return err
}

func (d *Debugger) cmdList() {
	fmt.Fprintln(d.out, "breakpoints:") //nolint:errcheck
	for _, bp := range d.breakpoints.List() {
		fmt.Fprintf(d.out, "  %s\n", bp.Summary()) //nolint:errcheck
	}
}

func (d *Debugger) printBreakpointStop(bp *Breakpoint, sp StopPoint) {
	fmt.Fprintf(d.out, "stopped: breakpoint #%d\n", bp.ID)                                         //nolint:errcheck
	fmt.Fprintf(d.out, "at %s bb%d:ip%d @ %s\n", sp.FuncName, sp.BB, sp.IP, d.formatSpan(sp.Span)) //nolint:errcheck
}

func (d *Debugger) printStepLine(sp StopPoint) {
	fmt.Fprintf(d.out, "step: %s bb%d:ip%d %s @ %s\n", sp.FuncName, sp.BB, sp.IP, d.formatOp(sp), d.formatSpan(sp.Span)) //nolint:errcheck
}

func (d *Debugger) formatOp(sp StopPoint) string {
	if d.fmt == nil {
		return "<op>"
	}
	if sp.IsTerm {
		if sp.Term != nil {
			return d.fmt.formatTerminator(sp.Term)
		}
		return "<term>"
	}
	if sp.Instr != nil {
		return d.fmt.formatInstr(sp.Instr)
	}
	return "<instr>"
}

func (d *Debugger) formatSpan(span source.Span) string {
	if d.fmt != nil {
		return d.fmt.formatSpan(span)
	}
	return "<no-span>"
}

func (d *Debugger) help() {
	fmt.Fprintln(d.out, "commands:")           //nolint:errcheck
	fmt.Fprintln(d.out, "  help")              //nolint:errcheck
	fmt.Fprintln(d.out, "  step|s")            //nolint:errcheck
	fmt.Fprintln(d.out, "  next|n")            //nolint:errcheck
	fmt.Fprintln(d.out, "  continue|c")        //nolint:errcheck
	fmt.Fprintln(d.out, "  break <file:line>") //nolint:errcheck
	fmt.Fprintln(d.out, "  break-fn <name>")   //nolint:errcheck
	fmt.Fprintln(d.out, "  delete <id>")       //nolint:errcheck
	fmt.Fprintln(d.out, "  list")              //nolint:errcheck
	fmt.Fprintln(d.out, "  locals")            //nolint:errcheck
	fmt.Fprintln(d.out, "  stack")             //nolint:errcheck
	fmt.Fprintln(d.out, "  heap")              //nolint:errcheck
	fmt.Fprintln(d.out, "  print <name|Lk>")   //nolint:errcheck
	fmt.Fprintln(d.out, "  quit")              //nolint:errcheck
}
