package vm

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// Runtime provides the interface between the VM and the outside world.
type Runtime interface {
	// Argv returns command-line arguments (excluding program name).
	Argv() []string

	// StdinReadAll reads all content from stdin as a string.
	StdinReadAll() string
	// StdinReadLine reads a single line from stdin (without trailing newline).
	StdinReadLine() string

	// Exit signals the VM to halt with the given exit code.
	Exit(code int)

	// ExitCode returns the exit code set by Exit, or -1 if not set.
	ExitCode() int

	// Exited returns true if Exit was called.
	Exited() bool

	// MonotonicNow returns monotonic time in nanoseconds.
	MonotonicNow() int64
}

// DefaultRuntime implements Runtime using OS facilities.
type DefaultRuntime struct {
	argv        []string // program arguments (after --)
	exitCode    int
	exited      bool
	monoStart   time.Time
	stdinReader *bufio.Reader
}

// NewDefaultRuntime creates a runtime with program arguments from os.Args.
func NewDefaultRuntime() *DefaultRuntime {
	return &DefaultRuntime{argv: os.Args[1:], exitCode: -1, monoStart: time.Now()}
}

// NewRuntimeWithArgs creates a runtime with the specified program arguments.
func NewRuntimeWithArgs(argv []string) *DefaultRuntime {
	return &DefaultRuntime{argv: argv, exitCode: -1, monoStart: time.Now()}
}

// Argv returns the command-line arguments.
func (r *DefaultRuntime) Argv() []string {
	return r.argv
}

