package buildpipeline

import (
	"context"
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
