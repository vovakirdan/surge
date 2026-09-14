package sema

import (
	"crypto/sha256"
	"encoding/json"
	"slices"
	"testing"

	"surge/internal/types"
)

// Backend pointers have no checked borrow lifetime. A missing source proof for
// an actual reference remains unresolved; these are different source contracts.
func TestReturnOriginPointerDeclarationHasNoBorrowedResult(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		kind types.Kind
	}{
		{"backend_pointer", "@intrinsic fn rt_alloc(size: uint, align: uint) -> *uint8;\n", types.KindPointer},
		{"incoming_reference", "fn keep(value: &int64) -> &int64 { return value; }\n", types.KindReference},
		{"unproved_reference", "fn opaque() -> &int64;\n", types.KindReference},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("RETURN_ORIGIN_POINTER_SOURCE case=%s sha256=%x source=%q", tc.name, sha256.Sum256([]byte(tc.src)), tc.src)
			result, unit := returnOriginPublicationFixture(t, tc.src, false)
			file := unit.Builder.Files.Get(unit.FileID)
			if file == nil || len(file.Items) != 1 || len(unit.Symbols.ItemSymbols[file.Items[0]]) != 1 {
				t.Fatal("PRECONDITION: expected one original typed declaration")
			}
			id := unit.Symbols.ItemSymbols[file.Items[0]][0]
			sym := unit.Symbols.Table.Symbols.Get(id)
			if sym == nil || sym.Signature == nil {
				t.Fatal("PRECONDITION: missing original function symbol")
			}
			info, ok := result.TypeInterner.FnInfo(sym.Type)
			if !ok || info == nil {
				t.Fatal("PRECONDITION: missing original function descriptor")
			}
			ret, ok := result.TypeInterner.Lookup(info.Result)
			if !ok || ret.Kind != tc.kind {
				t.Fatalf("PRECONDITION: source result kind=%v, want %v", ret.Kind, tc.kind)
			}
			analysis, err := AnalyzeReturnOrigins(t.Context(), result, []ReturnOriginUnit{unit})
			evidence, marshalErr := json.Marshal(map[string]any{"symbol": sym, "fn_info": info, "result_type": ret,
				"publication": unit.Publication, "analysis": analysis})
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			t.Logf("RETURN_ORIGIN_POINTER_EVIDENCE=%s error=%v", evidence, err)
			if err != nil || analysis == nil {
				t.Fatalf("PRECONDITION: source analysis could not run: %v", err)
			}
			if len(analysis.Diagnostics) != 0 {
				t.Fatalf("source acquired an unexpected refusal: %+v", analysis.Diagnostics)
			}
			if tc.name == "unproved_reference" {
				if analysis.Complete() || len(analysis.Pending) == 0 || len(analysis.Summaries) != 0 {
					t.Fatalf("opaque reference without an input source was treated as proven: %+v", analysis)
				}
				return
			}
			if !analysis.Complete() {
				t.Fatalf("known source contract acquired an invented obligation: %+v", analysis)
			}
			if tc.name == "backend_pointer" {
				if len(analysis.Summaries) != 0 {
					t.Fatal("bodyless pointer declaration invented a body summary")
				}
				return
			}
			if len(analysis.Summaries) != 1 || analysis.Summaries[0].Name != "keep" ||
				analysis.Summaries[0].Unknown || analysis.Summaries[0].NoNormalReturn ||
				!slices.Equal(analysis.Summaries[0].ParamSlots, []uint32{0}) {
				t.Fatalf("actual incoming reference source was lost: %+v", analysis.Summaries)
			}
		})
	}
}
