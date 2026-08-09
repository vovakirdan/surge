package sema

import (
	"context"
	"strings"
	"testing"

	"surge/internal/diag"
)

// onCrossingPrelude declares a minimal placement-crossing surface so the sema
// unit tests do not depend on the real stdlib prelude. It mirrors the intrinsic
// declarations added to core/intrinsics.sg.
const onCrossingPrelude = `
tag Success<T>(T);
tag Cancelled();
type TaskResult<T> = Success(T) | Cancelled;
tag Some<T>(T);
tag None();
type Option<T> = Some(T) | None;
	@intrinsic @copy
	type Placement = { __opaque: int };
type ShardId = uint32;
type Channel<T> = { __opaque: int };
@shard_pinned
type TcpConn = { __opaque: int };
type Task<T> = { __opaque: int };
@shard_movable
type Movable = { id: int };
@nosend
type LocalOnly = { id: int };
type Plain = { id: int };
@intrinsic const pool: Placement;
@intrinsic const distributed: Placement;
@intrinsic fn shard(id: ShardId) -> Placement;
@intrinsic fn channel_on<T>(dst: Placement, capacity: uint) -> far Channel<T>;
`

// onCrossingCodes runs parse + sema on the prelude plus src and returns the set
// of emitted diagnostic code identifiers.
func onCrossingCodes(t *testing.T, src string) map[string]bool {
	t.Helper()
	builder, fileID, parseBag := parseSource(t, onCrossingPrelude+src)
	codes := map[string]bool{}
	for _, d := range parseBag.Items() {
		codes[d.Code.ID()] = true
	}
	symRes := resolveSymbols(t, builder, fileID)
	semaBag := diag.NewBag(64)
	Check(context.Background(), builder, fileID, Options{
		Reporter:   &diag.BagReporter{Bag: semaBag},
		Symbols:    symRes,
		ModulePath: builder.StringsInterner.Intern("core"),
	})
	for _, d := range semaBag.Items() {
		if d.Severity == diag.SevError {
			codes[d.Code.ID()] = true
		}
	}
	return codes
}

