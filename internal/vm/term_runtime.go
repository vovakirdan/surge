package vm

import "os"

const (
	defaultTermCols = 80
	defaultTermRows = 24
)

// TermRuntime provides optional terminal IO hooks for VM intrinsics.
type TermRuntime interface {
	TermEnterAltScreen()
	TermExitAltScreen()
	TermSetRawMode(enabled bool)
	TermHideCursor()
	TermShowCursor()
	TermSize() (cols, rows int)
	TermWrite(data []byte)
	TermFlush()
	TermReadEvent() TermEventData
}

// TermEventKind describes the kind of terminal event.
type TermEventKind string

// TermEventKey and related constants describe terminal event kinds.
const (
	TermEventKey    TermEventKind = "key"
	TermEventResize TermEventKind = "resize"
	TermEventEof    TermEventKind = "eof"
)

// TermKeyKind describes terminal key kinds.
type TermKeyKind string

// TermKeyChar and related constants describe terminal key kinds.
const (
	TermKeyChar      TermKeyKind = "char"
	TermKeyEnter     TermKeyKind = "enter"
	TermKeyEsc       TermKeyKind = "esc"
	TermKeyBackspace TermKeyKind = "backspace"
	TermKeyTab       TermKeyKind = "tab"
	TermKeyUp        TermKeyKind = "up"
	TermKeyDown      TermKeyKind = "down"
	TermKeyLeft      TermKeyKind = "left"
	TermKeyRight     TermKeyKind = "right"
	TermKeyHome      TermKeyKind = "home"
	TermKeyEnd       TermKeyKind = "end"
	TermKeyPageUp    TermKeyKind = "page_up"
	TermKeyPageDown  TermKeyKind = "page_down"
	TermKeyDelete    TermKeyKind = "delete"
	TermKeyF         TermKeyKind = "f"
)

// TermKeyData represents a parsed terminal key payload.
type TermKeyData struct {
	Kind TermKeyKind `json:"kind"`
	Char uint32      `json:"char,omitempty"`
	F    uint8       `json:"f,omitempty"`
}

// TermKeyEventData describes a terminal key event with modifiers.
type TermKeyEventData struct {
	Key  TermKeyData `json:"key"`
	Mods uint8       `json:"mods"`
}

// TermEventData represents a terminal event payload.
type TermEventData struct {
	Kind TermEventKind    `json:"kind"`
	Key  TermKeyEventData `json:"key,omitempty"`
	Cols int              `json:"cols,omitempty"`
	Rows int              `json:"rows,omitempty"`
}

// TermCall records a terminal intrinsic invocation in tests.
type TermCall struct {
	Name    string
	Enabled bool
	Bytes   []byte
	Cols    int
	Rows    int
}

func defaultTermSize() (cols, rows int) {
	return defaultTermCols, defaultTermRows
}

// SetTermSize overrides the terminal size returned by TestRuntime.
func (r *TestRuntime) SetTermSize(cols, rows int) {
	if r == nil {
		return
	}
	r.termCols = cols
	r.termRows = rows
	r.termSizeSet = true
}

// EnqueueTermEvent appends a terminal event to the TestRuntime queue.
func (r *TestRuntime) EnqueueTermEvent(ev TermEventData) {
	if r == nil {
		return
	}
	r.termEvents = append(r.termEvents, ev)
}

// EnqueueTermEvents appends terminal events to the TestRuntime queue.
func (r *TestRuntime) EnqueueTermEvents(events ...TermEventData) {
	if r == nil {
		return
	}
	r.termEvents = append(r.termEvents, events...)
}

// TermCalls returns a copy of recorded terminal calls.
func (r *TestRuntime) TermCalls() []TermCall {
	if r == nil {
		return nil
	}
	out := make([]TermCall, len(r.termCalls))
	for i, call := range r.termCalls {
		out[i] = cloneTermCall(call)
	}
	return out
}

// TermWrites returns copies of recorded terminal writes.
func (r *TestRuntime) TermWrites() [][]byte {
	if r == nil {
		return nil
	}
	out := make([][]byte, len(r.termWrites))
	for i, chunk := range r.termWrites {
		out[i] = append([]byte(nil), chunk...)
	}
	return out
}

