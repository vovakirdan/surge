package parser

// Tests for the Epic 11 Block 3 grammar layer: the `spawn on <dst> { ... }`
// remote-spawn expression. These exercise parsing and AST shape only; semantic
// analysis (destination typing, `far Task<T>` result, capture legality, crosses
// propagation, and the `@local spawn on` rejection SEM3174) is owned by sema and
// is not covered here. The remote-spawn node reuses ast.ExprOn with the Spawn
// flag set, so it is never `spawn` applied to a separate `on` crossing.

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"testing"
)

// spawnOnExprs returns every ExprOn node whose Spawn flag is set (i.e. written as
// `spawn on ...`), in allocation order.
func spawnOnExprs(arenas *ast.Builder) []ast.ExprID {
	var out []ast.ExprID
	for _, id := range onExprs(arenas) {
		if data, ok := arenas.Exprs.On(id); ok && data != nil && data.Spawn {
			out = append(out, id)
		}
	}
	return out
}

// plainOnExprs returns every ExprOn node whose Spawn flag is clear (Block 2 `on`).
func plainOnExprs(arenas *ast.Builder) []ast.ExprID {
	var out []ast.ExprID
	for _, id := range onExprs(arenas) {
		if data, ok := arenas.Exprs.On(id); ok && data != nil && !data.Spawn {
			out = append(out, id)
		}
	}
	return out
}

// --- S01/S02/S03: remote spawn in every accepted expression position ----------

func TestSpawnOnRemoteExpressionPositions(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"S01_statement", "fn f() { spawn on distributed { ret 1; }; }"},
		{"S02_let_init", "fn f() { let t = spawn on pool { ret 1; }; }"},
		{"S03_return", "fn f() -> int { return spawn on distributed { ret 1; }; }"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arenas, bag, _ := parseProgram(t, tt.src)
			if bag.Len() != 0 {
				t.Fatalf("expected clean parse, got: %s", diagnosticsSummary(bag))
			}
			spawns := spawnOnExprs(arenas)
			if len(spawns) != 1 {
				t.Fatalf("expected exactly 1 spawn-on ExprOn, got %d", len(spawns))
			}
			data, ok := arenas.Exprs.On(spawns[0])
			if !ok || data == nil {
				t.Fatal("failed to load ExprOnData")
			}
			if data.Dest == ast.NoExprID {
				t.Error("spawn-on has no destination")
			}
			if !data.Body.IsValid() {
				t.Error("spawn-on has no body block")
			}
			if len(plainOnExprs(arenas)) != 0 {
				t.Error("spawn on must not also produce a plain `on` crossing node")
			}
		})
	}
}

// --- destination heads parse with struct-literal suppression ------------------

func TestSpawnOnDestinations(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantIdent string
		wantKind  ast.ExprKind
	}{
		{"distributed", "spawn on distributed { ret 1; }", "distributed", 0},
		{"pool", "spawn on pool { ret 1; }", "pool", 0},
		{"placement_var", "spawn on dst { ret 1; }", "dst", 0},
		{"shard_call", "spawn on shard(id) { ret 1; }", "", ast.ExprCall},
		{"route_fn_call", "spawn on route_for(uid) { ret 1; }", "", ast.ExprCall},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := "fn f() { let t = " + tt.body + "; }"
			arenas, bag, _ := parseProgram(t, src)
			if bag.Len() != 0 {
				t.Fatalf("expected clean parse, got: %s", diagnosticsSummary(bag))
			}
			spawns := spawnOnExprs(arenas)
			if len(spawns) != 1 {
				t.Fatalf("expected exactly 1 spawn-on ExprOn, got %d", len(spawns))
			}
			data, _ := arenas.Exprs.On(spawns[0])
			if tt.wantIdent != "" {
				if got := identName(arenas, data.Dest); got != tt.wantIdent {
					t.Errorf("expected destination ident %q, got %q", tt.wantIdent, got)
				}
			} else {
				destExpr := arenas.Exprs.Get(data.Dest)
				if destExpr == nil || destExpr.Kind != tt.wantKind {
					t.Errorf("expected destination kind %v, got %v", tt.wantKind, destExpr)
				}
			}
		})
	}
}

// `spawn on Job { ... }` where `Job` is a type name must parse as destination
// `Job` plus a body block (struct-literal recognition suppressed), so sema can
// reject the type destination (SEM3155) rather than the parser eating the body.
func TestSpawnOnTypeNameDestinationParses(t *testing.T) {
	arenas, bag, _ := parseProgram(t, "fn g() -> int { return spawn on Job { ret 1; }; }")
	if bag.Len() != 0 {
		t.Fatalf("expected clean parse, got: %s", diagnosticsSummary(bag))
	}
	spawns := spawnOnExprs(arenas)
	if len(spawns) != 1 {
		t.Fatalf("expected exactly 1 spawn-on ExprOn, got %d", len(spawns))
	}
	data, _ := arenas.Exprs.On(spawns[0])
	if got := identName(arenas, data.Dest); got != "Job" {
		t.Errorf("expected destination ident %q, got %q", "Job", got)
	}
	if !data.Body.IsValid() {
		t.Error("expected the spawn-on body block to be preserved, not consumed as a struct literal")
	}
}

// --- S04: missing block → SYN2032 --------------------------------------------

