package buildpipeline

import (
	"context"
	"strings"
	"testing"

	"surge/internal/driver"
	"surge/internal/layout"
	"surge/internal/mir"
	"surge/internal/mono"
	"surge/internal/symbols"
	"surge/internal/types"
	"surge/internal/valueops"
)

// operationProbeSource holds one type for each answer the registry can give: a
// `@copy` struct, a struct with a real `__clone` body, and a map whose key is a
// type the census must additionally mark as a key root.
const operationProbeSource = `
@copy type Plain = { x: int, y: int };
type Model = { text: string };
extern<Model> {
    fn __clone(self: &Model) -> Model {
        return Model { text = clone(&self.text) };
    }
}
fn helper() -> int {
    let flat = Plain { x = 1, y = 2 };
    let value = Model { text = "seed" };
    let copied = clone(&value);
    let mut table: Map<string, int> = Map::<string, int>.new();
    return flat.x;
}
`

func compileOperationProbe(t *testing.T) CompileResult {
	t.Helper()
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	result, err := Compile(context.Background(), &CompileRequest{
		TargetPath:     writeAnalysisSource(t, operationProbeSource),
		BaseDir:        testRepoRoot(t),
		MaxDiagnostics: 20,
		Analysis:       true,
	})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if result.MIR == nil || result.MIR.Meta == nil {
		t.Fatal("production compile returned MIR without metadata")
	}
	if result.MIR.Meta.Operations == nil {
		t.Fatal("production compile published no operation registry")
	}
	return result
}

// namedStruct finds one probe type by name. Interner ids depend on the order
// types happened to be created in, so the tests below never name one.
func namedStruct(t *testing.T, typesIn *types.Interner, name string) types.TypeID {
	t.Helper()
	id, ok := typesIn.FindStructInstance(typesIn.Strings.Intern(name), nil)
	if !ok || id == types.NoTypeID {
		t.Fatalf("%s is absent from its own compilation", name)
	}
	return id
}

// TestCompilePublishesACopyTypeWithNothingToEmit pins the COPY half of the
// staged flag set. A `@copy` type carries the bit and no clone symbol: copy_init
// is satisfied by the runtime's generic byte copy, so there is nothing for the
// compiler to emit and nothing for the entry to name.
func TestCompilePublishesACopyTypeWithNothingToEmit(t *testing.T) {
	result := compileOperationProbe(t)
	typesIn := result.Diagnose.Sema.TypeInterner
	entry, err := result.MIR.Meta.Operations.Value(namedStruct(t, typesIn, "Plain"))
	if err != nil {
		t.Fatalf("operation plan for Plain: %v", err)
	}
	if entry.Flags&valueops.FlagCopy == 0 {
		t.Fatalf("a `@copy` type published flags %s", entry.Flags)
	}
	if entry.Flags&valueops.FlagClonable != 0 {
		t.Fatalf("a `@copy` type published the clonable bit: %s", entry.Flags)
	}
	if entry.CloneInit != symbols.NoSymbolID {
		t.Fatalf("a `@copy` type named clone_init symbol %d", entry.CloneInit)
	}
	if entry.Facts.Size == 0 {
		t.Fatal("the entry carries no frozen physical facts")
	}
}

// TestCompilePublishesAClonableTypeNamingItsEmittedCloneInit pins the CLONABLE
// half, and with it the whole point of the seam: the published symbol is the
// instance monomorphization actually emitted into THIS module, not a template
// the compiler merely knew about.
func TestCompilePublishesAClonableTypeNamingItsEmittedCloneInit(t *testing.T) {
	result := compileOperationProbe(t)
	typesIn := result.Diagnose.Sema.TypeInterner
	entry, err := result.MIR.Meta.Operations.Value(namedStruct(t, typesIn, "Model"))
	if err != nil {
		t.Fatalf("operation plan for Model: %v", err)
	}
	if entry.Flags&valueops.FlagClonable == 0 {
		t.Fatalf("a type with a `__clone` body published flags %s", entry.Flags)
	}
	if entry.CloneInit == symbols.NoSymbolID {
		t.Fatal("a clonable type published no clone_init symbol")
	}
	funcID, emitted := result.MIR.FuncBySym[entry.CloneInit]
	if !emitted {
		t.Fatalf("clone_init symbol %d names no function in the module that published it", entry.CloneInit)
	}
	if fn := result.MIR.Funcs[funcID]; fn == nil || fn.Name != "__clone" {
		t.Fatalf("clone_init symbol %d names %v rather than a clone body", entry.CloneInit, result.MIR.Funcs[funcID])
	}
	if entry.Evidence.CloneMethodKey == "" {
		t.Fatal("a clonable type published no evidence for which body was selected")
	}
}

