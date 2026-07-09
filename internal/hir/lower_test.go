package hir_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/hir"
	"surge/internal/lexer"
	"surge/internal/parser"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func TestLowerSimpleFunction(t *testing.T) {
	src := `
fn add(a: int, b: int) -> int {
    return a + b;
}
`
	module, interner, err := parseAndLower(t, src)
	if err != nil {
		t.Fatalf("failed to lower: %v", err)
	}
	if module == nil {
		t.Fatal("module is nil")
	}

	if len(module.Funcs) != 1 {
		t.Errorf("expected 1 function, got %d", len(module.Funcs))
	}

	fn := module.Funcs[0]
	if fn.Name != "add" {
		t.Errorf("expected function name 'add', got %q", fn.Name)
	}

	if len(fn.Params) != 2 {
		t.Errorf("expected 2 parameters, got %d", len(fn.Params))
	}

	if fn.Body == nil {
		t.Error("expected function body")
	} else if len(fn.Body.Stmts) == 0 {
		t.Error("expected statements in body")
	}

	// Test printing
	var buf bytes.Buffer
	if err := hir.Dump(&buf, module, interner); err != nil {
		t.Fatalf("failed to dump: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "fn add") {
		t.Error("output should contain 'fn add'")
	}
	if !strings.Contains(output, "return") {
		t.Error("output should contain 'return'")
	}
}

func TestLowerIfStatement(t *testing.T) {
	src := `
fn test(x: int) -> int {
    if x > 0 {
        return 1;
    } else {
        return 0;
    }
}
`
	module, _, err := parseAndLower(t, src)
	if err != nil {
		t.Fatalf("failed to lower: %v", err)
	}
	if module == nil {
		t.Fatal("module is nil")
	}

	if len(module.Funcs) != 1 {
		t.Errorf("expected 1 function, got %d", len(module.Funcs))
	}

	fn := module.Funcs[0]
	if fn.Body == nil || len(fn.Body.Stmts) == 0 {
		t.Fatal("expected statements in body")
	}

	// First statement should be an if
	if fn.Body.Stmts[0].Kind != hir.StmtIf {
		t.Errorf("expected StmtIf, got %v", fn.Body.Stmts[0].Kind)
	}
}

func TestLowerWhileLoop(t *testing.T) {
	src := `
fn loop_test() {
    let mut i = 0;
    while i < 10 {
        i = i + 1;
    }
}
`
	module, _, err := parseAndLower(t, src)
	if err != nil {
		t.Fatalf("failed to lower: %v", err)
	}
	if module == nil {
		t.Fatal("module is nil")
	}

	fn := module.Funcs[0]
	if fn.Body == nil || len(fn.Body.Stmts) < 2 {
		t.Fatal("expected at least 2 statements in body")
	}

	// Second statement should be a while
	if fn.Body.Stmts[1].Kind != hir.StmtWhile {
		t.Errorf("expected StmtWhile, got %v", fn.Body.Stmts[1].Kind)
	}
}

func TestLowerLetBinding(t *testing.T) {
	src := `
fn test() {
    let x = 42;
    let mut y = x + 1;
}
`
	module, _, err := parseAndLower(t, src)
	if err != nil {
		t.Fatalf("failed to lower: %v", err)
	}
	if module == nil {
		t.Fatal("module is nil")
	}

	fn := module.Funcs[0]
	if fn.Body == nil || len(fn.Body.Stmts) < 2 {
		t.Fatal("expected at least 2 statements in body")
	}

	// Both statements should be let
	if fn.Body.Stmts[0].Kind != hir.StmtLet {
		t.Errorf("expected StmtLet for stmt 0, got %v", fn.Body.Stmts[0].Kind)
	}
	if fn.Body.Stmts[1].Kind != hir.StmtLet {
		t.Errorf("expected StmtLet for stmt 1, got %v", fn.Body.Stmts[1].Kind)
	}

	// Check mutability
	data0 := fn.Body.Stmts[0].Data.(hir.LetData)
	if data0.IsMut {
		t.Error("first let should be immutable")
	}

	data1 := fn.Body.Stmts[1].Data.(hir.LetData)
	if !data1.IsMut {
		t.Error("second let should be mutable")
	}
}

func TestLowerRetInBlockExpr(t *testing.T) {
	src := `
fn main() -> int {
    let x = { ret 1; };
    let y = { ret 2; };
    return x + y;
}
`
	module, _, err := parseAndLower(t, src)
	if err != nil {
		t.Fatalf("failed to lower: %v", err)
	}
	if module == nil || len(module.Funcs) != 1 {
		t.Fatalf("expected one function, got %+v", module)
	}

	fn := module.Funcs[0]
	if fn.Body == nil || len(fn.Body.Stmts) < 2 {
		t.Fatalf("expected at least two statements, got %+v", fn.Body)
	}

	for _, idx := range []int{0, 1} {
		stmt := fn.Body.Stmts[idx]
		if stmt.Kind != hir.StmtLet {
			t.Fatalf("stmt %d: expected let, got %s", idx, stmt.Kind)
		}
		data := stmt.Data.(hir.LetData)
		if data.Value == nil || data.Value.Kind != hir.ExprBlock {
			t.Fatalf("stmt %d: expected block expr value, got %+v", idx, data.Value)
		}
		block := data.Value.Data.(hir.BlockExprData).Block
		if block == nil || len(block.Stmts) != 1 {
			t.Fatalf("stmt %d: expected one block stmt, got %+v", idx, block)
		}
		if block.Stmts[0].Kind != hir.StmtRet {
			t.Fatalf("stmt %d: expected hir ret, got %s", idx, block.Stmts[0].Kind)
		}
		retData, ok := block.Stmts[0].Data.(hir.RetData)
		if !ok || retData.Value == nil {
			t.Fatalf("stmt %d: expected hir ret payload, got %+v", idx, block.Stmts[0].Data)
		}
	}
}

func TestLowerOnCrossingBypassReturnsError(t *testing.T) {
	src := `
tag Success<T>(T);
tag Cancelled();
type TaskResult<T> = Success(T) | Cancelled;
@intrinsic @copy
type Placement = { __opaque: int };
@intrinsic const pool: Placement;

fn score(n: int) -> TaskResult<int> {
    return on pool {
        ret n;
    };
}
`
	_, _, err := parseAndLower(t, src)
	if err == nil {
		t.Fatal("expected HIR lowering error for `on` crossing bypass, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "`on`") || !strings.Contains(msg, "HIR lowering") {
		t.Fatalf("expected deterministic `on` HIR lowering error, got %q", msg)
	}
}

func TestLowerSpawnOnCrossingBypassReturnsError(t *testing.T) {
	src := `
tag Success<T>(T);
tag Cancelled();
type TaskResult<T> = Success(T) | Cancelled;
type Task<T> = { __opaque: int };
@intrinsic @copy
type Placement = { __opaque: int };
@intrinsic const pool: Placement;

fn start(n: int) -> far Task<int> {
    return spawn on pool {
        ret n;
    };
}
`
	_, _, err := parseAndLower(t, src)
	if err == nil {
		t.Fatal("expected HIR lowering error for `spawn on` crossing bypass, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "`spawn on`") || !strings.Contains(msg, "HIR lowering") {
		t.Fatalf("expected deterministic `spawn on` HIR lowering error, got %q", msg)
	}
}

func TestLowerFarTaskCrossingBypassReturnsError(t *testing.T) {
	cases := []struct {
		name      string
		src       string
		construct string
	}{
		{
			name: "await",
			src: crossingLoweringPrelude + `
fn wait_remote(t: far Task<int>) -> TaskResult<int> {
    return t.await();
}
`,
			construct: "`far Task<T>.await()`",
		},
		{
			name: "cancel",
			src: crossingLoweringPrelude + `
fn cancel_remote(t: far Task<int>) -> TaskResult<nothing> {
    return t.cancel();
}
`,
			construct: "`far Task<T>.cancel()`",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := parseAndLower(t, tc.src)
			if err == nil {
				t.Fatalf("expected HIR lowering error for %s crossing bypass, got nil", tc.construct)
			}
			msg := err.Error()
			if !strings.Contains(msg, tc.construct) || !strings.Contains(msg, "HIR lowering") {
				t.Fatalf("expected deterministic %s HIR lowering error, got %q", tc.construct, msg)
			}
		})
	}
}

func TestLowerCrossingRepresentationWithExplicitCapability(t *testing.T) {
	cases := []struct {
		name          string
		src           string
		kind          sema.CrossingLoweringKind
		wantDest      sema.CrossingDestinationKind
		wantPayload   string
		wantResult    string
		wantHandle    string
		wantCaptures  int
		wantRemoteOps int
		wantReceiver  bool
		wantConsumes  bool
		wantBodyStmts int
	}{
		{
			name: "on placement",
			src: crossingLoweringPrelude + `
fn run(dst: Placement, n: int) -> TaskResult<int> {
    return on dst {
        ret n;
    };
}
`,
			kind:          sema.CrossingLoweringOnPlacement,
			wantDest:      sema.CrossingDestinationPlacement,
			wantPayload:   "int",
			wantResult:    "TaskResult<int>",
			wantCaptures:  1,
			wantBodyStmts: 1,
		},
		{
			name: "on far handle",
			src: crossingLoweringPrelude + `
fn run(conn: far TcpConn) -> TaskResult<nothing> {
    return on conn {
        conn.close();
        ret nothing;
    };
}
`,
			kind:          sema.CrossingLoweringOnFarHandle,
			wantDest:      sema.CrossingDestinationFarHandle,
			wantPayload:   "nothing",
			wantResult:    "TaskResult<nothing>",
			wantCaptures:  1,
			wantRemoteOps: 1,
			wantBodyStmts: 2,
		},
		{
			name: "spawn on",
			src: crossingLoweringPrelude + `
fn use(m: own Movable) -> int {
    return m.id;
}

fn run(dst: Placement, m: own Movable) -> far Task<int> {
    return spawn on dst {
        ret use(own m);
    };
}
`,
			kind:          sema.CrossingLoweringSpawnOn,
			wantDest:      sema.CrossingDestinationPlacement,
			wantPayload:   "int",
			wantResult:    "far Task<int>",
			wantHandle:    "far Task<int>",
			wantCaptures:  1,
			wantBodyStmts: 1,
		},
		{
			name: "far task await",
			src: crossingLoweringPrelude + `
fn wait_remote(t: far Task<int>) -> TaskResult<int> {
    return t.await();
}
`,
			kind:         sema.CrossingLoweringFarTaskAwait,
			wantPayload:  "int",
			wantResult:   "TaskResult<int>",
			wantReceiver: true,
			wantConsumes: true,
		},
		{
			name: "far task cancel",
			src: crossingLoweringPrelude + `
fn cancel_remote(t: far Task<int>) -> TaskResult<nothing> {
    return t.cancel();
}
`,
			kind:         sema.CrossingLoweringFarTaskCancel,
			wantPayload:  "nothing",
			wantResult:   "TaskResult<nothing>",
			wantReceiver: true,
			wantConsumes: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			module, interner, err := parseAndLowerWithOptions(t, tc.src, hir.LowerOptions{
				CrossingForms: map[sema.CrossingLoweringKind]bool{tc.kind: true},
			})
			if err != nil {
				t.Fatalf("lowering failed: %v", err)
			}
			expr := requireCrossingExpr(t, module, tc.kind)
			data := expr.Data.(hir.CrossingData)
			if data.Kind != tc.kind {
				t.Fatalf("kind = %d, want %d", data.Kind, tc.kind)
			}
			if data.Destination.Kind != tc.wantDest {
				t.Fatalf("destination kind = %d, want %d", data.Destination.Kind, tc.wantDest)
			}
			if tc.wantDest != sema.CrossingDestinationNone && data.Destination.Value == nil {
				t.Fatalf("missing destination expression")
			}
			if got := types.Label(interner, data.PayloadType); got != tc.wantPayload {
				t.Fatalf("payload type = %q, want %q", got, tc.wantPayload)
			}
			if got := types.Label(interner, data.ResultType); got != tc.wantResult {
				t.Fatalf("result type = %q, want %q", got, tc.wantResult)
			}
			if tc.wantHandle != "" {
				if got := types.Label(interner, data.HandleType); got != tc.wantHandle {
					t.Fatalf("handle type = %q, want %q", got, tc.wantHandle)
				}
			}
			if len(data.Captures) != tc.wantCaptures {
				t.Fatalf("captures = %d, want %d", len(data.Captures), tc.wantCaptures)
			}
			for i := range data.Captures {
				if data.Captures[i].Value == nil {
					t.Fatalf("capture %d missing HIR value", i)
				}
			}
			if len(data.RemoteOps) != tc.wantRemoteOps {
				t.Fatalf("remote ops = %d, want %d", len(data.RemoteOps), tc.wantRemoteOps)
			}
			for i := range data.RemoteOps {
				if data.RemoteOps[i].Receiver == nil {
					t.Fatalf("remote op %d missing receiver HIR value", i)
				}
			}
			if (data.Receiver != nil) != tc.wantReceiver {
				t.Fatalf("receiver present = %v, want %v", data.Receiver != nil, tc.wantReceiver)
			}
			if data.ConsumesHandle != tc.wantConsumes {
				t.Fatalf("consumes handle = %v, want %v", data.ConsumesHandle, tc.wantConsumes)
			}
			if tc.wantBodyStmts > 0 {
				if data.Body == nil {
					t.Fatalf("missing crossing body")
				}
				if len(data.Body.Stmts) != tc.wantBodyStmts {
					t.Fatalf("body statements = %d, want %d", len(data.Body.Stmts), tc.wantBodyStmts)
				}
			}
		})
	}
}

const crossingLoweringPrelude = `
tag Success<T>(T);
tag Cancelled();
type TaskResult<T> = Success(T) | Cancelled;
type Task<T> = { __opaque: int };
@intrinsic @copy
type Placement = { __opaque: int };
@intrinsic const pool: Placement;
@intrinsic const distributed: Placement;
type TcpConn = { fd: int };
type Channel<T> = { id: int };
@shard_movable
type Movable = { id: int };
`

func requireCrossingExpr(t *testing.T, module *hir.Module, kind sema.CrossingLoweringKind) *hir.Expr {
	t.Helper()
	var found *hir.Expr
	var walkExpr func(*hir.Expr)
	var walkBlock func(*hir.Block)
	walkExpr = func(e *hir.Expr) {
		if e == nil || found != nil {
			return
		}
		if e.Kind == hir.ExprCrossing {
			data := e.Data.(hir.CrossingData)
			if data.Kind == kind {
				found = e
			}
			return
		}
		switch data := e.Data.(type) {
		case hir.CallData:
			walkExpr(data.Callee)
			for _, arg := range data.Args {
				walkExpr(arg)
			}
		case hir.UnaryOpData:
			walkExpr(data.Operand)
		case hir.BinaryOpData:
			walkExpr(data.Left)
			walkExpr(data.Right)
		case hir.BlockExprData:
			walkBlock(data.Block)
		}
	}
	walkStmt := func(stmt hir.Stmt) {
		switch data := stmt.Data.(type) {
		case hir.ReturnData:
			walkExpr(data.Value)
		case hir.ExprStmtData:
			walkExpr(data.Expr)
		case hir.LetData:
			walkExpr(data.Value)
		}
	}
	walkBlock = func(block *hir.Block) {
		if block == nil || found != nil {
			return
		}
		for _, stmt := range block.Stmts {
			walkStmt(stmt)
		}
	}
	for _, fn := range module.Funcs {
		walkBlock(fn.Body)
	}
	if found == nil {
		t.Fatalf("missing HIR crossing expression kind %d", kind)
	}
	return found
}

func parseAndLower(t *testing.T, src string) (*hir.Module, *types.Interner, error) {
	t.Helper()
	return parseAndLowerWithOptions(t, src, hir.LowerOptions{})
}

func parseAndLowerWithOptions(t *testing.T, src string, lowerOpts hir.LowerOptions) (*hir.Module, *types.Interner, error) {
	t.Helper()
	fs := source.NewFileSet()
	fileID := fs.AddVirtual("test.sg", []byte(src))
	file := fs.Get(fileID)

	sharedStrings := source.NewInterner()
	typeInterner := types.NewInterner()

	bag := diag.NewBag(100)
	lx := lexer.New(file, lexer.Options{})
	builder := ast.NewBuilder(ast.Hints{}, sharedStrings)

	opts := parser.Options{
		Reporter:  &diag.BagReporter{Bag: bag},
		MaxErrors: 100,
	}

	result := parser.ParseFile(context.Background(), fs, lx, builder, opts)
	if bag.HasErrors() {
		for _, d := range bag.Items() {
			t.Logf("parse error: %v", d)
		}
		return nil, nil, fmt.Errorf("parse errors: %d", bag.Len())
	}

	// Run symbols resolution
	symbolsRes := symbols.ResolveFile(builder, result.File, &symbols.ResolveOptions{
		Reporter:   &diag.BagReporter{Bag: bag},
		Validate:   true,
		ModulePath: "core",
		FilePath:   "test.sg",
	})

	// Run sema
	semaOpts := sema.Options{
		Reporter:   &diag.BagReporter{Bag: bag},
		Symbols:    &symbolsRes,
		Types:      typeInterner,
		ModulePath: builder.StringsInterner.Intern("core"),
	}
	semaRes := sema.Check(context.Background(), builder, result.File, semaOpts)

	// Lower to HIR
	module, err := hir.LowerWithOptions(context.Background(), builder, result.File, &semaRes, &symbolsRes, lowerOpts)
	return module, typeInterner, err
}
