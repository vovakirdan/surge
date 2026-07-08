package parser

// Tests for the Epic 11 Block 2 grammar layer: the contextual `on dst { ... }`
// placement-crossing expression. The explicit `crosses` function-effect keyword
// has been removed from the language (the effect is inferred by sema), so
// `crosses` is now an ordinary identifier and is covered as such here. These
// exercise parsing and AST shape only; semantic analysis (destination typing,
// capture legality, effect inference) is owned by sema and is not covered here.

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

// identName returns the interned name of an ExprIdent, or "" otherwise.
func identName(arenas *ast.Builder, exprID ast.ExprID) string {
	if ident, ok := arenas.Exprs.Ident(exprID); ok && ident != nil {
		if name, ok := arenas.StringsInterner.Lookup(ident.Name); ok {
			return name
		}
	}
	return ""
}

// --- `crosses` is an ordinary identifier (effect keyword removed) ------------

// The explicit `crosses` function-effect keyword was removed from the language;
// the effect is inferred by sema from metadata. `crosses` therefore parses as a
// plain identifier in every position, and none of the former placement/target
// diagnostics fire.

func TestCrossesIsPlainIdentifier(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"let_binding", "let crosses = 1;"},
		{"let_typed_binding", "let crosses: int = 1;"},
		{"fn_name", "fn crosses() -> int { ret 1; }"},
		{"param_name", "fn route(crosses: int) -> int { ret crosses; }"},
		{"type_name", "type crosses = int;"},
		{"field_name", "type Data = { crosses: int };"},
		{"value_use", "fn f() -> int { let x = crosses; ret x; }"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, bag, _ := parseProgram(t, tt.input)
			if bag.Len() != 0 {
				t.Fatalf("expected clean parse of `crosses` as an identifier, got: %s", diagnosticsSummary(bag))
			}
		})
	}
}

func TestCrossesBindingNamePreserved(t *testing.T) {
	for _, input := range []string{"let crosses = 1;", "let crosses: int = 1;"} {
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
			src := "fn f() { " + tt.body + "; }"
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
	arenas, bag, _ := parseProgram(t, "fn g() -> int { return on Job { ret 1; }; }")
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
			arenas, bag, _ := parseProgram(t, "fn g() -> int { return "+body+"; }")
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
		{"statement_discard", "fn f() { on pool { ret 1; }; }"},
		{"after_return", "fn f() -> int { return on pool { ret 1; }; }"},
		{"after_assign", "fn f() { let r = on pool { ret 1; }; }"},
		{"compare_scrutinee", "fn f() { compare on pool { ret 1; } { Success(v) => v; Cancelled() => 0; } }"},
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
	_, bag, _ := parseProgram(t, "fn f() { on blocking { ret 1; } }")
	if !bagHasCode(bag, diag.FutOnDestBlocking) {
		t.Fatalf("expected FutOnDestBlocking, got: %s", diagnosticsSummary(bag))
	}
}

// --- `spawn on` parses as a single remote-spawn node (Block 3) ----------------

func TestSpawnOnParsesAsRemoteSpawnNode(t *testing.T) {
	// `spawn on pool { ... }` parses as one ExprOn node with the Spawn flag set,
	// never as `spawn` applied to a separate `on` crossing.
	arenas, bag, _ := parseProgram(t, "fn f() { spawn on pool { ret 1; }; }")
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