// TestCompilePublishesStagedCapabilitiesWithTheirEvidence pins that the four
// verdicts the ABI cannot carry yet are still ANSWERED, and answered with a
// reason. A registry that recorded them as bare false would be indistinguishable
// from one that never asked.
func TestCompilePublishesStagedCapabilitiesWithTheirEvidence(t *testing.T) {
	result := compileOperationProbe(t)
	typesIn := result.Diagnose.Sema.TypeInterner
	entry, err := result.MIR.Meta.Operations.Value(namedStruct(t, typesIn, "Model"))
	if err != nil {
		t.Fatalf("operation plan for Model: %v", err)
	}
	for name, reason := range map[string]string{
		"droppable":      entry.Evidence.DroppableReason,
		"traceable":      entry.Evidence.TraceableReason,
		"shard movable":  entry.Evidence.ShardReason,
		"cross clonable": entry.Evidence.CrossCloneReason,
	} {
		if reason == "" {
			t.Errorf("the %s verdict was published with no evidence", name)
		}
	}
	if entry.Flags&(valueops.FlagDroppable|valueops.FlagTraceable|
		valueops.FlagShardMovable|valueops.FlagCrossClonable) != 0 {
		t.Fatalf("a staged verdict reached the ABI flag set: %s", entry.Flags)
	}
}

// TestCompilePublishesKeyOperationsForMapKeyRoots pins that a type reached as a
// map key gets key operations of its own, recorded as explicitly absent while
// nothing derives them. An absent map entry would have refused the registry, so
// this also proves the census role survived into publication.
func TestCompilePublishesKeyOperationsForMapKeyRoots(t *testing.T) {
	result := compileOperationProbe(t)
	registry := result.MIR.Meta.Operations
	keys := registry.KeyTypeIDs()
	if len(keys) == 0 {
		t.Fatal("a program holding a map published no key operation roots")
	}
	stringKey := result.Diagnose.Sema.TypeInterner.Builtins().String
	found := false
	for _, id := range keys {
		if id == stringKey {
			found = true
		}
	}
	if !found {
		t.Fatalf("a `Map<string, int>` published key roots %v without its string key", keys)
	}
	key, err := registry.Key(stringKey)
	if err != nil {
		t.Fatalf("key operation plan for string: %v", err)
	}
	if key.Hash != symbols.NoSymbolID || key.Equal != symbols.NoSymbolID {
		t.Fatalf("staged key callbacks were published as present: hash=%d equal=%d", key.Hash, key.Equal)
	}
	if key.Value.Type != stringKey {
		t.Fatalf("a key entry embedded the value plan of type#%d", key.Value.Type)
	}
	if _, err := registry.Key(stringKey + 4096); err == nil {
		t.Fatal("the registry answered key operations for a type that is not a key")
	}
}

// TestCompilePublishesTheSameOperationRegistryTwice pins determinism. The
// registry's order is the census's, and the hash covers every field that
// distinguishes one entry from another, so two compiles of one program that
// disagreed would mean something in the walk reads a map.
//
// It compiles ONE path twice rather than the same text at two paths. Evidence
// carries the selected clone body's key, which names the module and file it was
// declared in, so the same source at two locations is two programs as far as
// this registry is concerned and the hash says so.
func TestCompilePublishesTheSameOperationRegistryTwice(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	path := writeAnalysisSource(t, operationProbeSource)
	hash := func() string {
		t.Helper()
		result, err := Compile(context.Background(), &CompileRequest{
			TargetPath:     path,
			BaseDir:        testRepoRoot(t),
			MaxDiagnostics: 20,
			Analysis:       true,
		})
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		if result.MIR.Meta.Operations == nil {
			t.Fatal("production compile published no operation registry")
		}
		digest, err := result.MIR.Meta.Operations.Hash()
		if err != nil {
			t.Fatalf("hash operation registry: %v", err)
		}
		return digest
	}
	if first, second := hash(), hash(); first != second {
		t.Fatalf("two compiles of one program published different registries:\n  %s\n  %s", first, second)
	}
}

