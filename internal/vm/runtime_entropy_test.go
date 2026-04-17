package vm

import (
	"bytes"
	"errors"
	"math"
	"testing"
)

func TestRecordingRuntimeEntropyBytesRecordsReplayableBytes(t *testing.T) {
	base := NewTestRuntime(nil, "")
	base.EnqueueEntropyBytes([]byte{1, 2, 3})

	var log bytes.Buffer
	rec := NewRecorder(&log)
	rt := NewRecordingRuntime(base, rec)

	data, err := rt.EntropyBytes(3)
	if err != nil {
		t.Fatalf("EntropyBytes returned error: %v", err)
	}
	if !bytes.Equal(data, []byte{1, 2, 3}) {
		t.Fatalf("unexpected entropy bytes: %v", data)
	}

	rp := NewReplayerFromBytes(log.Bytes())
	replay := NewReplayRuntime(&VM{}, rp)
	replayed, err := replay.EntropyBytes(3)
	if err != nil {
		t.Fatalf("replay EntropyBytes returned error: %v", err)
	}
	if !bytes.Equal(replayed, []byte{1, 2, 3}) {
		t.Fatalf("unexpected replayed entropy bytes: %v", replayed)
	}
	if rp.Remaining() != 0 {
		t.Fatalf("expected replay log fully consumed, remaining=%d", rp.Remaining())
	}
}

func TestRecordingRuntimeEntropyBytesRecordsReplayableError(t *testing.T) {
	base := NewTestRuntime(nil, "")
	base.SetEntropyError(errors.New("entropy backend failed"))

	var log bytes.Buffer
	rec := NewRecorder(&log)
	rt := NewRecordingRuntime(base, rec)

	data, err := rt.EntropyBytes(2)
	if err == nil {
		t.Fatal("expected EntropyBytes error, got nil")
	}
	if data != nil {
		t.Fatalf("expected nil entropy bytes on error, got %v", data)
	}
	if code := entropyErrorCode(err); code != entropyErrBackend {
		t.Fatalf("expected backend entropy code %d, got %d", entropyErrBackend, code)
	}

	rp := NewReplayerFromBytes(log.Bytes())
	replay := NewReplayRuntime(&VM{}, rp)
	replayed, replayErr := replay.EntropyBytes(2)
	if replayErr == nil {
		t.Fatal("expected replay EntropyBytes error, got nil")
	}
	if replayed != nil {
		t.Fatalf("expected nil replayed entropy bytes on error, got %v", replayed)
	}
	if code := entropyErrorCode(replayErr); code != entropyErrBackend {
		t.Fatalf("expected replay backend entropy code %d, got %d", entropyErrBackend, code)
	}
	if rp.Remaining() != 0 {
		t.Fatalf("expected replay log fully consumed, remaining=%d", rp.Remaining())
	}
}

func TestTestRuntimeEntropyBytesWrapsCursorWithoutIntConversions(t *testing.T) {
	rt := NewTestRuntime(nil, "")
	rt.entropyCursor = math.MaxUint64 - 1

	data, err := rt.EntropyBytes(3)
	if err != nil {
		t.Fatalf("EntropyBytes returned error: %v", err)
	}
	if !bytes.Equal(data, []byte{0xFE, 0xFF, 0x00}) {
		t.Fatalf("unexpected wrapped entropy bytes: %v", data)
	}
	if got := rt.entropyCursor; got != 1 {
		t.Fatalf("unexpected wrapped entropy cursor: got %d want 1", got)
	}
}
