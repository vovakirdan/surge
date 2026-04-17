package vm

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

type entropyRuntimeError struct {
	code uint64
}

func (e *entropyRuntimeError) Error() string {
	if e == nil {
		return entropyErrorMessage(entropyErrBackend)
	}
	return entropyErrorMessage(e.code)
}

func newEntropyRuntimeError(code uint64) error {
	return &entropyRuntimeError{code: code}
}

func entropyErrorCode(err error) uint64 {
	if err == nil {
		return 0
	}
	var rtErr *entropyRuntimeError
	if errors.As(err, &rtErr) && rtErr != nil && rtErr.code != 0 {
		return rtErr.code
	}
	return entropyErrBackend
}

// EntropyBytes returns secure host entropy for the default runtime.
func (r *DefaultRuntime) EntropyBytes(n int) ([]byte, error) {
	if n < 0 {
		return nil, newEntropyRuntimeError(entropyErrBackend)
	}
	out := make([]byte, n)
	if n == 0 {
		return out, nil
	}
	if _, err := io.ReadFull(rand.Reader, out); err != nil {
		return nil, errors.Join(newEntropyRuntimeError(entropyErrUnavailable), err)
	}
	return out, nil
}

// EnqueueEntropyBytes appends deterministic entropy chunks for test runs.
func (r *TestRuntime) EnqueueEntropyBytes(chunks ...[]byte) {
	if r == nil {
		return
	}
	for _, chunk := range chunks {
		r.entropyQueue = append(r.entropyQueue, append([]byte(nil), chunk...))
	}
}

// SetEntropyError configures the next test entropy call to fail with err.
func (r *TestRuntime) SetEntropyError(err error) {
	if r == nil {
		return
	}
	r.entropyErr = err
}

// EntropyBytes returns deterministic entropy for test runs.
func (r *TestRuntime) EntropyBytes(n int) ([]byte, error) {
	if r == nil {
		return nil, newEntropyRuntimeError(entropyErrUnavailable)
	}
	if n < 0 {
		return nil, newEntropyRuntimeError(entropyErrBackend)
	}
	if r.entropyErr != nil {
		return nil, r.entropyErr
	}
	if len(r.entropyQueue) > 0 {
		next := append([]byte(nil), r.entropyQueue[0]...)
		r.entropyQueue = r.entropyQueue[1:]
		if len(next) != n {
			return nil, newEntropyRuntimeError(entropyErrBackend)
		}
		return next, nil
	}
	out := make([]byte, n)
	cursor := r.entropyCursor
	for i := range out {
		out[i] = byte(cursor & 0xFF)
		cursor++
	}
	r.entropyCursor = cursor
	return out, nil
}

// EntropyBytes records entropy bytes in replay logs while delegating to rt.
func (r *RecordingRuntime) EntropyBytes(n int) ([]byte, error) {
	if r == nil || r.rt == nil {
		return nil, newEntropyRuntimeError(entropyErrUnavailable)
	}
	data, err := r.rt.EntropyBytes(n)
	if err != nil {
		if r.rec != nil {
			r.rec.RecordIntrinsic("rt_entropy_bytes", nil, LogEntropyError(entropyErrorCode(err)))
		}
		return nil, err
	}
	if r.rec != nil {
		r.rec.RecordIntrinsic("rt_entropy_bytes", nil, LogBytes(data))
	}
	return append([]byte(nil), data...), nil
}

// EntropyBytes replays previously recorded entropy bytes from the log.
func (r *ReplayRuntime) EntropyBytes(n int) ([]byte, error) {
	if r == nil || r.vm == nil || r.rp == nil {
		return nil, newEntropyRuntimeError(entropyErrUnavailable)
	}
	ev := r.rp.ConsumeIntrinsic(r.vm, "rt_entropy_bytes")
	switch ev.Ret.Type {
	case "bytes":
		data, err := MustDecodeBytes(ev.Ret)
		if err != nil {
			r.vm.panic(PanicInvalidReplayLogFormat, fmt.Sprintf("invalid rt_entropy_bytes ret: %v", err))
			return nil, newEntropyRuntimeError(entropyErrBackend)
		}
		if len(data) != n {
			r.vm.panic(PanicInvalidReplayLogFormat, "invalid rt_entropy_bytes length")
			return nil, newEntropyRuntimeError(entropyErrBackend)
		}
		return data, nil
	case "entropy_error":
		code, err := MustDecodeEntropyError(ev.Ret)
		if err != nil {
			r.vm.panic(PanicInvalidReplayLogFormat, fmt.Sprintf("invalid rt_entropy_bytes error ret: %v", err))
			return nil, newEntropyRuntimeError(entropyErrBackend)
		}
		return nil, newEntropyRuntimeError(code)
	default:
		r.vm.panic(PanicInvalidReplayLogFormat, "invalid rt_entropy_bytes replay value type")
		return nil, newEntropyRuntimeError(entropyErrBackend)
	}
}
