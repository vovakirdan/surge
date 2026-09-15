package hir

import (
	"context"
	"reflect"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/lexer"
	"surge/internal/parser"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func TestBlockExprNormalExitDrops(t *testing.T) {
	const read = `fn read(x: &string) -> int { return 1; }
`
	cases := []struct {
		name, source string
		tail         ast.StmtKind
		owners       []string
	}{
		{"implicit_return", read + `fn probe() -> int {
    return compare true {
        true => { let inner = "heap"; let owned = inner; read(&owned); };
        false => 0;
    };
}`, ast.StmtReturn, []string{"owned"}},
		{"legacy_tail_ret", read + `fn probe() -> int {
    let answer = { let inner = "heap"; let owned = inner; read(&owned); };
    return answer;
}`, ast.StmtExpr, []string{"owned"}},
		{"explicit_tail_ret", read + `fn probe() -> int {
    let answer = { let inner = "heap"; let owned = inner; ret read(&owned); };
    return answer;
}`, ast.StmtRet, []string{"owned"}},
		{"returned_owner", `fn probe() -> string {
    let answer = { let owned = "heap"; ret owned; };
    return answer;
}`, ast.StmtRet, nil},
		{"borrowed_outer_owner", read + `fn probe() -> int {
    let owner = "outer owner survives";
    let answer: &string = { let doomed = "block local"; ret &owner; };
    let n = read(answer);
    return n + read(&owner);
}`, ast.StmtRet, []string{"doomed"}},
		{"residual_partial_move", `type Pair = { left: string, right: string };
fn probe() -> string {
    let answer = {
        let holder = Pair { left = "leaves", right = "stays" };
        let taken = own holder.left;
        ret taken;
    };
    return answer;
}`, ast.StmtRet, []string{"holder"}},
		{"refcounted_result", `fn probe(input: float) -> float {
    let answer = { let doomed = "block local"; ret input + 0.25; };
    return answer;
}`, ast.StmtRet, []string{"doomed"}},
		{"existing_stmt_key", read + `fn probe() -> int {
    let answer = { let owned = "heap"; ret read(&owned); };
    return answer;
}`, ast.StmtRet, []string{"owned"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l, fileID, blockID, tailID := blockExitFixture(t, tc.source)
			if got := l.builder.Stmts.Get(tailID).Kind; got != tc.tail {
				t.Fatalf("AST tail = %v, want %v", got, tc.tail)
			}
			syms := l.semaRes.BlockExprEndDrops[blockID]
			var names []string
			for _, sym := range syms {
				names = append(names, l.bindingDropName(sym))
				if l.bindingDropType(sym) == types.NoTypeID {
					t.Fatalf("untyped producer obligation %v", sym)
				}
			}
			if !reflect.DeepEqual(names, tc.owners) {
				t.Fatalf("SEMA block owners = %v, want %v", names, tc.owners)
			}
			var want []DropLocal
			for _, sym := range syms {
				steps := l.semaRes.ResidualDrops[sema.DropSite{Expr: blockID, Symbol: sym}]
				want = append(want, DropLocal{SymbolID: sym, Type: l.bindingDropType(sym), Steps: steps})
			}
			if tc.name == "residual_partial_move" {
				right := l.strings.Intern("right")
				steps := []sema.DropStep{
					{Path: []sema.PlaceSegment{{Kind: sema.PlaceSegmentField, Name: right}}},
					{Shallow: true},
				}
				if !reflect.DeepEqual(want[0].Steps, steps) {
					t.Fatalf("producer residual = %#v, want %#v", want[0].Steps, steps)
				}
			}
			if tc.name == "existing_stmt_key" {
				// Structural authority control: ordinary source did not produce this
				// stmt-key. It deliberately disagrees with the expr-key plan.
				l.semaRes.EarlyExitDrops = map[ast.StmtID][]symbols.SymbolID{tailID: syms}
				l.semaRes.ResidualDrops = map[sema.DropSite][]sema.DropStep{
					{Stmt: tailID, Symbol: syms[0]}: {{Shallow: true}},
				}
				want[0].Steps = []sema.DropStep{{Shallow: true}}
			}
			// Exercise the public file pipeline too. Inspect the same real lowering
			// seam separately, before normalization rewrites the compare arm.
			mod, err := Lower(context.Background(), l.builder, fileID, l.semaRes, l.symRes)
			if err != nil || mod == nil || len(mod.Funcs) == 0 {
				t.Fatalf("full HIR lowering failed: module=%v error=%v", mod, err)
			}
			expr := l.lowerExpr(blockID)
			if l.err != nil || expr == nil || expr.Kind != ExprBlock {
				t.Fatalf("block lowering failed: expr=%v error=%v", expr, l.err)
			}
			block := expr.Data.(BlockExprData).Block
			for _, stmt := range block.Stmts {
				if stmt.Kind == StmtDrop {
					t.Fatal("normal exit must carry its drops, not also emit standalone drops")
				}
			}
			tail := block.LastStmt()
			var got []DropLocal
			var value *Expr
			switch data := tail.Data.(type) {
			case ReturnData:
				if tc.tail != ast.StmtReturn || !data.IsImplicit {
					t.Fatal("return lost its parser-synthetic provenance")
				}
				got, value = data.DropsAfterValue, data.Value
			case RetData:
				if tc.tail == ast.StmtReturn {
					t.Fatal("implicit return changed to ret")
				}
				got, value = data.DropsAfterValue, data.Value
			default:
				t.Fatalf("HIR tail = %v; no dead drop may follow the exit", tail.Kind)
			}
			if value == nil || value.Type != l.semaRes.ExprTypes[blockID] {
				t.Fatal("block result lost its value or resolved type")
			}
			if tc.name == "borrowed_outer_owner" {
				if ty, ok := l.semaRes.TypeInterner.Lookup(value.Type); !ok || ty.Kind != types.KindReference {
					t.Fatal("outer-owner result is not a reference")
				}
				original, ok := l.builder.Exprs.Unary(l.builder.Stmts.Ret(tailID).Expr)
				if !ok || original.Op != ast.ExprUnaryRef {
					t.Fatal("source result is not the explicit shared outer borrow")
				}
				root := l.symRes.ExprSymbols[original.Operand]
				address, ok := value.Data.(UnaryOpData)
				if !ok || address.Op != ast.ExprUnaryRef || address.Operand == nil {
					t.Fatal("HIR lost the shared borrow operation")
				}
				ref, ok := address.Operand.Data.(VarRefData)
				if !ok || !root.IsValid() || ref.SymbolID != root || l.bindingDropName(root) != "owner" {
					t.Fatal("HIR borrow no longer refers to the exact outer owner")
				}
			}
			if len(got) != len(want) {
				t.Fatalf("carried drops = %#v, want %#v", got, want)
			}
			for i := range want {
				if got[i].SymbolID != want[i].SymbolID || got[i].Type != want[i].Type || !reflect.DeepEqual(got[i].Steps, want[i].Steps) {
					t.Fatalf("carried drop %d = %#v, want %#v", i, got[i], want[i])
				}
			}
		})
	}
}

// No stdlib imports: parse, resolve and check every fixture independently, then
// retain its actual AST/SEMA identities for the lowering assertions above.
func blockExitFixture(t *testing.T, src string) (*lowerer, ast.FileID, ast.ExprID, ast.StmtID) {
	t.Helper()
	ctx := context.Background()
	fs := source.NewFileSetWithBase("")
	file := fs.AddVirtual("block_exit.sg", []byte(src))
	bag := diag.NewBag(100)
	builder := ast.NewBuilder(ast.Hints{}, nil)
	parsed := parser.ParseFile(ctx, fs, lexer.New(fs.Get(file), lexer.Options{}), builder,
		parser.Options{Reporter: &diag.BagReporter{Bag: bag}, MaxErrors: uint(bag.Cap())})
	if bag.HasErrors() {
		t.Fatalf("parse diagnostics: %v", bag.Items())
	}
	syms := symbols.ResolveFile(builder, parsed.File, &symbols.ResolveOptions{Reporter: &diag.BagReporter{Bag: bag}})
	result := sema.Check(ctx, builder, parsed.File, sema.Options{Symbols: &syms, Reporter: &diag.BagReporter{Bag: bag}})
	if bag.HasErrors() {
		t.Fatalf("resolve/SEMA diagnostics: %v", bag.Items())
	}
	var target ast.ExprID
	var tail ast.StmtID
	for id := ast.ExprID(1); uint32(id) <= builder.Exprs.Arena.Len(); id++ {
		if block, ok := builder.Exprs.Block(id); ok {
			if target.IsValid() || len(block.Stmts) == 0 {
				t.Fatal("fixture must contain exactly one nonempty block expression")
			}
			target, tail = id, block.Stmts[len(block.Stmts)-1]
		}
	}
	if !target.IsValid() || result.ExprTypes[target] == types.NoTypeID {
		t.Fatal("missing typed target block")
	}
	l := &lowerer{ctx: ctx, builder: builder, semaRes: &result, symRes: &syms, strings: builder.StringsInterner,
		nextFnID: 1, module: &Module{SourceAST: parsed.File, TypeInterner: result.TypeInterner, BindingTypes: result.BindingTypes, Symbols: &syms}}
	l.stmtSymbols = buildStmtSymbolIndex(&syms, parsed.File)
	l.crossingByExpr = buildCrossingLoweringIndex(&result)
	l.cloneRequests = buildDirectCloneRequestIndex(&result)
	return l, parsed.File, target, tail
}
