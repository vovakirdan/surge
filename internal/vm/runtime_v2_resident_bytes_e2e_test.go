package vm_test

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// runtimeV2ResidentTraceValues reads the one TRACE_RESIDENT line the exec
// trace prints for `reason`, as name=value fields.
func runtimeV2ResidentTraceValues(t *testing.T, stderr string, reason string) (map[string]uint64, string) {
	t.Helper()
	prefix := "TRACE_RESIDENT reason=" + reason + " "
	for _, line := range strings.Split(stderr, "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		values := make(map[string]uint64)
		for _, field := range strings.Fields(line) {
			name, raw, ok := strings.Cut(field, "=")
			if !ok || name == "reason" {
				continue
			}
			value, err := strconv.ParseUint(raw, 10, 64)
			if err != nil {
				t.Fatalf("TRACE_RESIDENT field %s=%q is not a number:\n%s", name, raw, line)
			}
			values[name] = value
		}
		return values, line
	}
	t.Fatalf("no %q line in stderr:\n%s", prefix, stderr)
	return nil, ""
}

// What a crossing keeps resident, by the owner of each byte, read off the
// TRACE_RESIDENT line the exec trace prints at exit (rt_resident_bytes.h):
//
//   - envelope and padding: the transport's fixed record per queued message,
//     fields and alignment slack apart, held from push to pop;
//   - record: the pending that tracks the crossing on the source side;
//   - payload: the shipped state block at its descriptor's width, held from
//     submission to the publication-accepted handoff;
//   - crossing clone: the bytes compiled code duplicates into that block for
//     every Copy capture -- a total, since the copy lives inside the payload.
//
// The program crosses twice with the same 64-byte Copy value captured, once
// as a spawned far task and once as an immediate `on` block, so the clone
// total is exactly two blocks wide and the payload was at least that wide.
// Every balance reads zero at exit: what a crossing acquires, it gives back.
const runtimeV2ResidentBytesSource = `
@copy
type Block = { a: int, b: int, c: int, d: int, e: int, f: int, g: int, h: int };

fn weigh(block: Block) -> int {
    return block.a + block.h;
}

async fn run() -> int {
    let block: Block = Block { a: 1, b: 2, c: 3, d: 4, e: 5, f: 6, g: 7, h: 41 };
    let task: far Task<int> = spawn on shard(1:ShardId) { ret weigh(block); };
    let v1: int = compare task.await() { Success(x) => x; Cancelled() => 0 - 2; };
    let reply: TaskResult<int> = on shard(1:ShardId) { ret weigh(block); };
    let v2: int = compare reply { Success(x) => x; Cancelled() => 0 - 2; };
    if v1 == 42 {
        if v2 == 42 {
            print("resident-bytes-ok");
            return 0;
        }
    }
    print("FAIL v1=");
    print(v1 to string);
    print(" v2=");
    print(v2 to string);
    return 1;
}

@entrypoint
fn main() -> int {
    let task = spawn run();
    return compare task.await() {
        Success(code) => code;
        Cancelled() => 90;
    };
}
`

const runtimeV2ResidentBlockBytes = 64

func TestRuntimeV2ResidentBytesTelemetry(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2ResidentBytesSource, nil)
	env := envWithStdlib(repoRoot(t))
	env = overrideEnvVar(env, "SURGE_SHARDS", "2")
	env = overrideEnvVar(env, "SURGE_THREADS", "2")
	env = overrideEnvVar(env, "SURGE_TRACE_EXEC", "1")

	duration, result := runBinaryWithTimeout(t, outputPath, env, 30*time.Second)
	if result.exitCode != 0 || !strings.Contains(result.stdout, "resident-bytes-ok") {
		t.Fatalf("crossing program failed (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
			result.exitCode, duration, result.stdout, result.stderr)
	}
	values, line := runtimeV2ResidentTraceValues(t, result.stderr, "exit")
	t.Logf("%s", line)

	field := func(name string) uint64 {
		t.Helper()
		got, ok := values[name]
		if !ok {
			t.Fatalf("TRACE_RESIDENT exit line has no %s field:\n%s", name, line)
		}
		return got
	}
	for _, kind := range []string{"envelope", "padding", "record", "payload", "sidecar"} {
		if live := field(kind + "_live"); live != 0 {
			t.Fatalf("%s bytes still resident at exit: %d, want 0 -- a crossing gave back less than it took:\n%s",
				kind, live, line)
		}
	}
	if underflows := field("underflows"); underflows != 0 {
		t.Fatalf("%d releases outran their acquires:\n%s", underflows, line)
	}
	if clones := field("crossing_clones"); clones != 2 {
		t.Fatalf("crossing_clones = %d, want 2 (one Copy capture per crossing):\n%s", clones, line)
	}
	if bytes := field("crossing_clone_bytes"); bytes != 2*runtimeV2ResidentBlockBytes {
		t.Fatalf("crossing_clone_bytes = %d, want %d (two 64-byte Copy captures):\n%s",
			bytes, 2*runtimeV2ResidentBlockBytes, line)
	}
	// A state block holds its captures, so the shipped payload was at least
	// one block wide on every crossing; and a crossing can be resident only
	// while an envelope, a pending and a state block are.
	if acquired := field("payload_acquired"); acquired < 2*runtimeV2ResidentBlockBytes {
		t.Fatalf("payload_acquired = %d, want at least %d:\n%s", acquired, 2*runtimeV2ResidentBlockBytes, line)
	}
	if peak := field("payload_peak"); peak < runtimeV2ResidentBlockBytes {
		t.Fatalf("payload_peak = %d, want at least one %d-byte block:\n%s", peak, runtimeV2ResidentBlockBytes, line)
	}
	for _, kind := range []string{"envelope", "record"} {
		if peak := field(kind + "_peak"); peak == 0 {
			t.Fatalf("%s_peak = 0: a crossing ran and nothing of it was ever resident:\n%s", kind, line)
		}
	}
	if fields, padding := field("envelope_acquired"), field("padding_acquired"); padding > fields {
		t.Fatalf("padding_acquired = %d exceeds envelope_acquired = %d:\n%s", padding, fields, line)
	}
	if total, live := field("peak_total"), field("live_total"); total == 0 || live != 0 {
		t.Fatalf("peak_total = %d, live_total = %d, want a nonzero peak and nothing live:\n%s", total, live, line)
	}
}
