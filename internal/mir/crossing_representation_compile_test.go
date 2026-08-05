package mir_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/hir"
	"surge/internal/lexer"
	"surge/internal/mir"
	"surge/internal/mono"
	"surge/internal/parser"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

const crossingMIRPrelude = `
tag Success<T>(T);
tag Cancelled();
type TaskResult<T> = Success(T) | Cancelled;
tag Some<T>(T);
tag None();
type Option<T> = Some(T) | None;
type Task<T> = { __opaque: int };
extern<Task<T>> {
    fn await(self: own Task<T>) -> TaskResult<T>;
}
@intrinsic @copy
type Placement = { __opaque: int };
type TcpConn = { fd: int };
type Channel<T> = { id: int };
@shard_movable
type Movable = { id: int };
`

type crossingMIRCompileResult struct {
	mod     *mir.Module
	types   *types.Interner
	sema    *sema.Result
	symbols *symbols.Result
}

func compileCrossingMIR(t *testing.T, src string, forms map[sema.CrossingLoweringKind]bool) crossingMIRCompileResult {
	t.Helper()
	fs := source.NewFileSet()
	fileID := fs.AddVirtual("test.sg", []byte(src))
	file := fs.Get(fileID)

	stringsIn := source.NewInterner()
	typesIn := types.NewInterner()
	instMap := mono.NewInstantiationMap()
	bag := diag.NewBag(200)
	lx := lexer.New(file, lexer.Options{})
	builder := ast.NewBuilder(ast.Hints{}, stringsIn)
	parsed := parser.ParseFile(context.Background(), fs, lx, builder, parser.Options{
		Reporter:  &diag.BagReporter{Bag: bag},
		MaxErrors: 200,
	})
	if bag.HasErrors() {
		t.Fatalf("parse diagnostics: %s", crossingDiagSummary(bag))
	}

	symbolsRes := symbols.ResolveFile(builder, parsed.File, &symbols.ResolveOptions{
		Reporter:   &diag.BagReporter{Bag: bag},
		Validate:   true,
		ModulePath: "core",
		FilePath:   "test.sg",
	})
	if bag.HasErrors() {
		t.Fatalf("symbol diagnostics: %s", crossingDiagSummary(bag))
	}

	semaRes := sema.Check(context.Background(), builder, parsed.File, sema.Options{
		Reporter:       &diag.BagReporter{Bag: bag},
		Symbols:        &symbolsRes,
		Types:          typesIn,
		ModulePath:     builder.StringsInterner.Intern("core"),
		Instantiations: mono.NewInstantiationMapRecorder(instMap),
	})
	if bag.HasErrors() {
		t.Fatalf("sema diagnostics: %s", crossingDiagSummary(bag))
	}
	finalizeTestInstantiationClosure(t, typesIn, &symbolsRes, &semaRes)

	hirMod, err := hir.LowerWithOptions(context.Background(), builder, parsed.File, &semaRes, &symbolsRes, hir.LowerOptions{
		CrossingForms: forms,
	})
	if err != nil {
		t.Fatalf("HIR lowering failed: %v", err)
	}
	monoMod, err := mono.MonomorphizeModule(hirMod, instMap, &semaRes, mono.Options{})
	if err != nil {
		t.Fatalf("monomorphization failed: %v", err)
	}
	mirMod, err := mir.LowerModuleWithOptions(monoMod, &semaRes, mir.LowerOptions{
		CrossingForms: forms,
	})
	if err != nil {
		t.Fatalf("MIR lowering failed: %v", err)
	}
	return crossingMIRCompileResult{mod: mirMod, types: typesIn, sema: &semaRes, symbols: &symbolsRes}
}

func crossingDiagSummary(bag *diag.Bag) string {
	if bag == nil || bag.Len() == 0 {
		return "(none)"
	}
	var b strings.Builder
	for _, d := range bag.Items() {
		if b.Len() > 0 {
			b.WriteString("; ")
		}
		fmt.Fprintf(&b, "%s: %s", d.Code.ID(), d.Message)
	}
	return b.String()
}