func TestSpawnOnMissingBlock(t *testing.T) {
	arenas, bag, _ := parseProgram(t, "fn f() { spawn on distributed; }")
	if !bagHasCode(bag, diag.SynSpawnOnMissingBlock) {
		t.Fatalf("expected SynSpawnOnMissingBlock (SYN2032), got: %s", diagnosticsSummary(bag))
	}
	// The destination is still captured on the recovery node.
	spawns := spawnOnExprs(arenas)
	if len(spawns) != 1 {
		t.Fatalf("expected 1 spawn-on recovery node, got %d", len(spawns))
	}
	data, _ := arenas.Exprs.On(spawns[0])
	if got := identName(arenas, data.Dest); got != "distributed" {
		t.Errorf("expected destination ident %q, got %q", "distributed", got)
	}
}

// --- S05: missing destination → SYN2033 --------------------------------------

func TestSpawnOnMissingDestination(t *testing.T) {
	arenas, bag, _ := parseProgram(t, "fn f() { spawn on { ret 1; }; }")
	if !bagHasCode(bag, diag.SynSpawnOnMissingDestination) {
		t.Fatalf("expected SynSpawnOnMissingDestination (SYN2033), got: %s", diagnosticsSummary(bag))
	}
	spawns := spawnOnExprs(arenas)
	if len(spawns) != 1 {
		t.Fatalf("expected 1 spawn-on recovery node, got %d", len(spawns))
	}
	data, _ := arenas.Exprs.On(spawns[0])
	if data.Dest != ast.NoExprID {
		t.Error("expected no destination on the missing-destination recovery node")
	}
	if !data.Body.IsValid() {
		t.Error("expected the body block to be consumed for recovery")
	}
}

// --- S06: `spawn distributed { ... }` stays on the local-spawn path -----------

func TestSpawnWithoutOnRoutesToLocalSpawn(t *testing.T) {
	// No `on`: `spawn` consumes the postfix expression and this must NOT become a
	// remote-spawn node nor emit a missing-`on` parse code. Its rejection is the
	// existing SemaSpawnNotTask (3111) at sema time.
	arenas, bag, _ := parseProgram(t, "fn f() { spawn distributed { ret 1; }; }")
	if len(spawnOnExprs(arenas)) != 0 {
		t.Error("`spawn distributed { ... }` must not parse as a remote spawn-on node")
	}
	if bagHasCode(bag, diag.SynSpawnOnMissingBlock) || bagHasCode(bag, diag.SynSpawnOnMissingDestination) {
		t.Fatalf("`spawn` without `on` must not emit a spawn-on parse code, got: %s", diagnosticsSummary(bag))
	}
}

// --- S07: `@local spawn on ...` must PARSE (sema rejects with SEM3174) --------

func TestLocalSpawnOnParses(t *testing.T) {
	arenas, bag, _ := parseProgram(t, "fn f() { @local spawn on distributed { ret 1; }; }")
	if bag.Len() != 0 {
		t.Fatalf("`@local spawn on` must parse cleanly (SEM3174 is a sema error), got: %s", diagnosticsSummary(bag))
	}
	spawns := spawnOnExprs(arenas)
	if len(spawns) != 1 {
		t.Fatalf("expected exactly 1 spawn-on ExprOn, got %d", len(spawns))
	}
	data, _ := arenas.Exprs.On(spawns[0])
	if got := identName(arenas, data.Dest); got != "distributed" {
		t.Errorf("expected destination ident %q, got %q", "distributed", got)
	}
	// The `@local` attribute must be preserved on the node so sema can reject it
	// with SEM3174.
	if data.AttrCount != 1 {
		t.Errorf("expected the `@local` attribute to be carried on the node, got AttrCount=%d", data.AttrCount)
	}
}

// --- `spawn on blocking` postponed destination → FUT7013 ----------------------

func TestSpawnOnBlockingDestinationRejected(t *testing.T) {
	_, bag, _ := parseProgram(t, "fn f() { spawn on blocking { ret 1; }; }")
	if !bagHasCode(bag, diag.FutSpawnOnDestBlocking) {
		t.Fatalf("expected FutSpawnOnDestBlocking (FUT7013), got: %s", diagnosticsSummary(bag))
	}
}

// --- `return` inside a spawn-on body parses (SEM3159 is a sema error) ---------

func TestSpawnOnBodyReturnParses(t *testing.T) {
	// `return` inside the remote body is a semantic error (SEM3159), not a parse
	// error: the body must still parse `return` normally.
	arenas, bag, _ := parseProgram(t, "fn f() { let t = spawn on pool { return 1; }; }")
	if bag.Len() != 0 {
		t.Fatalf("expected clean parse of `return` inside spawn-on body, got: %s", diagnosticsSummary(bag))
	}
	if len(spawnOnExprs(arenas)) != 1 {
		t.Fatal("expected exactly 1 spawn-on ExprOn")
	}
}

// --- back-compat: identifier `on` after `spawn` stays a local spawn -----------

func TestSpawnOnIdentifierBackCompat(t *testing.T) {
	// `spawn on;` — `on` is followed by a terminator, so it is the ordinary
	// identifier `on` spawned locally, not a remote spawn.
	for _, src := range []string{
		"fn f() { let on = 1; spawn on; }",
		"fn f() { let on = 1; let x = spawn on; }",
		"fn f() { let on = 1; spawn on.await(); }",
	} {
		t.Run(src, func(t *testing.T) {
			arenas, bag, _ := parseProgram(t, src)
			if bag.Len() != 0 {
				t.Fatalf("expected clean parse, got: %s", diagnosticsSummary(bag))
			}
			if len(spawnOnExprs(arenas)) != 0 {
				t.Errorf("expected no spawn-on node for %q", src)
			}
		})
	}
}
