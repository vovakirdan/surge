package driver

import (
	"crypto/sha256"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"surge/internal/sema"
	"surge/internal/symbols"
)

type originalGenericFixture struct {
	owner     *DiagnoseResult
	authority *sema.Result
	inputs    returnOriginInputs
	unit      sema.ReturnOriginUnit
}

// A checked dependency retains its original roots without becoming a driver
// execution seed. The standalone sema fixture intentionally seeds its bodies.
func originalGenericSignatureFixture(t *testing.T, text string, escape, dependency bool) originalGenericFixture {
	t.Helper()
	var program *DiagnoseResult
	wantUnits := 1
	if dependency {
		noStd := strings.HasPrefix(text, "pragma module::dep, no_std;\n")
		prelude := strings.HasPrefix(text, "pragma module::dep;\n")
		if (!noStd && !prelude) || escape {
			t.Fatal("PRECONDITION: dependency source lost its module declaration")
		}
		t.Setenv("SURGE_STDLIB", repoRootFromDriverTest(t))
		program = returnOriginModuleSourceFixture(t, true, text)
		wantUnits = 12
		logReturnOriginCallEvidence(t, map[string]any{"stage": "generic_root_before_closure", "source": string(program.File.Content),
			"sha256": sha256.Sum256(program.File.Content), "seeds": program.Sema.InstantiationCallableSeeds,
			"calls": program.Sema.FunctionCallEdges, "closure": program.Sema.InstantiationClosure, "diagnostics": program.Bag.Items()})
		seen := make(map[*moduleRecord]bool)
		for _, record := range program.moduleRecords {
			if record == nil || seen[record] {
				continue
			}
			seen[record] = true
			for _, fileID := range record.FileIDs {
				checked := record.Sema[fileID]
				if checked == nil {
					t.Fatal("PRECONDITION: original module lacks its semantic result")
				}
				logReturnOriginCallEvidence(t, map[string]any{"stage": "generic_module_before_closure", "file_id": fileID,
					"source_file": record.Builder.Files.Get(fileID).Span.File, "diagnostics": record.Bag.Items(),
					"seeds": checked.InstantiationCallableSeeds, "calls": checked.FunctionCallEdges,
					"candidates": checked.CallableCandidates, "roots": checked.InstantiationGraph.Roots(), "closure": checked.InstantiationClosure})
			}
		}
	} else {
		program = returnOriginTypedFixtureWithEscapeEvidence(t, text, escape)
	}
	if err := FinalizeInstantiationClosure(t.Context(), program, 64); err != nil {
		t.Fatalf("PRECONDITION: source closure failed: %v", err)
	}
	inputs, err := collectReturnOriginUnits(program)
	if err != nil || len(inputs.units) != wantUnits || program.Sema.InstantiationIdentity == nil || program.Sema.InstantiationClosure == nil {
		t.Fatalf("PRECONDITION: original owning authority missing: units=%d want=%d error=%v", len(inputs.units), wantUnits, err)
	}
	ownerPath := program.File.Path
	if dependency {
		ownerPath = filepath.Join(filepath.Dir(filepath.Dir(ownerPath)), "dep", "main.sg")
	}
	out := originalGenericFixture{authority: program.Sema, inputs: inputs}
	for _, unit := range inputs.units {
		file := program.FileSet.Get(unit.Builder.Files.Get(unit.FileID).Span.File)
		bag := inputs.bags[file.ID]
		if dependency {
			logReturnOriginCallEvidence(t, map[string]any{"stage": "generic_module_full_input", "source_key": unit.SourceKey,
				"path": file.Path, "source": string(file.Content), "sha256": sha256.Sum256(file.Content), "diagnostics": bag.Items(),
				"publication": unit.Publication, "expr_types": unit.Sema.ExprTypes, "expr_symbols": unit.Symbols.ExprSymbols,
				"symbols": unit.Symbols.Table.Symbols.Data(), "borrows": unit.Sema.Borrows, "binding_types": unit.Sema.BindingTypes})
			if bag.HasErrors() {
				for _, d := range bag.Items() {
					if d != nil {
						t.Logf("PRECONDITION_DIAGNOSTIC source_key=%s diagnostic=%+v", unit.SourceKey, *d)
					}
				}
				t.Fatal("PRECONDITION: complete module input has source diagnostics")
			}
		}
		if file.Path != ownerPath {
			continue
		}
		if out.owner != nil || string(file.Content) != text {
			t.Fatal("PRECONDITION: original source has ambiguous or changed physical ownership")
		}
		out.owner = &DiagnoseResult{FileSet: program.FileSet, File: file, FileID: unit.FileID, Builder: unit.Builder,
			Bag: bag, Symbols: unit.Symbols, Sema: unit.Sema}
		out.unit = unit
	}
	if out.owner == nil {
		t.Fatal("PRECONDITION: full input lacks the original source owner")
	}
	return out
}

func originalGenericSignatureLocal(t *testing.T, unit sema.ReturnOriginUnit, candidate sema.CallableCandidate) symbols.SymbolID {
	t.Helper()
	var found symbols.SymbolID
	for _, identity := range unit.Publication.LocalCallables {
		if identity.BodyKey != candidate.BodyKey || identity.SourceKey != candidate.SourceKey {
			continue
		}
		mapped := identity.Symbol == candidate.Symbol
		if len(unit.Publication.RootToLocalSymbols) != 0 {
			mapped = slices.Contains(unit.Publication.LocalSymbols(candidate.Symbol), identity.Symbol)
		}
		if found.IsValid() || !identity.Symbol.IsValid() || !mapped {
			t.Fatal("PRECONDITION: original declaration lacks unique local/canonical publication")
		}
		found = identity.Symbol
	}
	if !found.IsValid() {
		t.Fatal("PRECONDITION: original declaration is absent from local publication")
	}
	return found
}
