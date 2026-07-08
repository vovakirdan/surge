package parser

// Tests for the Epic 11 Block 2 grammar layer: the contextual `on dst { ... }`
// placement-crossing expression and the Block 4 grammar prerequisite (the
// contextual `crosses` function effect and the `@shard_movable` / `@shard_pinned`
// attribute targets). These exercise parsing and AST shape only; semantic
// analysis (destination typing, capture legality, crosses propagation) is owned
// by Block 4 sema and is not covered here.

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/lexer"
	"surge/internal/source"
	"testing"
)

// parseProgram parses a whole source string as a sequence of top-level items and
// returns the AST builder, the diagnostic bag, and the parsed file ID.
func parseProgram(t *testing.T, input string) (*ast.Builder, *diag.Bag, ast.FileID) {
	t.Helper()
	fs := source.NewFileSet()
	fileID := fs.AddVirtual("test.sg", []byte(input))
	file := fs.Get(fileID)

	bag := diag.NewBag(100)
	reporter := diag.BagReporter{Bag: bag}
	lx := lexer.New(file, lexer.Options{Reporter: reporter})
	arenas := ast.NewBuilder(ast.Hints{}, nil)

	p := &Parser{
		lx:     lx,
		arenas: arenas,
		file:   arenas.Files.New(lx.EmptySpan()),
		fs:     fs,
		opts:   Options{MaxErrors: 100, Reporter: reporter},
	}
	p.parseItems()
	return arenas, bag, p.file
}

func bagHasCode(bag *diag.Bag, code diag.Code) bool {
	if bag == nil {
		return false
	}
	for _, d := range bag.Items() {
		if d.Code == code {
			return true
		}
	}
	return false
}

// onExprs returns every ExprOn node allocated in the builder, in allocation
// order. Expr arena IDs are 1-based (see ast.Arena), so iteration runs 1..Len.
func onExprs(arenas *ast.Builder) []ast.ExprID {
	var out []ast.ExprID
	n := arenas.Exprs.Arena.Len()
	for i := uint32(1); i <= n; i++ {
		id := ast.ExprID(i)
		if expr := arenas.Exprs.Get(id); expr != nil && expr.Kind == ast.ExprOn {
			out = append(out, id)
		}
	}
	return out
}

// firstFnFlags returns the FnModifier flags of the first function item in the file.
func firstFnFlags(t *testing.T, arenas *ast.Builder, fileID ast.FileID) ast.FnModifier {
	t.Helper()
	file := arenas.Files.Get(fileID)
	if file == nil {
		t.Fatal("nil file")
	}
	for _, itemID := range file.Items {
		if fn, ok := arenas.Items.Fn(itemID); ok {
			return fn.Flags
		}
	}
	t.Fatal("no function item found")
	return 0
}

// identName returns the interned name of an ExprIdent, or "" otherwise.
func identName(arenas *ast.Builder, exprID ast.ExprID) string {
	if ident, ok := arenas.Exprs.Ident(exprID); ok && ident != nil {
		if name, ok := arenas.StringsInterner.Lookup(ident.Name); ok {
			return name
		}
	}
	return ""
}

// --- `crosses` function effect: acceptance and effect bit -------------------

func TestCrossesEffectAccepted(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"fn_with_return_type", "fn route() crosses -> int { ret 1; }"},
		{"fn_no_return_type", "fn notify() crosses { ret 1; }"},
		{"async_fn", "async fn route() crosses -> int { ret 1; }"},
		{"fn_declaration", "fn remote() crosses -> int;"},
		{"return_far_task_slot", "fn start() crosses -> int { ret 1; }"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arenas, bag, fileID := parseProgram(t, tt.input)
			if bag.Len() != 0 {
				t.Fatalf("expected no diagnostics, got: %s", diagnosticsSummary(bag))
			}
			flags := firstFnFlags(t, arenas, fileID)
			if flags&ast.FnModifierCrosses == 0 {
				t.Errorf("expected FnModifierCrosses to be set, flags=%b", flags)
			}
		})
	}
}

func TestCrossesEffectIndependentOfAsync(t *testing.T) {
	arenas, bag, fileID := parseProgram(t, "async fn route() crosses -> int { ret 1; }")
	if bag.Len() != 0 {
		t.Fatalf("unexpected diagnostics: %s", diagnosticsSummary(bag))
	}
	flags := firstFnFlags(t, arenas, fileID)
	if flags&ast.FnModifierCrosses == 0 {
		t.Error("expected FnModifierCrosses set")
	}
	if flags&ast.FnModifierAsync == 0 {
		t.Error("expected FnModifierAsync set alongside crosses")
	}
}