// TestCompilePublishesCloneInitForAnEntrypointOnlyType is the regression test
// for what publishing found.
//
// An argv entrypoint calls a `from_str` conversion returning `Erring<T, Error>`,
// and no expression in the source names that call. The reachable universe the
// required-operation derivation walked was read from the closure alone, so it
// never saw `Error` — a type with a real `__clone` body that reaches final MIR.
// Publication then found a clonable type whose implementation nothing had made
// reachable, which is the whole program failing to compile.
func TestCompilePublishesCloneInitForAnEntrypointOnlyType(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	result, err := Compile(context.Background(), &CompileRequest{
		TargetPath:     writeAnalysisSource(t, `@entrypoint("argv") fn main(x: int) -> int { return x; }`),
		BaseDir:        testRepoRoot(t),
		MaxDiagnostics: 20,
	})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	registry := result.MIR.Meta.Operations
	if registry == nil {
		t.Fatal("an argv entrypoint compile published no operation registry")
	}
	typesIn := result.Diagnose.Sema.TypeInterner
	errorType := namedStruct(t, typesIn, "Error")
	entry, err := registry.Value(errorType)
	if err != nil {
		t.Fatalf("operation plan for Error: %v", err)
	}
	if entry.Flags&valueops.FlagClonable == 0 {
		t.Fatalf("Error has a `__clone` body and published flags %s", entry.Flags)
	}
	if _, emitted := result.MIR.FuncBySym[entry.CloneInit]; !emitted {
		t.Fatalf("Error's clone_init symbol %d names no emitted function", entry.CloneInit)
	}
}

// mirroredCompile assembles the production lowering sequence in the test, which
// is the only way to hold the monomorphized module: CompileResult deliberately
// carries MIR and diagnostics alone.
//
// It exists to be a SECOND caller of the shared plan constructor, so that a
// production site quietly passing something else than what this builds is a
// failing hash comparison rather than a discovery made later by a backend.
func mirroredCompile(t *testing.T, path string) (*mir.Module, *mono.MonoModule, *driver.DiagnoseResult) {
	t.Helper()
	res, err := driver.DiagnoseWithOptions(context.Background(), path, &driver.DiagnoseOptions{
		Stage: driver.DiagnoseStageSema, BaseDir: testRepoRoot(t), MaxDiagnostics: 20,
		EmitHIR: true, EmitInstantiations: true,
	})
	if err != nil {
		t.Fatalf("diagnose: %v", err)
	}
	hirModule, err := driver.CombineHIRWithModules(context.Background(), res)
	if err != nil {
		t.Fatalf("combine HIR with modules: %v", err)
	}
	if hirModule == nil {
		hirModule = res.HIR
	}
	mm, err := mono.MonomorphizeModule(hirModule, res.Instantiations, res.Sema, mono.Options{MaxDepth: 64})
	if err != nil {
		t.Fatalf("monomorphize: %v", err)
	}
	mirMod, err := mir.LowerModule(mm, res.Sema)
	if err != nil {
		t.Fatalf("lower MIR: %v", err)
	}
	for _, fn := range mirMod.Funcs {
		mir.SimplifyCFG(fn)
		mir.RecognizeSwitchTag(fn)
		mir.SimplifyCFG(fn)
	}
	if err := mir.LowerAsyncStateMachine(mirMod, res.Sema, res.Symbols.Table); err != nil {
		t.Fatalf("lower async state machine: %v", err)
	}
	for _, fn := range mirMod.Funcs {
		mir.SimplifyCFG(fn)
	}
	if err := mir.FinalizeModuleMeta(mirMod, res.Sema.TypeInterner, layout.X86_64LinuxGNU(),
		mir.NewOperationPlanInput(res.Sema, mm)); err != nil {
		t.Fatalf("finalize module metadata: %v", err)
	}
	return mirMod, mm, res
}

