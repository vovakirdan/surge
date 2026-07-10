package crossinggate

import (
	"context"
	"path/filepath"
	"sort"
	"testing"

	"surge/internal/diag"
	"surge/internal/driver"
	"surge/internal/sema"
	"surge/internal/symbols"
	"surge/internal/types"
)

func TestCrossingGuardIntegrationFixtures(t *testing.T) {
	t.Setenv("SURGE_STDLIB", repoRoot(t))
	base := filepath.Join(repoRoot(t), "testdata", "golden", "crossing", "integration")

	valid := []struct {
		fixture      string
		backendCodes []string
		assert       func(*testing.T, *driver.DiagnoseResult)
	}{
		{
			fixture:      "_integration_combined_on_spawn_on.sg",
			backendCodes: []string{"FUT7014", "FUT7015"},
			assert:       assertCombinedOnSpawnOnRecords,
		},
		{
			fixture:      "_integration_spawn_on_then_await.sg",
			backendCodes: []string{"FUT7015", "FUT7016"},
			assert:       assertSpawnOnThenAwaitRecords,
		},
		{
			fixture:      "_integration_far_task_direct_call_chain.sg",
			backendCodes: []string{"FUT7014", "FUT7016", "FUT7017"},
			assert:       assertFarTaskDirectCallChainRecords,
		},
		{
			fixture:      "_integration_generic_crossing_sites.sg",
			backendCodes: []string{"FUT7014"},
			assert:       assertGenericCrossingRecords,
		},
		{
			fixture:      "_probe_far_channel_on_and_spawn_on.sg",
			backendCodes: []string{"FUT7014", "FUT7015"},
			assert:       assertFarChannelProbeRecords,
		},
	}

	for _, tc := range valid {
		t.Run("valid/"+tc.fixture, func(t *testing.T) {
			path := filepath.Join(base, "valid", tc.fixture)
			res := diagnoseIntegrationSema(t, path)
			requireNoSemaErrors(t, res)
			tc.assert(t, res)
			assertBackendCodes(t, path, tc.backendCodes)
		})
	}

	invalid := []string{
		"_integration_nested_crossing_rejected.sg",
		"_probe_tcpconn_remote_io_rejected.sg",
		"_probe_tcp_listener_pinned_capture_rejected.sg",
	}
	for _, fixture := range invalid {
		t.Run("invalid/"+fixture, func(t *testing.T) {
			path := filepath.Join(base, "invalid", fixture)
			res := diagnoseIntegrationSema(t, path)
			assertDiagnosticCode(t, res.Bag.Items(), expectedCode(t, path))
			if got := len(res.Sema.CrossingLowering); got != 0 {
				t.Fatalf("rejected fixture emitted %d accepted lowering records", got)
			}
		})
	}
}

func diagnoseIntegrationSema(t *testing.T, path string) *driver.DiagnoseResult {
	t.Helper()
	res, err := driver.Diagnose(context.Background(), path, driver.DiagnoseStageSema, 200)
	if err != nil {
		t.Fatalf("diagnose %s: %v", filepath.Base(path), err)
	}
	if res == nil || res.Bag == nil || res.Sema == nil || res.Symbols == nil {
		t.Fatalf("diagnose %s: missing sema result", filepath.Base(path))
	}
	return res
}

func requireNoSemaErrors(t *testing.T, res *driver.DiagnoseResult) {
	t.Helper()
	for _, d := range res.Bag.Items() {
		if d.Severity == diag.SevError {
			t.Fatalf("unexpected sema error %s: %s", d.Code.ID(), d.Message)
		}
	}
}

func assertBackendCodes(t *testing.T, path string, want []string) {
	t.Helper()
	wantCodes := append([]string(nil), want...)
	sort.Strings(wantCodes)
	for _, backend := range backendStageBackends {
		t.Run(string(backend), func(t *testing.T) {
			diags := diagnoseBackend(t, path, backend)
			gotCodes := diagnosticErrorCodes(diags)
			if !sameStrings(gotCodes, wantCodes) {
				t.Fatalf("backend error diagnostics = %v, want exactly %v", gotCodes, wantCodes)
			}
		})
	}
}