// StdinReadAll reads all input from stdin.
func (r *DefaultRuntime) StdinReadAll() string {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// StdinReadLine reads a single line from stdin.
func (r *DefaultRuntime) StdinReadLine() string {
	if r == nil {
		return ""
	}
	if r.stdinReader == nil {
		r.stdinReader = bufio.NewReader(os.Stdin)
	}
	line, err := r.stdinReader.ReadString('\n')
	if err != nil && err != io.EOF {
		return ""
	}
	line = strings.TrimSuffix(line, "\n")
	line = strings.TrimSuffix(line, "\r")
	return line
}

// Exit sets the exit code and marks the runtime as exited.
func (r *DefaultRuntime) Exit(code int) {
	r.exitCode = code
	r.exited = true
}

// ExitCode returns the exit code.
func (r *DefaultRuntime) ExitCode() int {
	return r.exitCode
}

// Exited reports whether the runtime has exited.
func (r *DefaultRuntime) Exited() bool {
	return r.exited
}

// MonotonicNow returns the current monotonic time in nanoseconds.
func (r *DefaultRuntime) MonotonicNow() int64 {
	if r == nil {
		return 0
	}
	if r.monoStart.IsZero() {
		r.monoStart = time.Now()
	}
	return time.Since(r.monoStart).Nanoseconds()
}

// TestRuntime implements Runtime with controlled inputs for testing.
type TestRuntime struct {
	argv     []string
	stdin    string
	stdinPos int
	exitCode int
	exited   bool

	termEvents  []TermEventData
	termCalls   []TermCall
	termWrites  [][]byte
	termCols    int
	termRows    int
	termSizeSet bool
}

// NewTestRuntime creates a test runtime with controlled inputs.
func NewTestRuntime(argv []string, stdin string) *TestRuntime {
	return &TestRuntime{
		argv:     argv,
		stdin:    stdin,
		exitCode: -1,
	}
}

// Argv returns the command-line arguments.
func (r *TestRuntime) Argv() []string {
	return r.argv
}

// StdinReadAll reads all input from stdin.
func (r *TestRuntime) StdinReadAll() string {
	return strings.TrimSpace(r.stdin)
}

// StdinReadLine reads a single line from stdin.
func (r *TestRuntime) StdinReadLine() string {
	if r == nil {
		return ""
	}
	if r.stdinPos >= len(r.stdin) {
		return ""
	}
	rest := r.stdin[r.stdinPos:]
	idx := strings.IndexByte(rest, '\n')
	var line string
	if idx < 0 {
		line = rest
		r.stdinPos = len(r.stdin)
	} else {
		line = rest[:idx]
		r.stdinPos += idx + 1
	}
	line = strings.TrimSuffix(line, "\r")
	return line
}

// Exit sets the exit code and marks the runtime as exited.
func (r *TestRuntime) Exit(code int) {
	r.exitCode = code
	r.exited = true
}

// ExitCode returns the exit code.
func (r *TestRuntime) ExitCode() int {
	return r.exitCode
}

// Exited reports whether the runtime has exited.
func (r *TestRuntime) Exited() bool {
	return r.exited
}

// MonotonicNow returns the current monotonic time in nanoseconds.
func (r *TestRuntime) MonotonicNow() int64 {
	return 0
}

// RecordingRuntime wraps another runtime and records intrinsic results for deterministic replay.
type RecordingRuntime struct {
	rt  Runtime
	rec *Recorder
}

// NewRecordingRuntime creates a RecordingRuntime that wraps another runtime.
func NewRecordingRuntime(rt Runtime, rec *Recorder) *RecordingRuntime {
	return &RecordingRuntime{rt: rt, rec: rec}
}

// Argv returns the command-line arguments.
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

// StdinReadAll reads all input from stdin.
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

// StdinReadLine reads a single line from stdin.
func (r *RecordingRuntime) StdinReadLine() string {
	if r == nil || r.rt == nil {
		return ""
	}
	s := r.rt.StdinReadLine()
	if r.rec != nil {
		r.rec.RecordIntrinsic("readline", nil, LogString(s))
	}
	return s
}

// Exit sets the exit code and marks the runtime as exited.
func (r *RecordingRuntime) Exit(code int) {
	if r == nil {
		return
	}
	if r.rt != nil {
		r.rt.Exit(code)
	}
}

// ExitCode returns the exit code.
func (r *RecordingRuntime) ExitCode() int {
	if r == nil || r.rt == nil {
		return -1
	}
	return r.rt.ExitCode()
}

// Exited reports whether the runtime has exited.
func (r *RecordingRuntime) Exited() bool {
	if r == nil || r.rt == nil {
		return false
	}
	return r.rt.Exited()
}

// MonotonicNow returns the current monotonic time in nanoseconds.
func (r *RecordingRuntime) MonotonicNow() int64 {
	if r == nil || r.rt == nil {
		return 0
	}
	v := r.rt.MonotonicNow()
	if r.rec != nil {
		r.rec.RecordIntrinsic("monotonic_now", nil, LogInt64(v))
	}
	return v
}

// ReplayRuntime serves intrinsic results from a recorded log and panics on mismatches.
// It never consults host runtime state.
type ReplayRuntime struct {
	vm *VM
	rp *Replayer

	exitCode int
	exited   bool
}

// NewReplayRuntime creates a ReplayRuntime that serves results from a replay log.
func NewReplayRuntime(vm *VM, rp *Replayer) *ReplayRuntime {
	return &ReplayRuntime{vm: vm, rp: rp, exitCode: -1}
}

// Argv returns the command-line arguments from the replay log.
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

// StdinReadAll reads all input from the replay log.
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

// StdinReadLine reads a single line from the replay log.
func (r *ReplayRuntime) StdinReadLine() string {
	if r == nil || r.vm == nil || r.rp == nil {
		return ""
	}
	ev := r.rp.ConsumeIntrinsic(r.vm, "readline")
	s, err := MustDecodeString(ev.Ret)
	if err != nil {
		r.vm.panic(PanicInvalidReplayLogFormat, fmt.Sprintf("invalid readline ret: %v", err))
	}
	return s
}

// Exit validates the exit code against the replay log.
func (r *ReplayRuntime) Exit(code int) {
	if r == nil || r.vm == nil || r.rp == nil {
		return
	}
	r.rp.ConsumeExit(r.vm, code)
	r.exitCode = code
	r.exited = true
}

// ExitCode returns the exit code.
func (r *ReplayRuntime) ExitCode() int {
	if r == nil {
		return -1
	}
	return r.exitCode
}

// Exited reports whether the runtime has exited.
func (r *ReplayRuntime) Exited() bool {
	if r == nil {
		return false
	}
	return r.exited
}

// MonotonicNow returns the current monotonic time from the replay log.
func (r *ReplayRuntime) MonotonicNow() int64 {
	if r == nil || r.vm == nil || r.rp == nil {
		return 0
	}
	ev := r.rp.ConsumeIntrinsic(r.vm, "monotonic_now")
	v, err := MustDecodeInt64(ev.Ret)
	if err != nil {
		r.vm.panic(PanicInvalidReplayLogFormat, fmt.Sprintf("invalid monotonic_now ret: %v", err))
	}
	return v
}
