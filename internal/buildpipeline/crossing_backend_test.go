package buildpipeline

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"surge/internal/diag"
)

func testRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("buildpipeline: cannot locate test source file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func compileFixtureDiagnostics(t *testing.T, fixture string, backend Backend) []*diag.Diagnostic {
	t.Helper()
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	res, err := Compile(context.Background(), &CompileRequest{
		TargetPath:     filepath.Join(testRepoRoot(t), fixture),
		Backend:        backend,
		MaxDiagnostics: 200,
	})
	if res.Diagnose == nil || res.Diagnose.Bag == nil {
		if err != nil {
			t.Fatalf("compile %s with backend %q: %v", fixture, backend, err)
		}
		return nil
	}
	return res.Diagnose.Bag.Items()
}

func findDiagnostic(diags []*diag.Diagnostic, code diag.Code) *diag.Diagnostic {
	for _, d := range diags {
		if d.Code == code {
			return d
		}
	}
	return nil
}

func TestCrossingBackendUnavailableMessages(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		code    diag.Code
		message string
	}{
		{
			name:    "on placement",
			fixture: "testdata/golden/crossing/block02/invalid/_on_negative_backend_unavailable.sg",
			code:    diag.FutOnBackendUnavailable,
			message: "`on` placement crossing cannot be executed: no available backend supports cross-shard transport",
		},
		{
			name:    "spawn on",
			fixture: "testdata/golden/crossing/block03/invalid/_spawn_on_negative_backend_unavailable.sg",
			code:    diag.FutSpawnOnBackendUnavailable,
			message: "`spawn on` remote spawn cannot be executed: no available backend supports cross-shard transport",
		},
		{
			name:    "far task await",
			fixture: "testdata/golden/crossing/block03/invalid/_spawn_on_negative_await_backend_unavailable.sg",
			code:    diag.FutFarTaskAwaitBackendUnavailable,
			message: "`far Task<T>.await()` cannot be executed: no available backend supports remote task transport",
		},
		{
			name:    "far task cancel",
			fixture: "testdata/golden/crossing/block03/invalid/_spawn_on_negative_cancel_backend_unavailable.sg",
			code:    diag.FutFarTaskCancelBackendUnavailable,
			message: "`far Task<T>.cancel()` cannot be executed: no available backend supports remote task transport",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diags := compileFixtureDiagnostics(t, tc.fixture, BackendVM)
			got := findDiagnostic(diags, tc.code)
			if got == nil {
				t.Fatalf("expected %s, got %s", tc.code.ID(), summarizeCodes(diags))
			}
			if got.Message != tc.message {
				t.Fatalf("message mismatch:\nwant: %s\n got: %s", tc.message, got.Message)
			}
			if strings.Contains(got.Message, "Phase") || strings.Contains(got.Message, "Epic") {
				t.Fatalf("message leaks internal planning wording: %q", got.Message)
			}
		})
	}
}

func TestCrossingBackendGuardsAreDefaultClosed(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		code    diag.Code
	}{
		{
			name:    "on placement",
			fixture: "testdata/golden/crossing/block02/invalid/_on_negative_backend_unavailable.sg",
			code:    diag.FutOnBackendUnavailable,
		},
		{
			name:    "spawn on",
			fixture: "testdata/golden/crossing/block03/invalid/_spawn_on_negative_backend_unavailable.sg",
			code:    diag.FutSpawnOnBackendUnavailable,
		},
		{
			name:    "far task await",
			fixture: "testdata/golden/crossing/block03/invalid/_spawn_on_negative_await_backend_unavailable.sg",
			code:    diag.FutFarTaskAwaitBackendUnavailable,
		},
		{
			name:    "far task cancel",
			fixture: "testdata/golden/crossing/block03/invalid/_spawn_on_negative_cancel_backend_unavailable.sg",
			code:    diag.FutFarTaskCancelBackendUnavailable,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diags := compileFixtureDiagnostics(t, tc.fixture, Backend("future_backend"))
			if got := findDiagnostic(diags, tc.code); got == nil {
				t.Fatalf("expected default-closed %s, got %s", tc.code.ID(), summarizeCodes(diags))
			}
		})
	}
}

