package driver

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

// RV2-DEBT-365/370 tripwire, compile line: the dropped-call form builds. The task check
// exempts a Task-valued call whose value is dropped where it stands (TC-1d), and that
// exemption is sound only because the runtime creates the task cold and ends it unrun
// (RT-COLD); TestH2TripwireDroppedCallNeverRuns (vm) runs it on each backend. A change that
// refuses the form here, or lets it build while the runtime publishes at creation again,
// turns one of the two red.
const h2TripwireDroppedCall = `async fn worker(x: &string) -> int {
    rt_exit(len(x) to int + 40);
    return len(x) to int;
}

fn leak() -> int {
    let l: string = "abcdef";
    worker(&l);
    return 0;
}

@entrypoint
fn main() -> int {
    let r = leak();
    let _ = checkpoint().await();
    let _ = checkpoint().await();
    return r;
}
`

func TestH2TripwireDroppedCallBuilds(t *testing.T) {
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(h2TripwireDroppedCall)))
	if own, core, built := h2TripwirePending(t, h2TripwireDroppedCall, digest); !built {
		t.Errorf("the dropped-call program does not build: own rows %+v, core rows %d", own, len(core))
	}
}