func diagnosticErrorCodes(diags []*diag.Diagnostic) []string {
	codes := make([]string, 0, len(diags))
	for _, d := range diags {
		if d.Severity < diag.SevError {
			continue
		}
		codes = append(codes, d.Code.ID())
	}
	sort.Strings(codes)
	return codes
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func assertDiagnosticCode(t *testing.T, diags []*diag.Diagnostic, want string) {
	t.Helper()
	for _, d := range diags {
		if d.Code.ID() == want {
			return
		}
	}
	t.Fatalf("expected diagnostic %s, got %s", want, summarize(diags))
}

func assertCombinedOnSpawnOnRecords(t *testing.T, res *driver.DiagnoseResult) {
	requireRecordCount(t, res, 2)
	on := requireRecord(t, res, "immediate", sema.CrossingLoweringOnPlacement)
	requireTypeLabel(t, res, on.Destination.Type, "Placement")
	requireTypeLabel(t, res, on.PayloadType, "int")
	requireTypeLabel(t, res, on.ResultType, "TaskResult<int>")

	spawn := requireRecord(t, res, "start", sema.CrossingLoweringSpawnOn)
	requireTypeLabel(t, res, spawn.Destination.Type, "Placement")
	requireTypeLabel(t, res, spawn.PayloadType, "int")
	requireTypeLabel(t, res, spawn.HandleType, "far Task<int>")
	requireCapture(t, res, spawn, "job", sema.CrossingCaptureMoveOwned, sema.CrossingCaptureOwnedShardMovable)
}

func assertSpawnOnThenAwaitRecords(t *testing.T, res *driver.DiagnoseResult) {
	requireRecordCount(t, res, 2)
	spawn := requireRecord(t, res, "run", sema.CrossingLoweringSpawnOn)
	requireTypeLabel(t, res, spawn.HandleType, "far Task<int>")

	await := requireRecord(t, res, "run", sema.CrossingLoweringFarTaskAwait)
	requireTypeLabel(t, res, await.ReceiverType, "far Task<int>")
	requireTypeLabel(t, res, await.ResultType, "TaskResult<int>")
	if !await.ConsumesHandle || symbolName(t, res.Symbols, await.ReceiverSymbol) != "task" {
		t.Fatalf("await receiver not recorded as consumed task handle")
	}
}

func assertFarTaskDirectCallChainRecords(t *testing.T, res *driver.DiagnoseResult) {
	requireRecordCount(t, res, 3)
	requireRecord(t, res, "wait_remote", sema.CrossingLoweringFarTaskAwait)
	requireRecord(t, res, "cancel_remote", sema.CrossingLoweringFarTaskCancel)
	requireRecord(t, res, "direct_on", sema.CrossingLoweringOnPlacement)
	requireMayCross(t, res, "wrap_on")
	requireNoFunctionRecord(t, res, "wrap_on")
}

func assertGenericCrossingRecords(t *testing.T, res *driver.DiagnoseResult) {
	requireRecordCount(t, res, 1)
	rec := requireRecord(t, res, "route", sema.CrossingLoweringOnPlacement)
	requireTypeLabel(t, res, rec.Destination.Type, "Placement")
	requireMayCross(t, res, "use_int")
	requireMayCross(t, res, "use_bool")
	requireNoFunctionRecord(t, res, "use_int")
	requireNoFunctionRecord(t, res, "use_bool")
}

func assertFarChannelProbeRecords(t *testing.T, res *driver.DiagnoseResult) {
	requireRecordCount(t, res, 2)
	on := requireRecord(t, res, "send_job", sema.CrossingLoweringOnFarHandle)
	if on.Destination.Kind != sema.CrossingDestinationFarHandle || !on.Destination.OwnerAnchored {
		t.Fatalf("far channel destination is not owner-anchored")
	}
	if got := symbolName(t, res.Symbols, on.Destination.AnchorSymbol); got != "ch" {
		t.Fatalf("anchor symbol = %q, want ch", got)
	}
	requireCapture(t, res, on, "msg", sema.CrossingCaptureMoveOwned, sema.CrossingCaptureOwnedShardMovable)
	if len(on.RemoteOps) != 1 || on.RemoteOps[0].Method != "send" {
		t.Fatalf("remote ops = %#v, want one send op", on.RemoteOps)
	}

	spawn := requireRecord(t, res, "schedule", sema.CrossingLoweringSpawnOn)
	requireTypeLabel(t, res, spawn.HandleType, "far Task<int>")
	requireCapture(t, res, spawn, "msg", sema.CrossingCaptureMoveOwned, sema.CrossingCaptureOwnedShardMovable)
}

func requireRecordCount(t *testing.T, res *driver.DiagnoseResult, want int) {
	t.Helper()
	if got := len(res.Sema.CrossingLowering); got != want {
		t.Fatalf("record count = %d, want %d", got, want)
	}
}

func requireRecord(t *testing.T, res *driver.DiagnoseResult, fn string, kind sema.CrossingLoweringKind) sema.CrossingLoweringInfo {
	t.Helper()
	fnID := functionID(t, res, fn)
	for _, rec := range res.Sema.CrossingLowering {
		if rec.Function == fnID && rec.Kind == kind {
			return rec
		}
	}
	t.Fatalf("missing record kind %d for function %s", kind, fn)
	return sema.CrossingLoweringInfo{}
}

func requireNoFunctionRecord(t *testing.T, res *driver.DiagnoseResult, fn string) {
	t.Helper()
	fnID := functionID(t, res, fn)
	for _, rec := range res.Sema.CrossingLowering {
		if rec.Function == fnID {
			t.Fatalf("unexpected lowering record kind %d for %s", rec.Kind, fn)
		}
	}
}

func requireMayCross(t *testing.T, res *driver.DiagnoseResult, fn string) {
	t.Helper()
	fnID := functionID(t, res, fn)
	if !res.Sema.FunctionEffects[fnID].MayCross {
		t.Fatalf("%s should be MayCross", fn)
	}
}

func requireCapture(t *testing.T, res *driver.DiagnoseResult, rec sema.CrossingLoweringInfo, name string, mode sema.CrossingCaptureMode, verdict sema.CrossingCaptureVerdict) {
	t.Helper()
	for _, cap := range rec.Captures {
		if symbolName(t, res.Symbols, cap.Symbol) == name {
			if cap.Mode != mode || cap.Verdict != verdict {
				t.Fatalf("capture %s mode/verdict = %d/%d, want %d/%d", name, cap.Mode, cap.Verdict, mode, verdict)
			}
			return
		}
	}
	t.Fatalf("missing capture %s", name)
}

func requireTypeLabel(t *testing.T, res *driver.DiagnoseResult, id types.TypeID, want string) {
	t.Helper()
	if got := types.Label(res.Sema.TypeInterner, id); got != want {
		t.Fatalf("type = %q, want %q", got, want)
	}
}

func functionID(t *testing.T, res *driver.DiagnoseResult, name string) symbols.SymbolID {
	t.Helper()
	for i := 1; i <= res.Symbols.Table.Symbols.Len(); i++ {
		id := symbols.SymbolID(i)
		sym := res.Symbols.Table.Symbols.Get(id)
		if sym != nil && sym.Kind == symbols.SymbolFunction && symbolName(t, res.Symbols, id) == name {
			return id
		}
	}
	t.Fatalf("missing function %s", name)
	return symbols.NoSymbolID
}

func symbolName(t *testing.T, res *symbols.Result, id symbols.SymbolID) string {
	t.Helper()
	sym := res.Table.Symbols.Get(id)
	if sym == nil {
		t.Fatalf("missing symbol %d", id)
	}
	name, ok := res.Table.Strings.Lookup(sym.Name)
	if !ok {
		t.Fatalf("missing name for symbol %d", id)
	}
	return name
}
