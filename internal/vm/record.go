package vm

import (
	"encoding/json"
	"io"
	"sync"

	"surge/internal/source"
)

// Recorder writes a deterministic NDJSON execution log.
type Recorder struct {
	mu   sync.Mutex
	enc  *json.Encoder
	err  error
	done bool
}

func NewRecorder(w io.Writer) *Recorder {
	r := &Recorder{enc: json.NewEncoder(w)}
	r.enc.SetEscapeHTML(false)
	r.writeHeader()
	return r
}

func (r *Recorder) Err() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

func (r *Recorder) Done() bool {
	if r == nil {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.done
}

func (r *Recorder) RecordIntrinsic(name string, args []LogValue, ret LogValue) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done || r.err != nil {
		return
	}
	ev := LogIntrinsicEvent{
		Kind: "intrinsic",
		Name: name,
		Args: args,
		Ret:  ret,
	}
	r.recordLocked(ev)
}

func (r *Recorder) RecordExit(code int) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done || r.err != nil {
		return
	}
	r.recordLocked(LogExitEvent{Kind: "exit", Code: code})
	r.done = true
}

func (r *Recorder) RecordPanic(vmErr *VMError, files *source.FileSet) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done || r.err != nil {
		return
	}
	r.recordLocked(NewLogPanicEvent(vmErr, files))
	r.done = true
}

func (r *Recorder) writeHeader() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return
	}
	r.recordLocked(NewLogHeader())
}

func (r *Recorder) recordLocked(v any) {
	if r.enc == nil || r.err != nil {
		return
	}
	if err := r.enc.Encode(v); err != nil {
		r.err = err
	}
}