func TestUserDefinedPlacementIntrinsicDoesNotBecomeRuntimePlacement(t *testing.T) {
	src := `
@intrinsic @copy
type Placement = { __opaque: int };
type ShardId = uint32;
tag Success<T>(T);
tag Cancelled();
type TaskResult<T> = Success(T) | Cancelled;
tag Some<T>(T);
tag None();
type Option<T> = Some(T) | None;
@intrinsic const pool: Placement;

fn f() -> TaskResult<int> {
	return on pool { ret 1; };
}
`
	builder, fileID, parseBag := parseSource(t, src)
	if parseBag.HasErrors() {
		t.Fatalf("parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	symRes := resolveSymbols(t, builder, fileID)
	semaBag := diag.NewBag(64)
	Check(context.Background(), builder, fileID, Options{
		Reporter: &diag.BagReporter{Bag: semaBag},
		Symbols:  symRes,
	})
	codes := map[string]bool{}
	for _, d := range semaBag.Items() {
		if d.Severity == diag.SevError {
			codes[d.Code.ID()] = true
		}
	}
	if !codes["SEM3144"] {
		t.Fatalf("expected SEM3144 for user-defined Placement intrinsic, got: %s", joinCodes(codes))
	}
}

func TestOnCrossingDiagnostics(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string // expected diagnostic code; "" means no error expected
	}{
		// Destinations (ON-DST).
		{"placement_const_ok", `fn f() -> TaskResult<int> { return on pool { ret 1; }; }`, ""},
		{"placement_call_ok", `fn g(id: ShardId) -> TaskResult<int> { return on shard(id) { ret 1; }; }`, ""},
		{"far_task_dest", `fn f(t: far Task<int>) -> TaskResult<int> { return on t { ret 1; }; }`, "SEM3195"},
		{"integer_dest", `fn f(p: Placement) -> TaskResult<int> { let x: int = 1; return on x { ret 1; }; }`, "SEM3144"},
		{"bare_fn_dest", `fn r() -> Placement { return pool; } fn f() -> TaskResult<int> { return on r { ret 1; }; }`, "SEM3146"},

		// Result wrapping (ON-BODY).
		{"unwrapped_result", `fn f() -> int { return on pool { ret 1; }; }`, "SEM3149"},
		{"return_in_body", `fn f() -> TaskResult<int> { return on pool { return 1; }; }`, "SEM3147"},
		{"missing_ret", `fn f() -> TaskResult<int> { return on pool { let x: int = 1; let _ = x; }; }`, "SEM3148"},

		// Captures (ON-CAP).
		{"copy_capture_ok", `fn dbl(x: int) -> int { return x; } fn f(n: int) -> TaskResult<int> { return on pool { ret dbl(n); }; }`, ""},
		{"movable_capture_ok", `fn use(m: own Movable) -> int { return m.id; } fn f(m: own Movable) -> TaskResult<int> { return on pool { ret use(own m); }; }`, ""},
		{"borrow_capture", `fn rd(r: &Plain) -> int { return r.id; } fn f(p: Plain) -> TaskResult<int> { let r: &Plain = &p; return on pool { ret rd(r); }; }`, "SEM3165"},
		{"nosend_capture", `fn pk(v: own LocalOnly) -> int { return v.id; } fn f(v: own LocalOnly) -> TaskResult<int> { return on pool { ret pk(own v); }; }`, "SEM3166"},
		{"pinned_capture", `fn f(c: own TcpConn) -> TaskResult<int> { return on pool { let _ = c; ret 1; }; }`, "SEM3167"},
		{"unmarked_capture", `fn use(p: own Plain) -> int { return p.id; } fn f(p: own Plain) -> TaskResult<int> { return on pool { ret use(own p); }; }`, "SEM3168"},

		// Anchor + control-only (ON-ANCHOR / ON-TCP).
		{"unanchored_far", `fn f(a: far Channel<int>, b: far Channel<int>) -> TaskResult<nothing> { return on a { b.close(); ret nothing; }; }`, "SEM3150"},
		{"tcp_close_ok", `fn f(conn: far TcpConn) -> TaskResult<nothing> { return on conn { conn.close(); ret nothing; }; }`, ""},
		{"tcp_read_rejected", `fn f(conn: far TcpConn) -> TaskResult<nothing> { return on conn { conn.read(); ret nothing; }; }`, "SEM3151"},
		{"far_op_outside_on", `fn f(conn: far TcpConn) -> nothing { conn.close(); return nothing; }`, "SEM3194"},

		// Anchored channel operations carry the local surface (ON-CHAN):
		// send(own T) -> nothing, recv() -> Option<T>, close() -> nothing.
		{"anchored_send_ok", `fn f(ch: far Channel<int>) -> TaskResult<nothing> { return on ch { ch.send(1); ret nothing; }; }`, ""},
		{"anchored_recv_ok", `fn f(ch: far Channel<int>) -> TaskResult<int> { return on ch { let v: Option<int> = ch.recv(); let _ = v; ret 1; }; }`, ""},
		{"anchored_close_ok", `fn f(ch: far Channel<int>) -> TaskResult<nothing> { return on ch { ch.close(); ret nothing; }; }`, ""},
		{"anchored_send_missing_value", `fn f(ch: far Channel<int>) -> TaskResult<nothing> { return on ch { ch.send(); ret nothing; }; }`, "SEM3175"},
		{"anchored_send_wrong_type", `fn f(ch: far Channel<int>, p: Plain) -> TaskResult<nothing> { return on ch { ch.send(p); ret nothing; }; }`, "SEM3015"},
		{"anchored_recv_takes_no_args", `fn f(ch: far Channel<int>) -> TaskResult<nothing> { return on ch { let _ = ch.recv(1); ret nothing; }; }`, "SEM3175"},
		{"anchored_try_send_unsupported", `fn f(ch: far Channel<int>) -> TaskResult<nothing> { return on ch { let _ = ch.try_send(1); ret nothing; }; }`, "SEM3175"},
		// Vertical-1 anchored-body shape (ON-SHAPE): the operation is the
		// first statement's immediate expression; one operation per block.
		{"anchored_op_first_stmt_let_ok", `fn f(ch: far Channel<int>) -> TaskResult<int> { return on ch { let v: Option<int> = ch.recv(); let _ = v; ret 1; }; }`, ""},
		{"anchored_op_first_stmt_ret_ok", `fn f(ch: far Channel<int>) -> TaskResult<Option<int>> { return on ch { ret ch.recv(); }; }`, ""},
		{"anchored_two_ops_rejected", `fn f(ch: far Channel<int>) -> TaskResult<nothing> { return on ch { ch.send(1); ch.close(); ret nothing; }; }`, "SEM3175"},
		{"anchored_op_after_effect_rejected", `fn g() -> int { return 1; } fn f(ch: far Channel<int>) -> TaskResult<nothing> { return on ch { let x: int = g(); ch.send(x); ret nothing; }; }`, "SEM3175"},
		{"anchored_op_under_if_rejected", `fn f(ch: far Channel<int>, c: bool) -> TaskResult<nothing> { return on ch { if c { ch.send(1); } ret nothing; }; }`, "SEM3175"},

		// Sibling-lease mint surface (SHARE): borrowed receiver, zero args,
		// result is the same far channel type; the original stays usable.
		{"share_types_and_preserves_original", `async fn f(ch: far Channel<int>) -> nothing { let sib: far Channel<int> = ch.share(); let _ = sib; let again: far Channel<int> = ch.share(); let _ = again; return nothing; }`, ""},
		{"share_takes_no_arguments", `async fn f(ch: far Channel<int>) -> nothing { let _ = ch.share(1); return nothing; }`, "SEM3175"},

		// Remote select surface (SELECT-FAR): a select with far channel arms
		// ships whole to their owner shard; any other arm kind is diagnosed
		// with the split rewrite (owner sameness stays a runtime check — the
		// tokens carry the shard).
		{"select_far_recv_arms_ok", `async fn f(a: far Channel<int>, b: far Channel<int>) -> int { let w: int = select { a.recv() => 1; b.recv() => 2; }; return w; }`, ""},
		{"select_far_send_arm_ok", `async fn f(a: far Channel<int>, b: far Channel<int>) -> int { let w: int = select { a.send(7) => 1; b.recv() => 2; }; return w; }`, ""},
		{"select_far_mixed_local_rejected", `async fn f(a: far Channel<int>, l: own Channel<int>) -> int { let w: int = select { a.recv() => 1; l.recv() => 2; }; return w; }`, "SEM3176"},
		{"select_far_default_rejected", `async fn f(a: far Channel<int>) -> int { let w: int = select { a.recv() => 1; default => 2; }; return w; }`, "SEM3176"},
		{"select_far_task_arm_rejected", `async fn f(a: far Channel<int>, t: Task<int>) -> int { let w: int = select { a.recv() => 1; t.await() => 2; }; return w; }`, "SEM3176"},

		// Far send arms get the local send-arm ownership discipline too
		// (Task 3's rules, applied through the far inner channel's payload
		// type): a non-copy payload needs the `own` marker, must be a
		// whole binding, and the move is visible after the select join.
		{"select_far_send_missing_own", `async fn f(a: far Channel<string>, b: far Channel<int>, msg: string) -> int { let w: int = select { a.send(msg) => 1; b.recv() => 2; }; return w; }`, "SEM3140"},
		{"select_far_send_temp_payload_rejected", `fn mk() -> string { return "x"; } async fn f(a: far Channel<string>, b: far Channel<int>) -> int { let w: int = select { a.send(own mk()) => 1; b.recv() => 2; }; return w; }`, "SEM3141"},
		{"select_far_send_own_binding_ok", `async fn f(a: far Channel<string>, b: far Channel<int>, msg: string) -> int { let w: int = select { a.send(own msg) => 1; b.recv() => 2; }; return w; }`, ""},
		{"select_far_send_use_after_join_rejected", `async fn f(a: far Channel<string>, b: far Channel<int>, msg: string) -> string { let w: int = select { a.send(own msg) => 1; b.recv() => 2; }; return msg; }`, "SEM3130"},

		// Effect + structure (ON-NEST). The `crosses` requirement (SEM3162) is
		// retired: `on dst { }` is valid without a `crosses` marker (the effect is
		// inferred).
		{"on_without_crosses_ok", `fn f() -> TaskResult<int> { return on pool { ret 1; }; }`, ""},
		{"nested_on", `fn f() -> TaskResult<int> { return on pool { let inner: TaskResult<int> = on distributed { ret 1; }; ret 1; }; }`, "SEM3153"},
		{"suspend_in_blocking", `fn f() -> TaskResult<int> { blocking { let _ = on pool { ret 1; }; ret nothing; }; return on pool { ret 1; }; }`, "SEM3152"},
		{"anchored_suspend_in_blocking", `fn f(ch: far Channel<int>) -> TaskResult<nothing> { blocking { let _ = on ch { ch.send(1); ret nothing; }; ret nothing; }; return on ch { ch.send(2); ret nothing; }; }`, "SEM3152"},
		{"anchored_borrow_capture", `fn rd(r: &Plain) -> int { return r.id; } fn f(ch: far Channel<int>, p: Plain) -> TaskResult<nothing> { let r: &Plain = &p; return on ch { ch.send(1); let _ = rd(r); ret nothing; }; }`, "SEM3165"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			codes := onCrossingCodes(t, tc.src)
			if tc.want == "" {
				if len(codes) != 0 {
					t.Fatalf("expected no errors, got: %s", joinCodes(codes))
				}
				return
			}
			if !codes[tc.want] {
				t.Fatalf("expected diagnostic %s, got: %s", tc.want, joinCodes(codes))
			}
		})
	}
}

