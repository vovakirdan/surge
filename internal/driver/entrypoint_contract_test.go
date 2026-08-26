package driver

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/fix"
)

func TestEntrypointArgvDefaultStillRequiresExactParser(t *testing.T) {
	result, _ := diagnoseEntrypointContract(t, `
type Config = { value: int };
@entrypoint("argv")
fn main(config: Config = Config { value = 1 }) -> int { return config.value; }
`)
	diagnostic := requireEntrypointDiagnostic(t, result, diag.SemaEntrypointParamNoFromArgv)
	if !strings.Contains(diagnostic.Message, `parameter "config"`) {
		t.Fatalf("diagnostic message = %q", diagnostic.Message)
	}
	requireEntrypointNote(t, diagnostic, "public fn Config.from_str(value: &string) -> Erring<Config, Error>")
	if result.HIR != nil {
		t.Fatal("post-merge parser failure must stop before HIR")
	}
}

func TestEntrypointStdinRequiresExactlyOneParameter(t *testing.T) {
	for name, sourceText := range map[string]string{
		"none": `@entrypoint("stdin") fn main() -> int { return 0; }`,
		"two":  `@entrypoint("stdin") fn main(a: string, b: string) -> int { return 0; }`,
	} {
		t.Run(name, func(t *testing.T) {
			result, _ := diagnoseEntrypointContract(t, sourceText)
			diagnostic := requireEntrypointDiagnostic(t, result, diag.SemaEntrypointStdinArity)
			requireEntrypointNote(t, diagnostic, "EOF supplies an empty string")
		})
	}
}

func TestEntrypointStdinDefaultFixIsGuardedAndRediagnoses(t *testing.T) {
	result, path := diagnoseEntrypointContract(t, `
@entrypoint("stdin")
fn main(text: string = "fallback") -> int { return 0; }
fn invoke_default() -> int { return main(); }
`)
	diagnostic := requireEntrypointDiagnostic(t, result, diag.SemaEntrypointStdinDefault)
	if len(diagnostic.Fixes) != 1 {
		t.Fatalf("fixes = %+v", diagnostic.Fixes)
	}
	if diagnostic.Fixes[0].Applicability != diag.FixApplicabilityManualReview || diagnostic.Fixes[0].IsPreferred {
		t.Fatalf("stdin default fix must require manual review: %+v", diagnostic.Fixes[0])
	}
	materialized, err := diag.MaterializeFixes(diag.FixBuildContext{FileSet: result.FileSet}, diagnostic.Fixes)
	if err != nil {
		t.Fatalf("materialize fix: %v", err)
	}
	if len(materialized) != 1 || len(materialized[0].Edits) != 1 {
		t.Fatalf("materialized fixes = %+v", materialized)
	}
	if materialized[0].Applicability != diag.FixApplicabilityManualReview || materialized[0].IsPreferred {
		t.Fatalf("materialized fix must require manual review: %+v", materialized[0])
	}
	edit := materialized[0].Edits[0]
	if edit.OldText != ` = "fallback"` || edit.NewText != "" {
		t.Fatalf("guarded deletion = %+v", edit)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read source before automatic apply: %v", err)
	}
	if _, applyErr := fix.Apply(result.FileSet, result.Bag.Items(), fix.ApplyOptions{Mode: fix.ApplyModeAll}); !errors.Is(applyErr, fix.ErrNoFixes) {
		t.Fatalf("automatic fix application error = %v, want ErrNoFixes", applyErr)
	}
	afterAuto, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read source after automatic apply: %v", err)
	}
	if string(afterAuto) != string(before) {
		t.Fatalf("automatic apply changed a manual-review fix:\n%s", afterAuto)
	}
	if _, applyErr := fix.Apply(result.FileSet, result.Bag.Items(), fix.ApplyOptions{
		Mode: fix.ApplyModeID, TargetID: "entrypoint.remove-stdin-default",
	}); applyErr != nil {
		t.Fatalf("apply fix: %v", applyErr)
	}
	fixed, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixed source: %v", err)
	}
	if strings.Contains(string(fixed), "fallback") {
		t.Fatalf("initializer remains after fix:\n%s", fixed)
	}
	rediagnosed := diagnoseEntrypointPath(t, path)
	for _, item := range rediagnosed.Bag.Items() {
		if item.Code == diag.SemaEntrypointStdinDefault {
			t.Fatalf("stdin default diagnostic remains after fix: %+v", item)
		}
	}
	if !rediagnosed.Bag.HasErrors() {
		t.Fatal("explicitly removing the default should expose the ordinary call that relied on it")
	}
}