// TestOperationRegistryAgreesAcrossPipelines compares what the production
// pipeline published with what an independently assembled one publishes for the
// same program, and checks the clone symbol against the callable map that
// supplied it.
func TestOperationRegistryAgreesAcrossPipelines(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	path := writeAnalysisSource(t, operationProbeSource)
	produced, err := Compile(context.Background(), &CompileRequest{
		TargetPath:     path,
		BaseDir:        testRepoRoot(t),
		MaxDiagnostics: 20,
		Analysis:       true,
	})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	mirrored, mm, res := mirroredCompile(t, path)
	if mirrored.Meta.Operations == nil {
		t.Fatal("the mirrored pipeline published no operation registry")
	}

	productionHash, err := produced.MIR.Meta.Operations.Hash()
	if err != nil {
		t.Fatalf("hash production registry: %v", err)
	}
	mirroredHash, err := mirrored.Meta.Operations.Hash()
	if err != nil {
		t.Fatalf("hash mirrored registry: %v", err)
	}
	if productionHash != mirroredHash {
		t.Fatalf("two pipelines published different registries for one program:\n  production %s\n  mirrored   %s",
			productionHash, mirroredHash)
	}

	// The published clone symbol must be the one monomorphization recorded for
	// that exact template and argument vector, not merely some emitted body.
	typesIn := res.Sema.TypeInterner
	model := namedStruct(t, typesIn, "Model")
	entry, err := mirrored.Meta.Operations.Value(model)
	if err != nil {
		t.Fatalf("operation plan for Model: %v", err)
	}
	capability, err := res.Sema.Capabilities.Classify(model)
	if err != nil {
		t.Fatalf("classify Model: %v", err)
	}
	op, required, err := res.Sema.NewCloneEmissionIndex().RequiredValueOpFor(&capability)
	if err != nil || !required {
		t.Fatalf("Model requires no clone operation: required=%v err=%v", required, err)
	}
	instance, found, err := mm.Callables.LookupChecked(op.Template, op.TemplateArgs)
	if err != nil || !found {
		t.Fatalf("callable map holds no instance for Model's clone: found=%v err=%v", found, err)
	}
	if entry.CloneInit != instance {
		t.Fatalf("published clone_init %d is not the emitted instance %d", entry.CloneInit, instance)
	}
}

// TestOperationRegistryRefusesACloneWithNoEmittedInstance constructs the miss
// the required-operation fixpoint exists to prevent, and pins what the compiler
// says when it happens.
//
// The construction is deliberate rather than found: a program whose clonable
// types all have their instances is finalized against ANOTHER program's callable
// map. That map carries a real canonical identity and resolves the stdlib clone
// template perfectly well — it simply retained no instance for it, because
// nothing in the program it came from was ever clonable. So the lookup reaches
// the entry table and finds nothing, which is precisely the shape a fixpoint
// that failed to inject a root would produce.
//
// The message is the assertion. When this fires for real it is a compiler
// defect, and what it names — the type, the operation, the template and the type
// arguments — is the whole investigation.
func TestOperationRegistryRefusesACloneWithNoEmittedInstance(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	// An argv entrypoint reaches `Erring<int, Error>`, and `Error` has a real
	// `__clone` body in the standard library.
	clonable := writeAnalysisSource(t, `@entrypoint("argv") fn main(x: int) -> int { return x; }`)
	_, mm, res := mirroredCompile(t, clonable)
	unrelated := writeAnalysisSource(t, `fn other() -> int { return 0; }`)
	_, stranger, _ := mirroredCompile(t, unrelated)

	// A fresh module, because the one mirroredCompile returned has already been
	// finalized against its own correct callable map.
	mirMod, err := mir.LowerModule(mm, res.Sema)
	if err != nil {
		t.Fatalf("lower MIR: %v", err)
	}
	plan := mir.NewOperationPlanInput(res.Sema, mm)
	if plan == nil {
		t.Fatal("a whole-program compile produced no operation plan input")
	}
	plan.Callables = stranger.Callables

	err = mir.FinalizeModuleMeta(mirMod, res.Sema.TypeInterner, layout.X86_64LinuxGNU(), plan)
	if err == nil {
		t.Fatal("publication accepted a clonable type with no emitted clone instance")
	}
	for _, want := range []string{"Error", "clone initialization", "template sym#", "no instance for"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal does not say %q: %v", want, err)
		}
	}
	if mirMod.Meta != nil && mirMod.Meta.Operations != nil {
		t.Fatal("a refused publication left an operation registry behind")
	}
}
