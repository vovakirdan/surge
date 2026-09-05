package llvm

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A fixture whose registry carries an entry with an EXACT id that differs from
// its resolved value type, and which is cross-clonable. `byte[]` with a string
// next to it reproduces the shape the behaviour corpus first tripped on: the
// descriptor for the exact id named a clone body keyed on the resolved id, and
// that body checked the plan against a descriptor that was never emitted.
const crossKeyingProbeProgram = `
@entrypoint
fn main() -> int {
    let s = "hi";
    let mut a: byte[] = [];
    return 0;
}
`

var (
	descriptorConstantRe = regexp.MustCompile(`^@(__surge_value_ops_type\d+) = constant .*$`)
	descriptorRefRe      = regexp.MustCompile(`@__surge_value_ops_type\d+`)
	definedSymbolRe      = regexp.MustCompile(`^define [^@]*@([A-Za-z0-9_.]+)\(`)
	crossBodyDefineRe    = regexp.MustCompile(`^define [^@]*@((?:cross_clone|cross_clone_walk|cross_move|plan_cross)\.type\d+)\(`)
)

// TestCrossBodiesReferenceOnlyDefinedDescriptors is the invariant the corpus
// found broken by linking: every crossing body a descriptor names must exist,
// and every descriptor constant a crossing body compares its plan against must
// exist too. The bodies are keyed on the EXACT registry id, like move_init,
// because the descriptor that names them is; a body keyed on the RESOLVED id
// serves a descriptor whose constant is named after a different number, and it
// then checks the plan against a symbol nothing defines.
//
// This reads the emitted TEXT, so it fails with the name of the missing symbol
// rather than with llc's line number; the assemble row below is the same claim
// made by the toolchain.
func TestCrossBodiesReferenceOnlyDefinedDescriptors(t *testing.T) {
	ir := emitCrossKeyingProbe(t)
	lines := strings.Split(ir, "\n")

	defined := map[string]bool{}
	for _, line := range lines {
		if m := descriptorConstantRe.FindStringSubmatch(line); m != nil {
			defined["@"+m[1]] = true
		}
		if m := definedSymbolRe.FindStringSubmatch(line); m != nil {
			defined["@"+m[1]] = true
		}
	}

	// Every crossing slot a descriptor binds names a body that is defined.
	crossSlotRe := regexp.MustCompile(`ptr @((?:cross_clone|cross_move|plan_cross)\.type\d+)`)
	for _, line := range lines {
		if !descriptorConstantRe.MatchString(line) {
			continue
		}
		for _, m := range crossSlotRe.FindAllStringSubmatch(line, -1) {
			if !defined["@"+m[1]] {
				t.Errorf("descriptor %q names @%s, which no body defines", line[:strings.Index(line, " =")], m[1])
			}
		}
	}

	// Every descriptor a crossing body compares its plan against is defined.
	body := ""
	for _, line := range lines {
		if m := crossBodyDefineRe.FindStringSubmatch(line); m != nil {
			body = m[1]
			continue
		}
		if line == "}" {
			body = ""
			continue
		}
		if body == "" {
			continue
		}
		for _, ref := range descriptorRefRe.FindAllString(line, -1) {
			if !defined[ref] {
				t.Errorf("crossing body @%s references %s, which is not defined: the body is keyed on a different id than the descriptor that names it", body, ref)
			}
		}
	}
}

// TestEmittedModuleWithCrossBodiesAssembles hands the whole emitted module to
// the toolchain, which is the only judge of whether it links. The text row
// above names WHAT is missing; this row is what the behaviour corpus and every
// crossing valgrind row actually do with the module, and it is the check that
// was skipped when the clone half landed.
func TestEmittedModuleWithCrossBodiesAssembles(t *testing.T) {
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang unavailable")
	}
	ir := emitCrossKeyingProbe(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "module.ll")
	if writeErr := os.WriteFile(path, []byte(ir), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	out, assembleErr := exec.Command(clang, "-x", "ir", "-c", "-o", filepath.Join(dir, "module.o"), path).CombinedOutput()
	if assembleErr != nil {
		t.Fatalf("the emitted module does not assemble: %v\n%s", assembleErr, out)
	}
}

func emitCrossKeyingProbe(t *testing.T) string {
	t.Helper()
	mirMod, result := lowerMIRFromSource(t, crossKeyingProbeProgram)
	if mirMod.Meta == nil || mirMod.Meta.Operations == nil {
		t.Fatal("no operation registry was published")
	}
	ir, err := emitModuleWithDescriptorDefect(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet, defectNone)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	return ir
}