func TestEntrypointStdinUsesDistinctPublicExactContract(t *testing.T) {
	t.Run("from_str is not a stdin parser", func(t *testing.T) {
		result, _ := diagnoseEntrypointContract(t, `
type Payload = { value: int };
extern<Payload> {
    pub fn from_str(_text: &string) -> Erring<Payload, Error> {
        return Error { message = "unused", code = 1:uint };
    }
}
@entrypoint("stdin") fn main(payload: Payload) -> int { return payload.value; }
`)
		diagnostic := requireEntrypointDiagnostic(t, result, diag.SemaEntrypointParamNoFromStdin)
		requireEntrypointNote(t, diagnostic, "public fn Payload.from_stdin(text: string) -> Erring<Payload, Error>")
	})

	t.Run("private exact candidate is rejected with declaration note", func(t *testing.T) {
		result, _ := diagnoseEntrypointContract(t, `
type Payload = { value: int };
extern<Payload> {
    fn from_stdin(_text: string) -> Erring<Payload, Error> {
        return Error { message = "private", code = 1:uint };
    }
}
@entrypoint("stdin") fn main(payload: Payload) -> int { return payload.value; }
`)
		diagnostic := requireEntrypointDiagnostic(t, result, diag.SemaEntrypointParamNoFromStdin)
		requireEntrypointNote(t, diagnostic, "is not public")
	})

	t.Run("public exact candidate is accepted", func(t *testing.T) {
		result, _ := diagnoseEntrypointContract(t, `
type Payload = { value: int };
extern<Payload> {
    pub fn from_stdin(_text: string) -> Erring<Payload, Error> {
        return Success(Payload { value = 29 });
    }
}
@entrypoint("stdin") fn main(payload: Payload) -> int { return payload.value; }
`)
		if result.Bag.HasErrors() {
			t.Fatalf("exact public parser diagnostics = %+v", result.Bag.Items())
		}
		if result.HIR == nil {
			t.Fatal("expected HIR after exact public parser resolution")
		}
	})
}

func TestEntrypointArgvRejectsNearMissSignatures(t *testing.T) {
	t.Run("owned string argument", func(t *testing.T) {
		result, _ := diagnoseEntrypointContract(t, `
type Payload = { value: int };
extern<Payload> {
    pub fn from_str(_text: string) -> Erring<Payload, Error> {
        return Error { message = "wrong arg", code = 1:uint };
    }
}
@entrypoint("argv") fn main(payload: Payload) -> int { return payload.value; }
`)
		diagnostic := requireEntrypointDiagnostic(t, result, diag.SemaEntrypointParamNoFromArgv)
		requireEntrypointNote(t, diagnostic, "incompatible parameter types")
	})

	t.Run("non Erring result", func(t *testing.T) {
		result, _ := diagnoseEntrypointContract(t, `
type Payload = { value: int };
extern<Payload> {
    pub fn from_str(_text: &string) -> Payload { return Payload { value = 1 }; }
}
@entrypoint("argv") fn main(payload: Payload) -> int { return payload.value; }
`)
		diagnostic := requireEntrypointDiagnostic(t, result, diag.SemaEntrypointParamNoFromArgv)
		requireEntrypointNote(t, diagnostic, "incompatible result type")
	})
}

func TestEntrypointBuiltinStdinFinalizationSurvivesHIRCombination(t *testing.T) {
	result, _ := diagnoseEntrypointContract(t, `
@entrypoint("stdin") fn main(text: string) -> int { return len(&text) to int; }
`)
	if result.Sema.InstantiationClosure == nil {
		var summary strings.Builder
		for _, item := range result.Bag.Items() {
			summary.WriteString(item.Code.ID() + ":" + item.Message + ";")
			for _, note := range item.Notes {
				summary.WriteString("note=" + note.Msg + ";")
			}
		}
		t.Fatalf("missing finalized closure after diagnosis: identity=%v graphEmpty=%t diagnostics=%s", result.Sema.InstantiationIdentity != nil, result.Sema.InstantiationGraph.IsEmpty(), summary.String())
	}
	if _, err := CombineHIRWithModules(context.Background(), result); err != nil {
		t.Fatalf("combine HIR: %v", err)
	}
	if result.Sema.InstantiationClosure == nil {
		t.Fatal("HIR combination cleared finalized closure")
	}
}

func diagnoseEntrypointContract(t *testing.T, sourceText string) (*DiagnoseResult, string) {
	t.Helper()
	t.Setenv("SURGE_STDLIB", repoRootFromDriverTest(t))
	path := filepath.Join(t.TempDir(), "main.sg")
	if err := os.WriteFile(path, []byte(sourceText), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	return diagnoseEntrypointPath(t, path), path
}

func diagnoseEntrypointPath(t *testing.T, path string) *DiagnoseResult {
	t.Helper()
	result, err := DiagnoseWithOptions(context.Background(), path, &DiagnoseOptions{
		Stage: DiagnoseStageSema, MaxDiagnostics: 20, EmitHIR: true, EmitInstantiations: true,
	})
	if err != nil {
		t.Fatalf("diagnose entrypoint: %v", err)
	}
	return result
}

func requireEntrypointDiagnostic(t *testing.T, result *DiagnoseResult, code diag.Code) *diag.Diagnostic {
	t.Helper()
	for _, diagnostic := range result.Bag.Items() {
		if diagnostic.Code == code {
			return diagnostic
		}
	}
	t.Fatalf("missing %s in %+v", code.ID(), result.Bag.Items())
	return nil
}

func requireEntrypointNote(t *testing.T, diagnostic *diag.Diagnostic, text string) {
	t.Helper()
	for _, note := range diagnostic.Notes {
		if strings.Contains(note.Msg, text) {
			return
		}
	}
	t.Fatalf("missing note %q in %+v", text, diagnostic.Notes)
}

// requireEntrypointHelp asserts an actionable line in the Help channel. A help
// entry that had drifted into Notes would still read the same to a person and
// would no longer be held to the "must be legal here" standard, so the channel
// is part of what is pinned.
func requireEntrypointHelp(t *testing.T, diagnostic *diag.Diagnostic, text string) {
	t.Helper()
	for _, entry := range diagnostic.Help {
		if strings.Contains(entry.Msg, text) {
			return
		}
	}
	t.Fatalf("missing help %q in %+v", text, diagnostic.Help)
}
