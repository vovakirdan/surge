package vm_test

import "testing"

func TestUserFunctionsNamedLikeChannelIntrinsics(t *testing.T) {
	const source = `async fn send(value: int) -> int { return value + 1; }
fn recv(value: int) -> int { return value + 1; }
@entrypoint fn main() -> int {
    return compare send(40).await() {
        Success(value) => recv(value) == 42 ? 0 : 1;
        Cancelled() => 2;
    };
}`
	for _, backend := range []string{backendVM, backendLLVM} {
		t.Run(backend, func(t *testing.T) {
			t.Setenv(backendEnvVar, backend)
			result := runProgramFromSource(t, source, runOptions{})
			if result.exitCode != 0 || result.stderr != "" {
				t.Fatalf("source function was not called: exit=%d stderr=%q", result.exitCode, result.stderr)
			}
		})
	}
}