// TermOutput returns the concatenated bytes written via TermWrite.
func (r *TestRuntime) TermOutput() []byte {
	if r == nil {
		return nil
	}
	total := 0
	for _, chunk := range r.termWrites {
		total += len(chunk)
	}
	out := make([]byte, 0, total)
	for _, chunk := range r.termWrites {
		out = append(out, chunk...)
	}
	return out
}

// TermEnterAltScreen records the enter-alt-screen call.
func (r *TestRuntime) TermEnterAltScreen() {
	r.appendTermCall(TermCall{Name: "term_enter_alt_screen"})
}

// TermExitAltScreen records the exit-alt-screen call.
func (r *TestRuntime) TermExitAltScreen() {
	r.appendTermCall(TermCall{Name: "term_exit_alt_screen"})
}

// TermSetRawMode records the raw mode toggle.
func (r *TestRuntime) TermSetRawMode(enabled bool) {
	r.appendTermCall(TermCall{Name: "term_set_raw_mode", Enabled: enabled})
}

// TermHideCursor records the hide-cursor call.
func (r *TestRuntime) TermHideCursor() {
	r.appendTermCall(TermCall{Name: "term_hide_cursor"})
}

// TermShowCursor records the show-cursor call.
func (r *TestRuntime) TermShowCursor() {
	r.appendTermCall(TermCall{Name: "term_show_cursor"})
}

// TermSize returns the configured size or defaults.
func (r *TestRuntime) TermSize() (cols, rows int) {
	if r == nil {
		return defaultTermSize()
	}
	if r.termSizeSet {
		return r.termCols, r.termRows
	}
	return defaultTermSize()
}

// TermWrite records a terminal write.
func (r *TestRuntime) TermWrite(data []byte) {
	if r == nil {
		return
	}
	cp := append([]byte(nil), data...)
	r.termWrites = append(r.termWrites, cp)
	r.appendTermCall(TermCall{Name: "term_write", Bytes: cp})
}

// TermFlush records the flush call.
func (r *TestRuntime) TermFlush() {
	r.appendTermCall(TermCall{Name: "term_flush"})
}

// TermReadEvent pops the next queued event or returns Eof.
func (r *TestRuntime) TermReadEvent() TermEventData {
	if r == nil || len(r.termEvents) == 0 {
		r.appendTermCall(TermCall{Name: "term_read_event"})
		return TermEventData{Kind: TermEventEof}
	}
	ev := r.termEvents[0]
	r.termEvents = r.termEvents[1:]
	r.appendTermCall(TermCall{Name: "term_read_event"})
	return ev
}

// TermEnterAltScreen forwards to the wrapped runtime when available.
func (r *RecordingRuntime) TermEnterAltScreen() {
	if r == nil || r.rt == nil {
		return
	}
	if tr, ok := r.rt.(TermRuntime); ok {
		tr.TermEnterAltScreen()
	}
}

// TermExitAltScreen forwards to the wrapped runtime when available.
func (r *RecordingRuntime) TermExitAltScreen() {
	if r == nil || r.rt == nil {
		return
	}
	if tr, ok := r.rt.(TermRuntime); ok {
		tr.TermExitAltScreen()
	}
}

// TermSetRawMode forwards to the wrapped runtime when available.
func (r *RecordingRuntime) TermSetRawMode(enabled bool) {
	if r == nil || r.rt == nil {
		return
	}
	if tr, ok := r.rt.(TermRuntime); ok {
		tr.TermSetRawMode(enabled)
	}
}

// TermHideCursor forwards to the wrapped runtime when available.
func (r *RecordingRuntime) TermHideCursor() {
	if r == nil || r.rt == nil {
		return
	}
	if tr, ok := r.rt.(TermRuntime); ok {
		tr.TermHideCursor()
	}
}

// TermShowCursor forwards to the wrapped runtime when available.
func (r *RecordingRuntime) TermShowCursor() {
	if r == nil || r.rt == nil {
		return
	}
	if tr, ok := r.rt.(TermRuntime); ok {
		tr.TermShowCursor()
	}
}