func joinCodes(codes map[string]bool) string {
	if len(codes) == 0 {
		return "(none)"
	}
	parts := make([]string, 0, len(codes))
	for c := range codes {
		parts = append(parts, c)
	}
	return strings.Join(parts, ", ")
}

// The moved-handle diagnostic must carry the share() fix per the kindness
// contract: naming the mechanism (per-holder leases) at the misuse site.
func TestFarChannelMovedHandleHintNamesShare(t *testing.T) {
	src := onCrossingPrelude + `
fn consume(ch: far Channel<int>) -> nothing { let _ = ch; return nothing; }
fn f(ch: far Channel<int>) -> nothing {
    let _ = consume(ch);
    let _ = consume(ch);
    return nothing;
}
`
	builder, fileID, parseBag := parseSource(t, src)
	if parseBag.HasErrors() {
		t.Fatalf("parse errors: %v", parseBag.Items())
	}
	symRes := resolveSymbols(t, builder, fileID)
	semaBag := diag.NewBag(64)
	Check(context.Background(), builder, fileID, Options{
		Reporter:   &diag.BagReporter{Bag: semaBag},
		Symbols:    symRes,
		ModulePath: builder.StringsInterner.Intern("core"),
	})
	for _, d := range semaBag.Items() {
		if d.Code == diag.SemaUseAfterMove {
			if !strings.Contains(d.Message, "share() before moving") {
				t.Fatalf("moved-handle message misses the share hint: %q", d.Message)
			}
			return
		}
	}
	t.Fatalf("expected a use-after-move diagnostic, got: %s", joinCodes(onCrossingBagCodes(semaBag)))
}

