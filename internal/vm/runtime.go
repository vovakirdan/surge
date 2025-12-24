package vm

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// Runtime provides the interface between the VM and the outside world.
type Runtime interface {
	// Argv returns command-line arguments (excluding program name).
	Argv() []string

	// StdinReadAll reads all content from stdin as a string.
	StdinReadAll() string

	// Exit signals the VM to halt with the given exit code.
	Exit(code int)

	// ExitCode returns the exit code set by Exit, or -1 if not set.
	ExitCode() int

	// Exited returns true if Exit was called.
	Exited() bool
}

// DefaultRuntime implements Runtime using OS facilities.
type DefaultRuntime struct {
	argv     []string // program arguments (after --)
	exitCode int
	exited   bool
}

// NewDefaultRuntime creates a runtime with program arguments from os.Args.
func NewDefaultRuntime() *DefaultRuntime {
	return &DefaultRuntime{argv: os.Args[1:], exitCode: -1}
}

// NewRuntimeWithArgs creates a runtime with the specified program arguments.
func NewRuntimeWithArgs(argv []string) *DefaultRuntime {
	return &DefaultRuntime{argv: argv, exitCode: -1}
}

func (r *DefaultRuntime) Argv() []string {
	return r.argv
}

func (r *DefaultRuntime) StdinReadAll() string {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (r *DefaultRuntime) Exit(code int) {
	r.exitCode = code
	r.exited = true
}

func (r *DefaultRuntime) ExitCode() int {
	return r.exitCode
}

func (r *DefaultRuntime) Exited() bool {
	return r.exited
}

// TestRuntime implements Runtime with controlled inputs for testing.
type TestRuntime struct {
	argv     []string
	stdin    string
	exitCode int
	exited   bool
}

// NewTestRuntime creates a test runtime with controlled inputs.
func NewTestRuntime(argv []string, stdin string) *TestRuntime {
	return &TestRuntime{
		argv:     argv,
		stdin:    stdin,
		exitCode: -1,
	}
}

func (r *TestRuntime) Argv() []string {
	return r.argv
}

func (r *TestRuntime) StdinReadAll() string {
	return strings.TrimSpace(r.stdin)
}

func (r *TestRuntime) Exit(code int) {
	r.exitCode = code
	r.exited = true
}

func (r *TestRuntime) ExitCode() int {
	return r.exitCode
}

func (r *TestRuntime) Exited() bool {
	return r.exited
}

// RecordingRuntime wraps another runtime and records intrinsic results for deterministic replay.
type RecordingRuntime struct {
	rt  Runtime
	rec *Recorder
}

func NewRecordingRuntime(rt Runtime, rec *Recorder) *RecordingRuntime {
	return &RecordingRuntime{rt: rt, rec: rec}
}

func (r *RecordingRuntime) Argv() []string {
	if r == nil || r.rt == nil {
		return nil
	}
	argv := r.rt.Argv()
	if r.rec != nil {
		r.rec.RecordIntrinsic("rt_argv", nil, LogStringArray(argv))
	}
	return argv
}

func (r *RecordingRuntime) StdinReadAll() string {
	if r == nil || r.rt == nil {
		return ""
	}
	s := r.rt.StdinReadAll()
	if r.rec != nil {
		r.rec.RecordIntrinsic("rt_stdin_read_all", nil, LogString(s))
	}
	return s
}

func (r *RecordingRuntime) Exit(code int) {
	if r == nil {
		return
	}
	if r.rt != nil {
		r.rt.Exit(code)
	}
}

func (r *RecordingRuntime) ExitCode() int {
	if r == nil || r.rt == nil {
		return -1
	}
	return r.rt.ExitCode()
}

func (r *RecordingRuntime) Exited() bool {
	if r == nil || r.rt == nil {
		return false
	}
	return r.rt.Exited()
}

// ReplayRuntime serves intrinsic results from a recorded log and panics on mismatches.
// It never consults host runtime state.
type ReplayRuntime struct {
	vm *VM
	rp *Replayer

	exitCode int
	exited   bool
}

func NewReplayRuntime(vm *VM, rp *Replayer) *ReplayRuntime {
	return &ReplayRuntime{vm: vm, rp: rp, exitCode: -1}
}

func (r *ReplayRuntime) Argv() []string {
	if r == nil || r.vm == nil || r.rp == nil {
		return nil
	}
	ev := r.rp.ConsumeIntrinsic(r.vm, "rt_argv")
	argv, err := MustDecodeStringArray(ev.Ret)
	if err != nil {
		r.vm.panic(PanicInvalidReplayLogFormat, fmt.Sprintf("invalid rt_argv ret: %v", err))
	}
	return argv
}

func (r *ReplayRuntime) StdinReadAll() string {
	if r == nil || r.vm == nil || r.rp == nil {
		return ""
	}
	ev := r.rp.ConsumeIntrinsic(r.vm, "rt_stdin_read_all")
	s, err := MustDecodeString(ev.Ret)
	if err != nil {
		r.vm.panic(PanicInvalidReplayLogFormat, fmt.Sprintf("invalid rt_stdin_read_all ret: %v", err))
	}
	return s
}

func (r *ReplayRuntime) Exit(code int) {
	if r == nil || r.vm == nil || r.rp == nil {
		return
	}
	r.rp.ConsumeExit(r.vm, code)
	r.exitCode = code
	r.exited = true
}

func (r *ReplayRuntime) ExitCode() int {
	if r == nil {
		return -1
	}
	return r.exitCode
}

func (r *ReplayRuntime) Exited() bool {
	if r == nil {
		return false
	}
	return r.exited
}
