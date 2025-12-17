package vm

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type replayEvent struct {
	Kind      string
	Intrinsic *LogIntrinsicEvent
	Exit      *LogExitEvent
	Panic     *LogPanicEvent
}

// Replayer reads and validates a deterministic NDJSON execution log.
type Replayer struct {
	header   LogHeader
	events   []replayEvent
	next     int
	parseErr error

	consumedTerm bool
}

func NewReplayerFromBytes(data []byte) *Replayer {
	r := &Replayer{}
	r.parse(bytes.NewReader(data))
	return r
}

func NewReplayerFromReader(rd io.Reader) *Replayer {
	r := &Replayer{}
	r.parse(rd)
	return r
}

func (r *Replayer) Validate() error {
	if r == nil {
		return fmt.Errorf("nil replayer")
	}
	if r.parseErr != nil {
		return r.parseErr
	}
	if r.header.Kind != "header" {
		return fmt.Errorf("missing header")
	}
	if r.header.V != 1 {
		return fmt.Errorf("unsupported log version %d", r.header.V)
	}
	if r.header.Policy.Overflow != "panic" || r.header.Policy.Bounds != "panic" {
		return fmt.Errorf("unsupported policy")
	}
	return nil
}

func (r *Replayer) Remaining() int {
	if r == nil {
		return 0
	}
	if r.next >= len(r.events) {
		return 0
	}
	return len(r.events) - r.next
}

func (r *Replayer) PeekKind() (string, bool) {
	if r == nil || r.next >= len(r.events) {
		return "", false
	}
	return r.events[r.next].Kind, true
}

func (r *Replayer) ConsumeIntrinsic(vm *VM, name string) *LogIntrinsicEvent {
	ev := r.expectNext(vm, "intrinsic")
	if ev.Intrinsic == nil {
		vm.panic(PanicInvalidReplayLogFormat, "invalid intrinsic event")
	}
	if ev.Intrinsic.Name != name {
		vm.panic(PanicReplayMismatch, fmt.Sprintf("replay mismatch: expected intrinsic %q, got %q", name, ev.Intrinsic.Name))
	}
	return ev.Intrinsic
}

func (r *Replayer) ConsumeExit(vm *VM, code int) {
	ev := r.expectNext(vm, "exit")
	if ev.Exit == nil {
		vm.panic(PanicInvalidReplayLogFormat, "invalid exit event")
	}
	if ev.Exit.Code != code {
		vm.panic(PanicReplayMismatch, fmt.Sprintf("replay mismatch: expected exit code %d, got %d", ev.Exit.Code, code))
	}
	r.consumedTerm = true
}

func (r *Replayer) CheckPanic(vm *VM, actual *VMError) *VMError {
	if r == nil {
		return actual
	}
	if actual == nil {
		return nil
	}
	if vm == nil {
		return actual
	}

	if err := r.Validate(); err != nil {
		return vm.eb.invalidReplayLogFormat(err.Error())
	}

	if actual.Code == PanicReplayLogExhausted || actual.Code == PanicReplayMismatch || actual.Code == PanicInvalidReplayLogFormat {
		return actual
	}

	if r.next >= len(r.events) {
		return vm.eb.replayLogExhausted("")
	}
	ev := r.events[r.next]
	if ev.Kind != "panic" || ev.Panic == nil {
		return vm.eb.replayMismatch(fmt.Sprintf("replay mismatch: expected panic, got %s", ev.Kind))
	}

	want := *ev.Panic
	got := NewLogPanicEvent(actual, vm.Files)
	if want.Code != got.Code || want.Msg != got.Msg || want.At != got.At || !equalStrings(want.Bt, got.Bt) {
		return vm.eb.replayMismatch("replay mismatch: panic does not match log")
	}

	r.next++
	r.consumedTerm = true
	return actual
}

func (r *Replayer) FinalizeExit(vm *VM, code int) *VMError {
	if r == nil {
		return nil
	}
	if vm == nil {
		return nil
	}
	if err := r.Validate(); err != nil {
		return vm.eb.invalidReplayLogFormat(err.Error())
	}

	if r.consumedTerm {
		if r.next != len(r.events) {
			return vm.eb.replayMismatch("replay mismatch: extra log events after termination")
		}
		return nil
	}

	if r.next >= len(r.events) {
		return vm.eb.replayLogExhausted("")
	}
	ev := r.events[r.next]
	if ev.Kind != "exit" || ev.Exit == nil {
		return vm.eb.replayMismatch(fmt.Sprintf("replay mismatch: expected exit, got %s", ev.Kind))
	}
	if ev.Exit.Code != code {
		return vm.eb.replayMismatch(fmt.Sprintf("replay mismatch: expected exit code %d, got %d", ev.Exit.Code, code))
	}
	r.next++
	r.consumedTerm = true
	if r.next != len(r.events) {
		return vm.eb.replayMismatch("replay mismatch: extra log events after termination")
	}
	return nil
}

func (r *Replayer) expectNext(vm *VM, kind string) replayEvent {
	if r == nil {
		vm.panic(PanicReplayLogExhausted, "replay log exhausted")
	}
	if err := r.Validate(); err != nil {
		vm.panic(PanicInvalidReplayLogFormat, err.Error())
	}
	if r.next >= len(r.events) {
		vm.panic(PanicReplayLogExhausted, "replay log exhausted")
	}
	ev := r.events[r.next]
	if ev.Kind != kind {
		vm.panic(PanicReplayMismatch, fmt.Sprintf("replay mismatch: expected %s, got %s", kind, ev.Kind))
	}
	r.next++
	return ev
}

func (r *Replayer) parse(rd io.Reader) {
	sc := bufio.NewScanner(rd)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}

		if r.parseErr != nil {
			continue
		}

		if line[0] != '{' {
			r.parseErr = fmt.Errorf("invalid JSON on line %d", lineNo)
			continue
		}

		if r.header.Kind == "" {
			var h LogHeader
			if err := json.Unmarshal([]byte(line), &h); err != nil {
				r.parseErr = fmt.Errorf("invalid header: %w", err)
				continue
			}
			r.header = h
			continue
		}

		var k struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal([]byte(line), &k); err != nil {
			r.parseErr = fmt.Errorf("invalid event on line %d: %w", lineNo, err)
			continue
		}
		switch k.Kind {
		case "intrinsic":
			var ev LogIntrinsicEvent
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				r.parseErr = fmt.Errorf("invalid intrinsic event on line %d: %w", lineNo, err)
				continue
			}
			r.events = append(r.events, replayEvent{Kind: "intrinsic", Intrinsic: &ev})
		case "exit":
			var ev LogExitEvent
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				r.parseErr = fmt.Errorf("invalid exit event on line %d: %w", lineNo, err)
				continue
			}
			r.events = append(r.events, replayEvent{Kind: "exit", Exit: &ev})
		case "panic":
			var ev LogPanicEvent
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				r.parseErr = fmt.Errorf("invalid panic event on line %d: %w", lineNo, err)
				continue
			}
			r.events = append(r.events, replayEvent{Kind: "panic", Panic: &ev})
		default:
			r.parseErr = fmt.Errorf("unknown event kind %q on line %d", k.Kind, lineNo)
		}
	}
	if err := sc.Err(); err != nil && r.parseErr == nil {
		r.parseErr = err
	}
	if r.header.Kind == "" && r.parseErr == nil {
		r.parseErr = fmt.Errorf("missing header")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
