//go:build runtime_v2_pending

package vm_test

import (
	"strings"
	"testing"
	"time"
)

const placedBodyCancelSource = `async fn borrow_until_cancelled(value: &string, started: &Channel<int>) -> nothing {
    started.send(value.__len() to int);
    while true {
        let _ = value.__len();
        checkpoint().await();
    };
}
@entrypoint fn main() -> int {
    let ready: far Channel<int> = channel_on::<int>(shard(0:ShardId), READY_CAP);
    let ready_sender = ready.share();
    let seed: int = 9223372036854775808;
    let task = spawn on distributed {
        let parked = Channel::<string>::new(0:uint);
        let held: string = "held across remote and local suspension";
        let started = Channel::<int>::new(0:uint);
        let child = spawn borrow_until_cancelled(&held, &started);
        let _ = started.recv();
        let value: int = seed + 1;
        let _ = select { ready_sender.send(1) => 0; };
        let _ = parked.recv();
        let _ = child.await();
        ret value + (held.__len() to int);
    };
    AFTER
    return compare task.cancel() { Cancelled() => 0; Success(_) => 1; };
}
`

func TestRuntimeV2PlacedBodyCancellationReclaimsFrame(t *testing.T) {
	for _, row := range []struct{ name, after, capacity string }{
		{"publication", "", "0"},
		{"entered", "let _ = select { ready.recv() => 0; }; checkpoint().await(); checkpoint().await();", "0"},
		{"buffered-entered", "let _ = select { ready.recv() => 0; }; checkpoint().await(); checkpoint().await();", "1"},
	} {
		t.Run(row.name, func(t *testing.T) {
			source := strings.ReplaceAll(placedBodyCancelSource, "AFTER", row.after)
			source = strings.ReplaceAll(source, "READY_CAP", row.capacity)
			binary := buildLLVMProgramFromSource(t, source)
			for _, shards := range []string{"1", "2", "8"} {
				env := overrideEnvVar(envWithStdlib(repoRoot(t)), "SURGE_SHARDS", shards)
				for range 10 {
					_, result := runBinaryWithTimeout(t, binary, env, 30*time.Second)
					if result.exitCode != 0 || result.stdout != "" || result.stderr != "" {
						t.Fatalf("placed cancellation at shards=%s: exit=%d stdout=%q stderr=%q", shards, result.exitCode, result.stdout, result.stderr)
					}
				}
			}
			env := overrideEnvVar(envWithStdlib(repoRoot(t)), "SURGE_SHARDS", "2")
			stdout, stderr, code := runBinaryUnderValgrind(t, binary, env, 120*time.Second)
			if code != 0 || stdout != "" || hasValgrindMemcheckError(stderr) || !strings.Contains(stderr, "All heap blocks were freed") {
				t.Fatalf("placed cancellation lifetime: exit=%d stdout=%q\n%s", code, stdout, stderr)
			}
		})
	}
}
