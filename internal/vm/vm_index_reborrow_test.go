package vm_test

import (
	"os"
	"strings"
	"testing"

	"surge/internal/mir"
)

const customIndexSharedReborrowSource = `type Bag = {
    values: int[],
};

extern<Bag> {
    pub fn __index(self: &Bag, index: int) -> &int {
        return self.values[index];
    }
}

@entrypoint
fn main() -> int {
    let mut values: Map<string, int> = Map::<string, int>.new();
    let _ = values.insert("key", 7);
    let map_ref: &int = &values["key"];
    let bag: Bag = Bag { values = [5] };
    let bag_ref: &int = &bag[0];
    let fixed: int[2] = [3, 4];
    let fixed_ref: &int = &fixed[1];
    return *map_ref + *bag_ref + *fixed_ref - 16;
}
`

const customIndexAliasCarrierSource = `type Bag = { marker: int };
type IntRef = &int;

extern<Bag> {
    fn __index(self: &Bag, index: IntRef) -> IntRef {
        let _ = self;
        return index;
    }
}

fn read(bag: &Bag, index: IntRef) -> int {
    let value: &int = &bag[index];
    return *value;
}
`

func TestVMRefsCustomIndexSharedReborrowVMAndLLVM(t *testing.T) {
	for _, backend := range []string{backendVM, backendLLVM} {
		t.Run(backend, func(t *testing.T) {
			t.Setenv(backendEnvVar, backend)
			result := runProgramFromSource(t, customIndexSharedReborrowSource, runOptions{})
			if result.exitCode != 0 {
				t.Fatalf("%s exit code = %d, want 0\nstderr:\n%s\ndiagnostics:\n%s", backend, result.exitCode, result.stderr, result.diagnostics)
			}
			if result.stderr != "" {
				t.Fatalf("%s stderr is not empty:\n%s", backend, result.stderr)
			}
		})
	}
}

func TestVMRefsCustomIndexSharedReborrowKeepsExactCallsInMIR(t *testing.T) {
	mirMod, _, _ := compileToMIRFromSource(t, customIndexSharedReborrowSource)

	callResults := make(map[mir.LocalID]string, 2)
	var mainFn *mir.Func
	for _, fn := range mirMod.Funcs {
		if fn != nil && fn.Name == "main" {
			mainFn = fn
			break
		}
	}
	if mainFn == nil {
		t.Fatal("missing main in MIR")
	}
	for _, bb := range mainFn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Kind != mir.InstrCall || !strings.HasPrefix(instr.Call.Callee.Name, "__index") {
				continue
			}
			if instr.Call.Callee.Kind != mir.CalleeSym || !instr.Call.Callee.Sym.IsValid() {
				t.Fatalf("__index lost exact selected symbol: %+v", instr.Call.Callee)
			}
			if !instr.Call.HasDst {
				t.Fatalf("reference-returning __index has no destination: %+v", instr.Call)
			}
			callResults[instr.Call.Dst.Local] = instr.Call.Callee.Name
		}
	}
	if len(callResults) != 2 {
		t.Fatalf("exact __index call results = %v, want Map and custom Bag calls", callResults)
	}

	reborrowed := make(map[mir.LocalID]bool, len(callResults))
	for _, bb := range mainFn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Kind != mir.InstrAssign || instr.Assign.Src.Kind != mir.RValueUse {
				continue
			}
			use := instr.Assign.Src.Use
			if use.Kind != mir.OperandAddrOf {
				continue
			}
			if _, ok := callResults[use.Place.Local]; !ok {
				continue
			}
			if len(use.Place.Proj) == 0 || use.Place.Proj[len(use.Place.Proj)-1].Kind != mir.PlaceProjDeref {
				t.Fatalf("__index reborrow does not dereference the carrier: %+v", use.Place)
			}
			reborrowed[use.Place.Local] = true
		}
	}
	for local, name := range callResults {
		if !reborrowed[local] {
			t.Fatalf("%s result L%d was not reborrowed as addr_of (*L%d)", name, local, local)
		}
	}
}

func TestVMRefsCustomIndexAliasCarrierReborrowsPointeeInMIR(t *testing.T) {
	mirMod, _, _ := compileToMIRFromSource(t, customIndexAliasCarrierSource)

	var readFn *mir.Func
	for _, fn := range mirMod.Funcs {
		if fn != nil && fn.Name == "read" {
			readFn = fn
			break
		}
	}
	if readFn == nil {
		t.Fatal("missing read in MIR")
	}
	for _, bb := range readFn.Blocks {
		for _, instr := range bb.Instrs {
			if instr.Kind != mir.InstrCall || !strings.HasPrefix(instr.Call.Callee.Name, "__index") || !instr.Call.HasDst {
				continue
			}
			if instr.Call.Callee.Kind != mir.CalleeSym || !instr.Call.Callee.Sym.IsValid() {
				t.Fatalf("aliased __index carrier lost its exact selected symbol: %+v", instr.Call.Callee)
			}
			for _, useBB := range readFn.Blocks {
				for _, useInstr := range useBB.Instrs {
					if useInstr.Kind != mir.InstrAssign || useInstr.Assign.Src.Kind != mir.RValueUse {
						continue
					}
					use := useInstr.Assign.Src.Use
					if use.Kind == mir.OperandAddrOf && use.Place.Local == instr.Call.Dst.Local && len(use.Place.Proj) > 0 && use.Place.Proj[len(use.Place.Proj)-1].Kind == mir.PlaceProjDeref {
						return
					}
				}
			}
			t.Fatalf("aliased __index carrier L%d was not reborrowed as addr_of (*L%d)", instr.Call.Dst.Local, instr.Call.Dst.Local)
		}
	}
	t.Fatal("missing exact __index call in read MIR")
}

func TestVMRefsCustomIndexSharedCarrierRejectsMutableBorrowBeforeBackend(t *testing.T) {
	const sourceCode = `@entrypoint
fn main() -> int {
    let mut values: Map<string, int> = Map::<string, int>.new();
    let _ = values.insert("key", 7);
    let value: &mut int = &mut values["key"];
    return *value;
}
`
	root := repoRoot(t)
	artifacts := newTestArtifacts(t, root)
	sourcePath := artifactSourcePath(artifacts)
	if err := os.WriteFile(sourcePath, []byte(sourceCode), 0o600); err != nil {
		t.Fatalf("write negative source: %v", err)
	}
	surge := buildSurgeBinary(t, root)

	cases := []struct {
		name string
		args []string
	}{
		{name: backendVM, args: []string{"run", "--backend=vm", sourcePath}},
		{name: backendLLVM, args: []string{"build", sourcePath}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, exitCode := runSurgeWithInput(t, root, surge, "", tc.args...)
			if exitCode == 0 {
				t.Fatalf("%s unexpectedly accepted mutable borrow through shared __index", tc.name)
			}
			output := stdout + stderr
			if !strings.Contains(output, "selected `__index` returns shared &int") {
				t.Fatalf("%s did not report the friendly shared-carrier diagnostic:\n%s", tc.name, output)
			}
			if strings.Contains(output, "LLVM emit failed") || strings.Contains(output, "index projection on non-array type") {
				t.Fatalf("%s reached a backend after the sema error:\n%s", tc.name, output)
			}
		})
	}
}
