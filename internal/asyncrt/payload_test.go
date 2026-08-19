package asyncrt

import "testing"

// What the payload parameter bought, stated as behaviour rather than as a type
// signature.
//
// Under `any` a channel slot held an interface word, and the two things a
// receiver could get back — a real payload and "nothing" — were both `nil`-able
// values of the same static type. The parameter makes the slot the payload's
// own storage, so the vacated slot is the payload type's zero and a receiver
// that ignored the accompanying bool would read a zero rather than a stale
// value that still looks live.
func TestChannelBufferClearsTheSlotItHandsOut(t *testing.T) {
	exec := NewExecutor[string](Config{Deterministic: true})
	id := exec.ChanNew(2)

	if !exec.ChanTrySend(id, "first") {
		t.Fatal("the first send into a buffer of two must succeed")
	}
	if !exec.ChanTrySend(id, "second") {
		t.Fatal("the second send into a buffer of two must succeed")
	}
	ch := exec.channels[id]
	if ch == nil {
		t.Fatal("the channel must exist")
	}

	got, ok := exec.ChanTryRecv(id)
	if !ok || got != "first" {
		t.Fatalf("the receive gave (%q, %v), want (\"first\", true)", got, ok)
	}
	// The slot the receive emptied is the one the buffer still owns; a payload
	// left in it outlives the receive that took it.
	if ch.buf[0] != "" {
		t.Fatalf("the vacated slot still holds %q — the buffer kept a payload it handed away",
			ch.buf[0])
	}
	// The slot that was NOT read is untouched, so the clearing is aimed rather
	// than a blanket reset.
	if ch.buf[1] != "second" {
		t.Fatalf("the unread slot reads %q, want \"second\"", ch.buf[1])
	}
}

// A drained buffer hands every payload back exactly once and as itself.
//
// This is the round-trip the interface box made untestable without a type
// assertion: the drain used to return `[]any`, so a caller had to ask each
// element what it was, and an element that answered wrong was a runtime error
// rather than a compile error.
func TestDrainReturnsEveryPayloadAsItself(t *testing.T) {
	exec := NewExecutor[string](Config{Deterministic: true})
	id := exec.ChanNew(3)
	for _, want := range []string{"a", "b", "c"} {
		if !exec.ChanTrySend(id, want) {
			t.Fatalf("sending %q must succeed", want)
		}
	}
	drained := exec.DrainTasks()
	if len(drained.ChannelPayloads) != 3 {
		t.Fatalf("the drain returned %d payloads, want 3", len(drained.ChannelPayloads))
	}
	for i, want := range []string{"a", "b", "c"} {
		if drained.ChannelPayloads[i] != want {
			t.Fatalf("payload %d is %q, want %q", i, drained.ChannelPayloads[i], want)
		}
	}
}
