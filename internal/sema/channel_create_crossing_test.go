package sema

import (
	"testing"

	"surge/internal/types"
)

func TestChannelCreateCrossingRecord(t *testing.T) {
	res, _ := checkCrossingLowering(t, `
async fn produce(dst: Placement) -> int {
	let ch: far Channel<int> = channel_on::<int>(dst, 4);
	let _ = ch;
	return 0;
}
`)
	info := requireCrossingLowering(t, res, CrossingLoweringChannelCreate)
	if !info.SuspendCapable {
		t.Fatalf("async channel_on record must be suspend-capable")
	}
	if info.Destination.Kind != CrossingDestinationPlacement {
		t.Fatalf("destination kind = %d, want placement", info.Destination.Kind)
	}
	if info.PayloadType == types.NoTypeID {
		t.Fatalf("element payload type missing")
	}
	if info.ResultType == types.NoTypeID {
		t.Fatalf("result type missing")
	}
}

func TestChannelCreateCrossingRecordSyncContextIsNotSuspendCapable(t *testing.T) {
	res, _ := checkCrossingLowering(t, `
fn produce(dst: Placement) -> int {
	let ch: far Channel<int> = channel_on::<int>(dst, 4);
	let _ = ch;
	return 0;
}
`)
	info := requireCrossingLowering(t, res, CrossingLoweringChannelCreate)
	if info.SuspendCapable {
		t.Fatalf("synchronous channel_on record must not be suspend-capable")
	}
}
