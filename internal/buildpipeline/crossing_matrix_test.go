package buildpipeline

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"surge/internal/diag"
)

// The executable-shaped async crossings (the exact shapes the LLVM capability
// opened) must stay guarded on every non-transport backend: the capability is
// per-(backend, form), not a blanket unlock.
func TestVMAndUnknownBackendsKeepExecutableAsyncFormsGuarded(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	dir := t.TempDir()
	path := filepath.Join(dir, "main.sg")
	source := `
async fn immediate(dst: Placement) -> int {
    return compare on dst {
        ret 1;
    } {
        Success(v) => v;
        Cancelled() => -1;
    };
}

async fn lifecycle(dst: Placement) -> int {
    let task: far Task<int> = spawn on dst {
        ret 2;
    };
    let cancel_target: far Task<int> = spawn on dst {
        ret 3;
    };
    let _ = compare cancel_target.cancel() {
        Success(_) => 0;
        Cancelled() => 0;
    };
    return compare task.await() {
        Success(v) => v;
        Cancelled() => -1;
    };
}

@entrypoint
fn main() -> int {
    return 0;
}
`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	wantCodes := []diag.Code{
		diag.FutOnBackendUnavailable,
		diag.FutSpawnOnBackendUnavailable,
		diag.FutFarTaskAwaitBackendUnavailable,
		diag.FutFarTaskCancelBackendUnavailable,
	}
	for _, backend := range []Backend{BackendVM, Backend("future_backend")} {
		t.Run(string(backend), func(t *testing.T) {
			res, compileErr := Compile(context.Background(), &CompileRequest{
				TargetPath:     path,
				Backend:        backend,
				MaxDiagnostics: 200,
			})
			if compileErr == nil {
				t.Fatal("expected the async executable shapes to stay guarded off LLVM")
			}
			if res.MIR != nil {
				t.Fatal("expected no MIR for a guarded crossing compile")
			}
			if res.Diagnose == nil || res.Diagnose.Bag == nil {
				t.Fatalf("missing diagnostics bag: %v", compileErr)
			}
			diags := res.Diagnose.Bag.Items()
			for _, code := range wantCodes {
				if findDiagnostic(diags, code) == nil {
					t.Errorf("missing %s on %s, got %s", code.ID(), backend, summarizeCodes(diags))
				}
			}
		})
	}
}

// The channel producer stays guarded on every backend until its lowering
// lands: an async, copy-element `channel_on` — the exact shape that will
// become executable — reports its own diagnostic, not a generic one.
func TestChannelOnStaysGuardedOnAllBackends(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	dir := t.TempDir()
	path := filepath.Join(dir, "main.sg")
	source := `
async fn produce(dst: Placement) -> int {
    let ch: far Channel<int> = channel_on::<int>(dst, 4);
    let _ = ch;
    return 0;
}

@entrypoint
fn main() -> int {
    return 0;
}
`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	for _, backend := range []Backend{BackendVM, BackendLLVM, Backend("future_backend")} {
		t.Run(string(backend), func(t *testing.T) {
			res, compileErr := Compile(context.Background(), &CompileRequest{
				TargetPath:     path,
				Backend:        backend,
				MaxDiagnostics: 200,
			})
			if compileErr == nil {
				t.Fatal("expected channel_on to stay guarded before its lowering lands")
			}
			if res.MIR != nil {
				t.Fatal("expected no MIR for a guarded channel_on compile")
			}
			if res.Diagnose == nil || res.Diagnose.Bag == nil {
				t.Fatalf("missing diagnostics bag: %v", compileErr)
			}
			diags := res.Diagnose.Bag.Items()
			if findDiagnostic(diags, diag.FutChannelOnBackendUnavailable) == nil {
				t.Fatalf("missing FUT7018 on %s, got %s", backend, summarizeCodes(diags))
			}
		})
	}
}

// Production LLVM opens the async immediate `on placement` shape: it compiles
// to MIR with no backend-unavailable diagnostic.
func TestLLVMTransportCapabilityOpensAsyncImmediateOn(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	dir := t.TempDir()
	path := filepath.Join(dir, "main.sg")
	source := `
async fn immediate(dst: Placement) -> int {
    return compare on dst {
        ret 1;
    } {
        Success(v) => v;
        Cancelled() => -1;
    };
}

@entrypoint
fn main() -> int {
    return 0;
}
`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	res, compileErr := Compile(context.Background(), &CompileRequest{
		TargetPath:     path,
		Backend:        BackendLLVM,
		MaxDiagnostics: 200,
	})
	if compileErr != nil {
		t.Fatalf("production LLVM async immediate on should compile to MIR: %v", compileErr)
	}
	if res.MIR == nil {
		t.Fatal("production LLVM async immediate on produced no MIR")
	}
	if res.Diagnose != nil && res.Diagnose.Bag != nil {
		if got := findDiagnostic(res.Diagnose.Bag.Items(), diag.FutOnBackendUnavailable); got != nil {
			t.Fatalf("production LLVM capability must suppress FUT7014, got %s", got.Code.ID())
		}
	}
}

// Production LLVM opens the async far-task lifecycle shapes (spawn on +
// await + cancel) together: they compile to MIR with no FUT7015/7016/7017.
func TestLLVMTransportCapabilityOpensAsyncFarTaskLifecycle(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	dir := t.TempDir()
	path := filepath.Join(dir, "main.sg")
	source := `
async fn lifecycle(dst: Placement) -> int {
    let task: far Task<int> = spawn on dst {
        ret 2;
    };
    let cancel_target: far Task<int> = spawn on dst {
        ret 3;
    };
    let _ = compare cancel_target.cancel() {
        Success(_) => 0;
        Cancelled() => 0;
    };
    return compare task.await() {
        Success(v) => v;
        Cancelled() => -1;
    };
}

@entrypoint
fn main() -> int {
    return 0;
}
`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	res, compileErr := Compile(context.Background(), &CompileRequest{
		TargetPath:     path,
		Backend:        BackendLLVM,
		MaxDiagnostics: 200,
	})
	if compileErr != nil {
		t.Fatalf("production LLVM async far-task lifecycle should compile to MIR: %v", compileErr)
	}
	if res.MIR == nil {
		t.Fatal("production LLVM async far-task lifecycle produced no MIR")
	}
	if res.Diagnose != nil && res.Diagnose.Bag != nil {
		for _, code := range []diag.Code{
			diag.FutSpawnOnBackendUnavailable,
			diag.FutFarTaskAwaitBackendUnavailable,
			diag.FutFarTaskCancelBackendUnavailable,
		} {
			if got := findDiagnostic(res.Diagnose.Bag.Items(), code); got != nil {
				t.Fatalf("production LLVM capability must suppress %s, got %s", code.ID(), got.Code.ID())
			}
		}
	}
}
