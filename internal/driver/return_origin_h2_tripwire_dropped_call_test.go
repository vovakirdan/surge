package driver

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

// RV2-DEBT-365/370 tripwire, compile line: a program that drops the last handle of a cold task
// builds. A task dropped where it stands (`worker(&l);`) is refused since the dropped-task rule
// (SEM3218), and a borrowing task bound and dropped is refused at the frame's exit, so the handle
// dropped here is an unused binding on a task that borrows nothing; the runtime must end it unrun
// (RT-COLD), and TestH2TripwireDroppedCallNeverRuns (vm) runs it on each backend.
const h2TripwireDroppedCall = `async fn worker(n: int) -> int {
    rt_exit(n + 40);
    return n;
}

fn leak() -> int {
    let t = worker(6);
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
