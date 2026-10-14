package vm_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/driver"
)

func TestRuntimeV2SyncChannelHelpersRequireAsyncLowering(t *testing.T) {
	ensureLLVMToolchain(t)
	rows := []struct {
		name, source, message string
	}{
		{
			name:    "send",
			message: "Channel.send() requires async context",
			source: `fn send_sync(ch: &Channel<int64>) -> nothing {
    ch.send(7:int64);
    return nothing;
}

async fn body() -> int {
    let ch = Channel::<int64>::new(1:uint);
    let _ = send_sync(&ch);
    return 0;
}

@entrypoint
fn main() -> int {
    return compare body().await() { Success(v) => v; Cancelled() => 9; };
}
`,
		},
		{
			name:    "receive",
			message: "Channel.recv() requires async context",
			source: `fn receive_sync(ch: &Channel<int64>) -> int64 {
    return compare ch.recv() { Some(v) => v; nothing => -1:int64; };
}

async fn body() -> int {
    let ch = Channel::<int64>::new(1:uint);
    let _ = receive_sync(&ch);
    return 0;
}

@entrypoint
fn main() -> int {
    return compare body().await() { Success(v) => v; Cancelled() => 9; };
}
`,
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			requireSuspensionContextDiagnostic(t, row.source, row.message)
			legal := strings.Replace(row.source, "fn send_sync", "async fn send_sync", 1)
			legal = strings.Replace(legal, "fn receive_sync", "async fn receive_sync", 1)
			legal = strings.Replace(legal, "send_sync(&ch);", "send_sync(&ch).await();", 1)
			legal = strings.Replace(legal, "receive_sync(&ch);", "receive_sync(&ch).await();", 1)
			if row.name == "receive" {
				legal = strings.Replace(legal, "let _ = receive_sync", "ch.send(7:int64);\n    let _ = receive_sync", 1)
			}
			for _, backend := range []string{backendVM, backendLLVM} {
				t.Run(backend, func(t *testing.T) {
					t.Setenv(backendEnvVar, backend)
					res := runProgramFromSource(t, legal, runOptions{})
					if res.exitCode != 0 || res.stderr != "" {
						t.Fatalf("legal async helper failed: exit=%d stderr=%q", res.exitCode, res.stderr)
					}
				})
			}
		})
	}
}

func requireSuspensionContextDiagnostic(t *testing.T, sourceCode, message string) {
	t.Helper()
	t.Setenv("SURGE_STDLIB", repoRoot(t))
	path := filepath.Join(t.TempDir(), "context.sg")
	if err := os.WriteFile(path, []byte(sourceCode), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := driver.DiagnoseWithOptions(t.Context(), path, &driver.DiagnoseOptions{
		Stage: driver.DiagnoseStageSema, BaseDir: filepath.Dir(path), MaxDiagnostics: 64,
	})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, d := range result.Bag.Items() {
		if d.Severity < diag.SevError {
			continue
		}
		if d.Code != diag.SemaIntrinsicBadContext || !strings.Contains(d.Message, message) {
			t.Fatalf("unexpected diagnostic: %s %s", d.Code.ID(), d.Message)
		}
		count++
	}
	if count != 1 {
		t.Fatalf("got %d context diagnostics, want one", count)
	}
}

func TestRuntimeV2BlockingBodyRejectsSuspension(t *testing.T) {
	for _, host := range []string{"async fn host()", "@entrypoint fn main()"} {
		t.Run(host, func(t *testing.T) {
			sourceCode := host + ` -> nothing {
    let job = blocking { checkpoint().await(); ret nothing; };
    let _ = job.await();
    return nothing;
}`
			requireSuspensionContextDiagnostic(t, sourceCode, "await can only be used in async context")
		})
	}
}
