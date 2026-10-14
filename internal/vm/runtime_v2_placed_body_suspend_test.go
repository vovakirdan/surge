//go:build runtime_v2_pending

package vm_test

import (
	"strings"
	"testing"
	"time"
)

const placedBodySuspendSource = `async fn inspect(value: &string) -> uint {
    checkpoint().await();
    return value.__len();
}
@entrypoint fn main() -> int {
    let seed: int = 9223372036854775808;
    START {
        print("placed-begin");
        let local = "stable placed storage";
        let child = spawn inspect(&local);
        let value = seed + 1;
        checkpoint().await();
        checkpoint().await();
        let n = compare child.await() { Success(v) => v; Cancelled() => 0:uint; };
        print("placed-end");
        ret n == local.__len() ? value : 0;
    };
    FINISH
    if seed != 9223372036854775808 { return 3; };
    return compare result {
        Success(v) => v == 9223372036854775809 ? 0 : 1;
        Cancelled() => 2;
    };
}
`

func TestRuntimeV2PlacedBodySuspendsWithLocalBorrow(t *testing.T) {
	for _, row := range []struct{ name, start, finish string }{
		{"spawn", "let task = spawn on distributed", "let result = task.await();"},
		{"immediate", "let result = on distributed", ""},
	} {
		t.Run(row.name, func(t *testing.T) {
			source := strings.ReplaceAll(placedBodySuspendSource, "START", row.start)
			source = strings.ReplaceAll(source, "FINISH", row.finish)
			binary := buildLLVMProgramFromSource(t, source)
			for _, shards := range []string{"1", "2", "8"} {
				env := overrideEnvVar(envWithStdlib(repoRoot(t)), "SURGE_SHARDS", shards)
				_, result := runBinaryWithTimeout(t, binary, env, 30*time.Second)
				if result.exitCode != 0 || result.stdout != "placed-begin\nplaced-end\n" || result.stderr != "" {
					t.Fatalf("placed body at shards=%s: exit=%d stdout=%q stderr=%q", shards, result.exitCode, result.stdout, result.stderr)
				}
			}
			env := overrideEnvVar(envWithStdlib(repoRoot(t)), "SURGE_SHARDS", "2")
			stdout, stderr, code := runBinaryUnderValgrind(t, binary, env, 120*time.Second)
			if code != 0 || stdout != "placed-begin\nplaced-end\n" || hasValgrindMemcheckError(stderr) || !strings.Contains(stderr, "All heap blocks were freed") {
				t.Fatalf("placed frame lifetime: exit=%d stdout=%q\n%s", code, stdout, stderr)
			}
		})
	}
}
