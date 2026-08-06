package driver

import (
	"context"
	"sort"
	"strings"
	"testing"

	"surge/internal/sema"
	"surge/internal/types"
)

// capabilityProbeFiles is one multi-module program holding a type for every
// capability question, so the two whole-program paths can be asked the same
// things.
func capabilityProbeFiles() map[string]string {
	return map[string]string{
		"model/value.sg": `
pragma module::model;
pub type Model = { text: string }
extern<Model> {
    fn __clone(self: &Model) -> Model {
        return Model { text = clone(&self.text) };
    }
}
pub fn duplicate(value: &Model) -> Model { return clone(value); }
@shard_movable pub type Payload = { id: int, label: string }
@shard_pinned pub type Pinned = { id: int }
@copy pub type Plain = { x: int, y: int }
`,
		"main.sg": `
pragma module::app;
import model::{Model, Payload, Pinned, Plain, duplicate};
@entrypoint fn main() -> int {
    let value = Model { text = "seed" };
    let copied = duplicate(&value);
    let load = Payload { id = 1, label = "name" };
    let pin = Pinned { id = 2 };
    let flat = Plain { x = 1, y = 2 };
    return 0;
}
`,
	}
}

var capabilityProbeNames = []string{"Model", "Payload", "Pinned", "Plain"}

// capabilityBuildAuthority runs the build path's whole-program pre-pass, which
// is the step CombineHIRWithModulesWithOptions performs for buildpipeline.
func capabilityBuildAuthority(t *testing.T, files map[string]string) *DiagnoseResult {
	t.Helper()
	result := diagnoseCloneProject(t, files)
	if _, err := CombineHIRWithModulesWithOptions(context.Background(), result, HIRCombineOptions{}); err != nil {
		t.Fatalf("combine HIR with modules: %v", err)
	}
	return result
}

// capabilityEvidence renders one classifier's answer for every probe type the
// result's interner holds, keyed by the type's name.
//
// The rendering names types by label rather than by interner id on purpose: ids
// depend on the order types happened to be created in, which differs between
// the two paths and says nothing about what either one decided.
func capabilityEvidence(t *testing.T, res *sema.Result) map[string]string {
	t.Helper()
	if res == nil || res.Capabilities == nil || res.TypeInterner == nil || res.TypeInterner.Strings == nil {
		return nil
	}
	out := make(map[string]string, len(capabilityProbeNames))
	for _, name := range capabilityProbeNames {
		id, ok := res.TypeInterner.FindStructInstance(res.TypeInterner.Strings.Intern(name), nil)
		if !ok || id == types.NoTypeID {
			continue
		}
		rendered, err := res.Capabilities.Describe(id)
		if err != nil {
			t.Fatalf("describe %s: %v", name, err)
		}
		out[name] = rendered
	}
	return out
}

// TestCapabilityClassificationRunsAtTheBuildAuthority pins that the build
// path's pre-pass classifies, and that what it produces is the merged
// whole-program view: every probe type is declared in an imported module, so a
// file-local answer would have nothing to say about any of them.
func TestCapabilityClassificationRunsAtTheBuildAuthority(t *testing.T) {
	result := capabilityBuildAuthority(t, capabilityProbeFiles())
	requireNoCloneErrors(t, result)
	if result.Sema == nil || result.Sema.Capabilities == nil {
		t.Fatal("the whole-program pre-pass produced no capability authority")
	}
	evidence := capabilityEvidence(t, result.Sema)
	for _, name := range capabilityProbeNames {
		if _, ok := evidence[name]; !ok {
			t.Fatalf("no capability evidence for %s: %v", name, evidence)
		}
	}
	if !strings.Contains(evidence["Payload"], "shard-movable=true") {
		t.Fatalf("Payload is `@shard_movable` and classified as %q", evidence["Payload"])
	}
	if !strings.Contains(evidence["Pinned"], "shard-movable=false") {
		t.Fatalf("Pinned is `@shard_pinned` and classified as %q", evidence["Pinned"])
	}
	if !strings.Contains(evidence["Model"], "clone=valid-method") {
		t.Fatalf("Model has a `__clone` and classified as %q", evidence["Model"])
	}
	if !strings.Contains(evidence["Plain"], "copy=true") {
		t.Fatalf("Plain is `@copy` and classified as %q", evidence["Plain"])
	}
}

// TestCapabilityClassificationRunsWithoutAnyModuleRecords is the site-identity
// point. A single-file program reaches the same authority site holding no
// module records at all, and it is still the whole program: with nothing
// imported, its own facts are already complete. Gating classification on record
// presence would skip exactly this build and leave it with no answers, or with
// a set of false ones, which is the same failure wearing a better face.
func TestCapabilityClassificationRunsWithoutAnyModuleRecords(t *testing.T) {
	result := capabilityBuildAuthority(t, map[string]string{
		"main.sg": `
@shard_pinned type Pinned = { id: int }
@entrypoint fn main() -> int {
    let pin = Pinned { id = 1 };
    return 0;
}
`,
	})
	if result.rootRecord != nil || len(result.moduleRecords) != 0 {
		t.Skip("this build grew module records, so it no longer covers the no-records shape")
	}
	if result.Sema == nil || result.Sema.Capabilities == nil {
		t.Fatal("a single-file build reached the whole-program site and produced no capability authority")
	}
	id, ok := result.Sema.TypeInterner.FindStructInstance(result.Sema.TypeInterner.Strings.Intern("Pinned"), nil)
	if !ok {
		t.Fatal("Pinned is absent from its own build")
	}
	capability, err := result.Sema.Capabilities.Classify(id)
	if err != nil {
		t.Fatalf("classify Pinned: %v", err)
	}
	if capability.ShardMovable {
		t.Fatalf("a `@shard_pinned` type classified as movable: %q", capability.ShardReason)
	}
}