func TestCrossingBackendGuardDoesNotMaskSemaErrors(t *testing.T) {
	cases := []struct {
		name       string
		fixture    string
		semaCode   diag.Code
		futureCode diag.Code
	}{
		{
			name:       "on",
			fixture:    "testdata/golden/crossing/block02/invalid/on_negative_integer_destination.sg",
			semaCode:   diag.SemaOnDestNotPlacement,
			futureCode: diag.FutOnBackendUnavailable,
		},
		{
			name:       "spawn on",
			fixture:    "testdata/golden/crossing/block03/invalid/spawn_on_negative_nonplacement_literal.sg",
			semaCode:   diag.SemaSpawnOnDestNotPlacement,
			futureCode: diag.FutSpawnOnBackendUnavailable,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diags := compileFixtureDiagnostics(t, tc.fixture, BackendVM)
			if got := findDiagnostic(diags, tc.semaCode); got == nil {
				t.Fatalf("expected semantic diagnostic %s, got %s", tc.semaCode.ID(), summarizeCodes(diags))
			}
			if got := findDiagnostic(diags, tc.futureCode); got != nil {
				t.Fatalf("unexpected backend-unavailable diagnostic on sema-invalid crossing: %s", got.Code.ID())
			}
		})
	}
}

func TestCrossingBackendGuardsCoverImportedModules(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))

	dir, err := os.MkdirTemp(".", "rv2-crossing-xmod-")
	if err != nil {
		t.Fatalf("mkdir temp project: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	remoteDir := filepath.Join(dir, "remote")
	if mkdirErr := os.MkdirAll(remoteDir, 0o755); mkdirErr != nil {
		t.Fatalf("mkdir remote module: %v", mkdirErr)
	}
	writeFile := func(path, body string) {
		t.Helper()
		if writeErr := os.WriteFile(path, []byte(body), 0o600); writeErr != nil {
			t.Fatalf("write %s: %v", path, writeErr)
		}
	}
	writeFile(filepath.Join(remoteDir, "remote.sg"), `
pragma module::remote;

pub fn remote_score() -> TaskResult<int> {
	return on pool {
		ret 7;
	};
}

pub fn start_remote() -> far Task<int> {
	return spawn on pool {
		ret 41;
	};
}

pub fn cancel_remote() -> TaskResult<nothing> {
	let task: far Task<int> = spawn on pool {
		ret 1;
	};
	return task.cancel();
}
`)
	mainPath := filepath.Join(dir, "main.sg")
	writeFile(mainPath, `
import remote::remote_score;
import remote::start_remote;
import remote::cancel_remote;

fn caller() -> TaskResult<int> {
	let _score = remote_score();
	let _cancelled = cancel_remote();
	let task = start_remote();
	return task.await();
}
`)

	wantCodes := []diag.Code{
		diag.FutOnBackendUnavailable,
		diag.FutSpawnOnBackendUnavailable,
		diag.FutFarTaskAwaitBackendUnavailable,
		diag.FutFarTaskCancelBackendUnavailable,
	}
	for _, backend := range []Backend{BackendVM, BackendLLVM, Backend("future_backend")} {
		t.Run(string(backend), func(t *testing.T) {
			res, compileErr := Compile(context.Background(), &CompileRequest{
				TargetPath:     mainPath,
				Backend:        backend,
				MaxDiagnostics: 200,
			})
			if compileErr == nil {
				t.Fatal("expected compile to stop on imported crossing constructs before lowering")
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
					t.Errorf("missing %s for imported crossing surface, got %s", code.ID(), summarizeCodes(diags))
				}
			}
		})
	}
}

func summarizeCodes(diags []*diag.Diagnostic) string {
	if len(diags) == 0 {
		return "(no diagnostics)"
	}
	codes := make([]string, 0, len(diags))
	for _, d := range diags {
		codes = append(codes, d.Code.ID())
	}
	return strings.Join(codes, ", ")
}