// --- `crosses` placement rejections (SYN2034) --------------------------------

func TestCrossesPlacementRejected(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"prefix_fn", "crosses fn route() -> int { ret 1; }"},
		{"after_return_type", "fn route() -> int crosses { ret 1; }"},
		{"before_params", "fn route crosses() -> int { ret 1; }"},
		{"duplicate", "fn route() crosses crosses -> int { ret 1; }"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, bag, _ := parseProgram(t, tt.input)
			if !bagHasCode(bag, diag.SynCrossesPlacement) {
				t.Fatalf("expected SynCrossesPlacement, got: %s", diagnosticsSummary(bag))
			}
		})
	}
}

// --- `crosses` on non-function targets (SYN2035) -----------------------------

func TestCrossesNonFnTargetRejected(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"type_target", "type crosses Data = { id: uint64 };"},
		{"field_target", "type Data = { crosses id: uint64 };"},
		{"let_target", "let crosses data = other;"},
		{"block_target", "fn f() { crosses { ret 1; } }"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, bag, _ := parseProgram(t, tt.input)
			if !bagHasCode(bag, diag.SynCrossesTarget) {
				t.Fatalf("expected SynCrossesTarget, got: %s", diagnosticsSummary(bag))
			}
		})
	}
}

// --- `crosses fn(...)` function-type syntax (SYN2036) ------------------------

func TestCrossesFnTypeRejected(t *testing.T) {
	_, bag, _ := parseProgram(t, "let cb: crosses fn(int) -> int = route;")
	if !bagHasCode(bag, diag.SynCrossesFnType) {
		t.Fatalf("expected SynCrossesFnType, got: %s", diagnosticsSummary(bag))
	}
}

// --- `crosses` back-compat as an identifier ----------------------------------

func TestCrossesIdentifierBackCompat(t *testing.T) {
	tests := []string{
		"let crosses = 1;",
		"let crosses: int = 1;",
	}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			letItem, arenas, bag := parseLetWithBag(t, input)
			if bag.Len() != 0 {
				t.Fatalf("expected clean parse, got: %s", diagnosticsSummary(bag))
			}
			if letItem == nil {
				t.Fatal("expected a let item")
			}
			name, _ := arenas.StringsInterner.Lookup(letItem.Name)
			if name != "crosses" {
				t.Errorf("expected binding name 'crosses', got %q", name)
			}
		})
	}
}

// --- `on` crossing expression: destinations and AST shape --------------------

func TestOnCrossingDestinations(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantIdent  string       // expected destination ident name, if any
		wantDstAny ast.ExprKind // else expected destination expr kind
	}{
		{"pool", "on pool { ret 1; }", "pool", 0},
		{"distributed", "on distributed { ret 1; }", "distributed", 0},
		{"placement_var", "on dst { ret 1; }", "dst", 0},
		{"shard_call", "on shard(id) { ret 1; }", "", ast.ExprCall},
		{"placement_fn", "on route_for(uid) { ret 1; }", "", ast.ExprCall},
		{"far_channel", "on ch { ch.send(own msg); ret nothing; }", "ch", 0},
		{"far_tcpconn", "on conn { conn.close(); ret nothing; }", "conn", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := "fn f() crosses { " + tt.body + "; }"
			arenas, bag, _ := parseProgram(t, src)
			if bag.Len() != 0 {
				t.Fatalf("expected clean parse, got: %s", diagnosticsSummary(bag))
			}
			ons := onExprs(arenas)
			if len(ons) != 1 {
				t.Fatalf("expected exactly 1 ExprOn, got %d", len(ons))
			}
			data, ok := arenas.Exprs.On(ons[0])
			if !ok || data == nil {
				t.Fatal("failed to load ExprOnData")
			}
			if data.Dest == ast.NoExprID {
				t.Fatal("ExprOn has no destination")
			}
			if !data.Body.IsValid() {
				t.Fatal("ExprOn has no body block")
			}
			if tt.wantIdent != "" {
				if got := identName(arenas, data.Dest); got != tt.wantIdent {
					t.Errorf("expected destination ident %q, got %q", tt.wantIdent, got)
				}
			} else {
				destExpr := arenas.Exprs.Get(data.Dest)
				if destExpr == nil || destExpr.Kind != tt.wantDstAny {
					t.Errorf("expected destination kind %v, got %v", tt.wantDstAny, destExpr)
				}
			}
		})
	}
}

