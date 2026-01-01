package vm_test

import (
	"testing"
	"time"

	"surge/internal/asyncrt"
	"surge/internal/vm"
)

func TestRealTimeSleepDoesNotReturnImmediately(t *testing.T) {
	if testing.Short() {
		t.Skip("real-time sleep test skipped in short mode")
	}

	src := `
@entrypoint
fn main() -> int {
    let _ = (async {
        sleep(50).await();
        return 0;
    }).await();
    return 0;
}
`
	mirMod, files, types := compileToMIRFromSource(t, src)
	rt := vm.NewRuntimeWithArgs(nil)
	vmInstance := vm.New(mirMod, rt, files, types, nil)
	vmInstance.AsyncConfig = asyncrt.Config{
		Deterministic: true,
		TimerMode:     asyncrt.TimerModeReal,
	}

	start := time.Now()
	vmErr := vmInstance.Run()
	elapsed := time.Since(start)
	if vmErr != nil {
		t.Fatalf("vm run failed: %v", vmErr)
	}
	if elapsed < 10*time.Millisecond {
		t.Fatalf("sleep returned too fast: %v", elapsed)
	}
}
