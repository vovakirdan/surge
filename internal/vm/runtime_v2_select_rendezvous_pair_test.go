//go:build runtime_v2_pending

package vm_test

import (
	"strings"
	"testing"
	"time"
)

const selectRendezvousPairSource = `async fn offer(ch: &Channel<string>) -> int {
    let payload = "selector-" + "owned-payload";
    return select { ch.send(own payload) => 31; };
}
async fn pair() -> int {
    let ch = Channel::<string>::new(0:uint);
    let producer = spawn offer(&ch);
    let received = select { ch.recv() => 11; };
    let sent = compare producer.await() { Success(v) => v; Cancelled() => 0; };
    return received == 11 && sent == 31 ? 0 : 1;
}
@entrypoint fn main() -> int {
    return compare pair().await() { Success(v) => v; Cancelled() => 2; };
}
`

func TestRuntimeV2SelectRendezvousBetweenSelectors(t *testing.T) {
	t.Run("vm", func(t *testing.T) {
		t.Setenv(backendEnvVar, backendVM)
		result := runProgramFromSource(t, selectRendezvousPairSource, runOptions{})
		if result.exitCode != 0 || result.stderr != "" {
			t.Fatalf("select rendezvous: exit=%d stderr=%q", result.exitCode, result.stderr)
		}
	})
	t.Run("llvm", func(t *testing.T) {
		binary := buildLLVMProgramFromSource(t, selectRendezvousPairSource)
		for _, shards := range []string{"1", "2", "8"} {
			env := overrideEnvVar(envWithStdlib(repoRoot(t)), "SURGE_SHARDS", shards)
			_, result := runBinaryWithTimeout(t, binary, env, 30*time.Second)
			if result.exitCode != 0 || result.stdout != "" || result.stderr != "" {
				t.Fatalf("select rendezvous at shards=%s: exit=%d stdout=%q stderr=%q", shards, result.exitCode, result.stdout, result.stderr)
			}
		}
		env := overrideEnvVar(envWithStdlib(repoRoot(t)), "SURGE_SHARDS", "2")
		stdout, stderr, code := runBinaryUnderValgrind(t, binary, env, 120*time.Second)
		if code != 0 || stdout != "" || hasValgrindMemcheckError(stderr) || !strings.Contains(stderr, "All heap blocks were freed") {
			t.Fatalf("select rendezvous lifetime: exit=%d stdout=%q\n%s", code, stdout, stderr)
		}
	})
}