// --- type-name `on` destination must parse as dst + body (sema rejects) ------

func TestOnTypeNameDestinationParses(t *testing.T) {
	// `on Job { ... }` where `Job` is a type name must parse as destination `Job`
	// plus a body block (struct-literal recognition suppressed), so sema can reject
	// the type-name destination (SEM3145) rather than the parser eating the body.
	arenas, bag, _ := parseProgram(t, "fn g() crosses -> int { return on Job { ret 1; }; }")
	if bag.Len() != 0 {
		t.Fatalf("expected clean parse, got: %s", diagnosticsSummary(bag))
	}
	ons := onExprs(arenas)
	if len(ons) != 1 {
		t.Fatalf("expected exactly 1 ExprOn, got %d", len(ons))
	}
	data, ok := arenas.Exprs.On(ons[0])
	if !ok || data == nil {
		t.Fatal("failed to load ExprOnData")
	}
	if got := identName(arenas, data.Dest); got != "Job" {
		t.Errorf("expected destination ident %q, got %q", "Job", got)
	}
	if !data.Body.IsValid() {
		t.Error("expected the `on` body block to be preserved, not consumed as a struct literal")
	}
}

func TestStructLiteralStillParsesInValuePosition(t *testing.T) {
	// The struct-literal suppression must be scoped to the `on` destination only.
	arenas, bag, _ := parseProgram(t, "fn f() { let x = Job { id: 1 }; }")
	if bag.Len() != 0 {
		t.Fatalf("expected clean parse of a struct literal, got: %s", diagnosticsSummary(bag))
	}
	if len(onExprs(arenas)) != 0 {
		t.Error("did not expect an ExprOn here")
	}
}

// --- literal `on` destinations parse as a crossing (sema rejects) ------------

func TestOnLiteralDestinationParses(t *testing.T) {
	// `on 1 { ... }` (and other literal heads) must parse as a crossing with the
	// literal as destination, so sema can reject the non-Placement destination
	// (SEM3144) rather than the parser treating `on` as an identifier.
	for _, body := range []string{
		"on 1 { ret 1; }",
		`on "x" { ret 1; }`,
		"on true { ret 1; }",
	} {
		t.Run(body, func(t *testing.T) {
			arenas, bag, _ := parseProgram(t, "fn g() crosses -> int { return "+body+"; }")
			if bag.Len() != 0 {
				t.Fatalf("expected clean parse, got: %s", diagnosticsSummary(bag))
			}
			ons := onExprs(arenas)
			if len(ons) != 1 {
				t.Fatalf("expected exactly 1 ExprOn, got %d", len(ons))
			}
			data, ok := arenas.Exprs.On(ons[0])
			if !ok || data == nil || data.Dest == ast.NoExprID {
				t.Fatal("expected an ExprOn with a literal destination")
			}
		})
	}
}

// --- `on` at every required expression-head position -------------------------

func TestOnExpressionPositions(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"statement_discard", "fn f() crosses { on pool { ret 1; }; }"},
		{"after_return", "fn f() crosses -> int { return on pool { ret 1; }; }"},
		{"after_assign", "fn f() crosses { let r = on pool { ret 1; }; }"},
		{"compare_scrutinee", "fn f() crosses { compare on pool { ret 1; } { Success(v) => v; Cancelled() => 0; } }"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arenas, bag, _ := parseProgram(t, tt.src)
			if bag.Len() != 0 {
				t.Fatalf("expected clean parse, got: %s", diagnosticsSummary(bag))
			}
			if len(onExprs(arenas)) != 1 {
				t.Fatalf("expected exactly 1 ExprOn in %q", tt.src)
			}
		})
	}
}

// --- `on` back-compat as an identifier ---------------------------------------