// TestCapabilityClassificationSkipsThePerFilePath pins the other half of the
// site-identity rule: the per-file finalization path holds one file's view and
// must emit no capability verdicts from it.
//
// Files carrying no module pragma are what reaches that path — a file that
// declares a module is finalized through its module's record instead, at the
// authority site, which is why this fixture declares none.
func TestCapabilityClassificationSkipsThePerFilePath(t *testing.T) {
	root := writeCloneProject(t, map[string]string{
		"a.sg": `
@shard_pinned type Pinned = { id: int }
fn helper() -> int {
    let pin = Pinned { id = 1 };
    return 0;
}
`,
		"b.sg": `
@entrypoint fn main() -> int { return 0; }
`,
	})
	_, results, err := DiagnoseDirWithOptions(context.Background(), root, &DiagnoseOptions{
		Stage: DiagnoseStageAll, MaxDiagnostics: 64, FullModuleGraph: false, KeepArtifacts: true,
	}, 1)
	if err != nil {
		t.Fatalf("diagnose project directory per file: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("the per-file path produced no results")
	}
	for i := range results {
		if results[i].Sema != nil && results[i].Sema.Capabilities != nil {
			t.Fatalf("the per-file path classified %s from one file's view", results[i].Path)
		}
	}
}

// TestCapabilityBuildAndDiagnoseAgree is the parity test. One program taken
// through the build path's pre-pass and through the parallel diagnose path's
// module-record authority must reach the same verdicts: a capability is a
// property of the program, so two ways of compiling it that disagreed would
// mean one of them is wrong with nothing to say which.
//
// Consumers arrive in later commits, so parity here is the classifier's own
// output for a probe set of types.
func TestCapabilityBuildAndDiagnoseAgree(t *testing.T) {
	files := capabilityProbeFiles()
	built := capabilityEvidence(t, capabilityBuildAuthority(t, files).Sema)
	if len(built) != len(capabilityProbeNames) {
		t.Fatalf("the build path answered for %d of %d probes: %v",
			len(built), len(capabilityProbeNames), built)
	}

	root := writeCloneProject(t, files)
	_, results, err := DiagnoseDirWithOptions(context.Background(), root, &DiagnoseOptions{
		Stage: DiagnoseStageAll, MaxDiagnostics: 64, FullModuleGraph: true, KeepArtifacts: true,
	}, 1)
	if err != nil {
		t.Fatalf("diagnose project directory: %v", err)
	}

	answered := 0
	for i := range results {
		diagnosed := capabilityEvidence(t, results[i].Sema)
		if len(diagnosed) == 0 {
			continue
		}
		answered++
		for _, name := range sortedProbeNames(diagnosed) {
			if diagnosed[name] != built[name] {
				t.Errorf("%s classifies %s differently:\n  build    %s\n  diagnose %s",
					results[i].Path, name, built[name], diagnosed[name])
			}
		}
	}
	if answered == 0 {
		t.Fatal("the parallel diagnose path produced no capability authority to compare against")
	}
}

// TestCapabilityAuthorityIsSharedAcrossModules pins the shape the parity test
// found: the module-record path finalizes one module at a time, and each
// module's aggregate carries only its own callable catalog. A `__clone` private
// to `model` is absent from `app`'s list, so a classifier built per module
// called Model clonable while finalizing model and non-clonable while
// finalizing app. Every module gets the same authority, built from every
// record, so the question cannot come back.
func TestCapabilityAuthorityIsSharedAcrossModules(t *testing.T) {
	root := writeCloneProject(t, capabilityProbeFiles())
	_, results, err := DiagnoseDirWithOptions(context.Background(), root, &DiagnoseOptions{
		Stage: DiagnoseStageAll, MaxDiagnostics: 64, FullModuleGraph: true, KeepArtifacts: true,
	}, 1)
	if err != nil {
		t.Fatalf("diagnose project directory: %v", err)
	}

	var shared *sema.CapabilityClassifier
	modules := 0
	for i := range results {
		if results[i].Sema == nil || results[i].Sema.Capabilities == nil {
			continue
		}
		modules++
		if shared == nil {
			shared = results[i].Sema.Capabilities
			continue
		}
		if results[i].Sema.Capabilities != shared {
			t.Fatalf("%s finalized against its own capability authority", results[i].Path)
		}
	}
	if modules < 2 {
		t.Fatalf("only %d module aggregate carried an authority, so sharing is untested", modules)
	}
	evidence := capabilityEvidence(t, results[0].Sema)
	if !strings.Contains(evidence["Model"], "clone=valid-method") {
		t.Fatalf("a module-private `__clone` was invisible to the shared authority: %q", evidence["Model"])
	}
}

func sortedProbeNames(evidence map[string]string) []string {
	out := make([]string, 0, len(evidence))
	for name := range evidence {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
