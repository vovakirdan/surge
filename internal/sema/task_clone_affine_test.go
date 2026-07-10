package sema

import "testing"

const taskClonePrelude = `
extern<Task<T>> {
	@intrinsic fn clone(self: &Task<T>) -> Task<T>;
}
`

func TestTaskCloneRejectsDirectFarTaskPayload(t *testing.T) {
	codes := onCrossingCodes(t, taskClonePrelude+`
fn clone_outer(t: &Task<far Task<int>>) -> Task<far Task<int>> {
	return t.clone();
}
`)
	if !codes["SEM3116"] {
		t.Fatalf("expected SEM3116 for Task<far Task<int>>.clone(), got: %s", joinCodes(codes))
	}
}

func TestTaskCloneKeepsPlainPayloadClonable(t *testing.T) {
	codes := onCrossingCodes(t, taskClonePrelude+`
fn clone_outer(t: &Task<int>) -> Task<int> {
	return t.clone();
}
`)
	if len(codes) != 0 {
		t.Fatalf("expected Task<int>.clone() to remain valid, got: %s", joinCodes(codes))
	}
}