func TestOnIdentifierBackCompat(t *testing.T) {
	t.Run("let_binding_name", func(t *testing.T) {
		letItem, arenas, bag := parseLetWithBag(t, "let on = 1;")
		if bag.Len() != 0 {
			t.Fatalf("expected clean parse, got: %s", diagnosticsSummary(bag))
		}
		name, _ := arenas.StringsInterner.Lookup(letItem.Name)
		if name != "on" {
			t.Errorf("expected binding name 'on', got %q", name)
		}
	})
	t.Run("value_use", func(t *testing.T) {
		_, arenas, bag := parseLetWithBag(t, "let x = on + 1;")
		if bag.Len() != 0 {
			t.Fatalf("expected clean parse, got: %s", diagnosticsSummary(bag))
		}
		if len(onExprs(arenas)) != 0 {
			t.Error("`on` in value position must not parse as a crossing")
		}
	})
	// `on` followed by an operator, `.`, `(`, `[`, `;`, `=` stays an identifier.
	for _, src := range []string{
		"fn f() { let x = on.foo; }", // member access
		"fn f() { let x = on(1); }",  // call-argument / callee position
		"fn f() { let x = on[0]; }",  // index
		"fn f() { let x = on; }",     // `on` as the last token before `;`
		"fn f() { let x = on + 1; }", // binary operator
		"fn f() { g(on); }",          // `on` in call-argument position
		"fn f() { on = 1; }",         // `on` as an assignment target
		"fn f() { on; }",             // bare `on` expression statement
	} {
		t.Run(src, func(t *testing.T) {
			arenas, bag, _ := parseProgram(t, src)
			if bag.Len() != 0 {
				t.Fatalf("expected clean parse, got: %s", diagnosticsSummary(bag))
			}
			if len(onExprs(arenas)) != 0 {
				t.Errorf("expected no ExprOn for %q", src)
			}
		})
	}
}

// --- `@intrinsic` value-less const (Block 4 placement intrinsics) -------------

func TestIntrinsicConstValueless(t *testing.T) {
	// `@intrinsic pub const pool: Placement;` declares no initializer — its value
	// is runtime-provided, mirroring an `@intrinsic fn` with no body.
	arenas, bag, fileID := parseProgram(t, "@intrinsic pub const pool: Placement;")
	if bag.Len() != 0 {
		t.Fatalf("expected clean parse, got: %s", diagnosticsSummary(bag))
	}
	file := arenas.Files.Get(fileID)
	var found *ast.ConstItem
	for _, itemID := range file.Items {
		if c, ok := arenas.Items.Const(itemID); ok {
			found = c
			break
		}
	}
	if found == nil {
		t.Fatal("expected a const item")
	}
	if found.Value != ast.NoExprID {
		t.Error("expected value-less const (Value == NoExprID)")
	}
	if name, _ := arenas.StringsInterner.Lookup(found.Name); name != "pool" {
		t.Errorf("expected const name 'pool', got %q", name)
	}
}

func TestValuelessConstRequiresIntrinsic(t *testing.T) {
	// A value-less const WITHOUT @intrinsic still requires `= value`.
	_, bag, _ := parseProgram(t, "pub const pool: Placement;")
	if bag.Len() == 0 {
		t.Fatal("expected a diagnostic for a value-less non-intrinsic const")
	}
}

// --- postponed `on blocking` destination (FUT7012) ---------------------------

func TestOnBlockingDestinationRejected(t *testing.T) {
	_, bag, _ := parseProgram(t, "fn f() crosses { on blocking { ret 1; } }")
	if !bagHasCode(bag, diag.FutOnDestBlocking) {
		t.Fatalf("expected FutOnDestBlocking, got: %s", diagnosticsSummary(bag))
	}
}

// --- `spawn on` parses as a single remote-spawn node (Block 3) ----------------

func TestSpawnOnParsesAsRemoteSpawnNode(t *testing.T) {
	// `spawn on pool { ... }` parses as one ExprOn node with the Spawn flag set,
	// never as `spawn` applied to a separate `on` crossing.
	arenas, bag, _ := parseProgram(t, "fn f() crosses { spawn on pool { ret 1; }; }")
	if bag.Len() != 0 {
		t.Fatalf("expected clean parse, got: %s", diagnosticsSummary(bag))
	}
	ons := onExprs(arenas)
	if len(ons) != 1 {
		t.Fatalf("expected exactly 1 ExprOn, got %d", len(ons))
	}
	data, ok := arenas.Exprs.On(ons[0])
	if !ok || data == nil || !data.Spawn {
		t.Fatal("expected the ExprOn node to carry the Spawn flag")
	}
}
