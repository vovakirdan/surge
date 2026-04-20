package asyncrt

import (
	"slices"
	"testing"
)

func TestDrainTasksCollectsPendingChannelPayloads(t *testing.T) {
	exec := NewExecutor(Config{Deterministic: true})

	bufferedID := exec.ChanNew(1)
	if ok := exec.ChanTrySend(bufferedID, "buffered"); !ok {
		t.Fatal("expected buffered send to succeed")
	}
	buffered := exec.channels[bufferedID]
	if buffered == nil {
		t.Fatal("expected buffered channel to exist")
	}

	taskID := exec.Spawn(1, nil)
	parkedID := exec.ChanNew(0)
	parked := exec.channels[parkedID]
	if parked == nil {
		t.Fatal("expected parked channel to exist")
	}
	exec.SetCurrent(taskID)
	if ok := exec.ChanSendOrPark(parkedID, "parked"); ok {
		t.Fatal("expected send to park on unbuffered channel")
	}

	drained := exec.DrainTasks()
	if len(drained.Tasks) != 1 {
		t.Fatalf("expected 1 drained task, got %d", len(drained.Tasks))
	}
	if !slices.Equal(drained.ChannelPayloads, []any{"buffered", "parked"}) {
		t.Fatalf("unexpected drained payloads: %v", drained.ChannelPayloads)
	}
	if buffered.buf != nil || buffered.head != 0 {
		t.Fatalf("expected buffered channel queue to be cleared, got buf=%v head=%d", buffered.buf, buffered.head)
	}
	if parked.sendq != nil {
		t.Fatalf("expected parked send queue to be cleared, got %v", parked.sendq)
	}
	if len(exec.channels) != 0 {
		t.Fatalf("expected executor channels to be reset, got %d", len(exec.channels))
	}
}