// TermSize forwards to the wrapped runtime and records the result.
func (r *RecordingRuntime) TermSize() (cols, rows int) {
	cols, rows = defaultTermSize()
	if r != nil && r.rt != nil {
		if tr, ok := r.rt.(TermRuntime); ok {
			cols, rows = tr.TermSize()
		}
	}
	if r != nil && r.rec != nil {
		r.rec.RecordIntrinsic("term_size", nil, LogTermSize(cols, rows))
	}
	return cols, rows
}

// TermWrite forwards to the wrapped runtime or stdout.
func (r *RecordingRuntime) TermWrite(data []byte) {
	if r != nil && r.rt != nil {
		if tr, ok := r.rt.(TermRuntime); ok {
			tr.TermWrite(data)
			return
		}
	}
	if len(data) == 0 {
		return
	}
	if _, err := os.Stdout.Write(data); err != nil {
		_ = err
	}
}

// TermFlush forwards to the wrapped runtime when available.
func (r *RecordingRuntime) TermFlush() {
	if r == nil || r.rt == nil {
		return
	}
	if tr, ok := r.rt.(TermRuntime); ok {
		tr.TermFlush()
	}
}

// TermReadEvent forwards to the wrapped runtime and records the result.
func (r *RecordingRuntime) TermReadEvent() TermEventData {
	ev := TermEventData{Kind: TermEventEof}
	if r != nil && r.rt != nil {
		if tr, ok := r.rt.(TermRuntime); ok {
			ev = tr.TermReadEvent()
		}
	}
	if r != nil && r.rec != nil {
		r.rec.RecordIntrinsic("term_read_event", nil, LogTermEvent(ev))
	}
	return ev
}

// TermEnterAltScreen is a no-op for replay runtime.
func (r *ReplayRuntime) TermEnterAltScreen() {
}

// TermExitAltScreen is a no-op for replay runtime.
func (r *ReplayRuntime) TermExitAltScreen() {
}

// TermSetRawMode is a no-op for replay runtime.
func (r *ReplayRuntime) TermSetRawMode(_ bool) {
}

// TermHideCursor is a no-op for replay runtime.
func (r *ReplayRuntime) TermHideCursor() {
}

// TermShowCursor is a no-op for replay runtime.
func (r *ReplayRuntime) TermShowCursor() {
}

// TermSize replays a recorded terminal size.
func (r *ReplayRuntime) TermSize() (cols, rows int) {
	if r == nil || r.vm == nil || r.rp == nil {
		return defaultTermSize()
	}
	ev := r.rp.ConsumeIntrinsic(r.vm, "term_size")
	cols, rows, err := MustDecodeTermSize(ev.Ret)
	if err != nil {
		r.vm.panic(PanicInvalidReplayLogFormat, "invalid term_size ret")
	}
	return cols, rows
}

// TermWrite writes to stdout during replay.
func (r *ReplayRuntime) TermWrite(data []byte) {
	if len(data) == 0 {
		return
	}
	if _, err := os.Stdout.Write(data); err != nil {
		_ = err
	}
}

// TermFlush is a no-op for replay runtime.
func (r *ReplayRuntime) TermFlush() {
}

// TermReadEvent replays a recorded terminal event.
func (r *ReplayRuntime) TermReadEvent() TermEventData {
	if r == nil || r.vm == nil || r.rp == nil {
		return TermEventData{Kind: TermEventEof}
	}
	ev := r.rp.ConsumeIntrinsic(r.vm, "term_read_event")
	out, err := MustDecodeTermEvent(ev.Ret)
	if err != nil {
		r.vm.panic(PanicInvalidReplayLogFormat, "invalid term_read_event ret")
	}
	return out
}

func (r *TestRuntime) appendTermCall(call TermCall) {
	if r == nil {
		return
	}
	r.termCalls = append(r.termCalls, cloneTermCall(call))
}

func cloneTermCall(call TermCall) TermCall {
	if len(call.Bytes) == 0 {
		return call
	}
	cp := append([]byte(nil), call.Bytes...)
	call.Bytes = cp
	return call
}
