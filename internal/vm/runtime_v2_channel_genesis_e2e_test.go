package vm_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"surge/internal/sema"
)

// The genesis mint vertical: a program creates channels on explicit and
// distributed placements and completes. Anchored send/recv over the minted
// handles is the next vertical; this proves the producer round trip, the
// suspend/resume path, and a second mint on the same destination. Runs under
// the test-scoped capability override until the production flip.
const runtimeV2ChannelGenesisSource = `
async fn produce() -> int {
    let first: far Channel<int> = channel_on::<int>(shard(0:ShardId), 4);
    let _ = first;
    let second: far Channel<int> = channel_on::<int>(distributed, 2);
    let _ = second;
    let third: far Channel<int> = channel_on::<int>(distributed, 2);
    let _ = third;
    print("channel-genesis-ok");
    return 0;
}

@entrypoint
fn main() -> int {
    let task = spawn produce();
    return compare task.await() {
        Success(code) => code;
        Cancelled() => 90;
    };
}
`

func TestRuntimeV2ChannelGenesisOverrideAcrossShards(t *testing.T) {
	forms := map[sema.CrossingLoweringKind]bool{
		sema.CrossingLoweringChannelCreate: true,
	}
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2ChannelGenesisSource, forms)
	baseEnv := envWithStdlib(repoRoot(t))
	for _, shardCount := range []int{1, 2, 8} {
		t.Run(fmt.Sprintf("shards_%d", shardCount), func(t *testing.T) {
			value := strconv.Itoa(shardCount)
			env := overrideEnvVar(baseEnv, "SURGE_SHARDS", value)
			env = overrideEnvVar(env, "SURGE_THREADS", value)
			duration, result := runBinaryWithTimeout(t, outputPath, env, 20*time.Second)
			if result.exitCode != 0 {
				t.Fatalf(
					"channel genesis e2e failed (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
					result.exitCode,
					duration,
					result.stdout,
					result.stderr,
				)
			}
			if !strings.Contains(result.stdout, "channel-genesis-ok") {
				t.Fatalf("channel genesis e2e missing completion marker; stdout=%q", result.stdout)
			}
		})
	}
}