func onCrossingBagCodes(bag *diag.Bag) map[string]bool {
	codes := map[string]bool{}
	for _, d := range bag.Items() {
		codes[d.Code.ID()] = true
	}
	return codes
}

// TestFarSelectSendArmDropsSynthesizeOnLoser pins that a far select send
// arm rides the same per-arm drop synthesis as a local one: HIR only
// special-cases non-ChannelSelect crossings when lowering a select
// (internal/hir/lower_expr.go), so every arm.Result still lowers through
// l.lowerExpr and picks up the ArmDropsExpr tag the sema walker recorded
// here — the losing recv arm must carry the reclaim for the payload the
// winning send arm would have delivered.
func TestFarSelectSendArmDropsSynthesizeOnLoser(t *testing.T) {
	src := onCrossingPrelude + `
async fn f(a: far Channel<string>, b: far Channel<int>, msg: string) -> int {
    let w: int = select {
        a.send(own msg) => 1;
        b.recv() => 2;
    };
    return w;
}
`
	builder, fileID, parseBag := parseSource(t, src)
	if parseBag.HasErrors() {
		t.Fatalf("parse errors: %v", parseBag.Items())
	}
	symRes := resolveSymbols(t, builder, fileID)
	semaBag := diag.NewBag(64)
	res := Check(context.Background(), builder, fileID, Options{
		Reporter:   &diag.BagReporter{Bag: semaBag},
		Symbols:    symRes,
		ModulePath: builder.StringsInterner.Intern("core"),
	})
	if semaBag.HasErrors() {
		t.Fatalf("unexpected sema diagnostics: %s", diagnosticsSummary(semaBag))
	}
	if len(res.ArmDropsExpr) != 1 {
		t.Fatalf("expected exactly one arm-drop entry for the losing recv arm, got %d: %v", len(res.ArmDropsExpr), res.ArmDropsExpr)
	}
}
