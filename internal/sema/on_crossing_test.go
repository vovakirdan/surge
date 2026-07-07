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
@copy
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
		Reporter: &diag.BagReporter{Bag: semaBag},
		Symbols:  symRes,
	})
	for _, d := range semaBag.Items() {
		if d.Severity == diag.SevError {
			codes[d.Code.ID()] = true
		}
	}
	return codes
}

func TestOnCrossingDiagnostics(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string // expected diagnostic code; "" means no error expected
	}{
		// Destinations (ON-DST).
		{"placement_const_ok", `fn f() crosses -> TaskResult<int> { return on pool { ret 1; }; }`, ""},
		{"placement_call_ok", `fn g(id: ShardId) crosses -> TaskResult<int> { return on shard(id) { ret 1; }; }`, ""},
		{"far_task_dest", `fn f(t: far Task<int>) crosses -> TaskResult<int> { return on t { ret 1; }; }`, "SEM3143"},
		{"integer_dest", `fn f(p: Placement) crosses -> TaskResult<int> { let x: int = 1; return on x { ret 1; }; }`, "SEM3144"},
		{"bare_fn_dest", `fn r() -> Placement { return pool; } fn f() crosses -> TaskResult<int> { return on r { ret 1; }; }`, "SEM3146"},

		// Result wrapping (ON-BODY).
		{"unwrapped_result", `fn f() crosses -> int { return on pool { ret 1; }; }`, "SEM3149"},
		{"return_in_body", `fn f() crosses -> TaskResult<int> { return on pool { return 1; }; }`, "SEM3147"},
		{"missing_ret", `fn f() crosses -> TaskResult<int> { return on pool { let x: int = 1; let _ = x; }; }`, "SEM3148"},

		// Captures (ON-CAP).
		{"copy_capture_ok", `fn dbl(x: int) -> int { return x; } fn f(n: int) crosses -> TaskResult<int> { return on pool { ret dbl(n); }; }`, ""},
		{"movable_capture_ok", `fn use(m: own Movable) -> int { return m.id; } fn f(m: own Movable) crosses -> TaskResult<int> { return on pool { ret use(own m); }; }`, ""},
		{"borrow_capture", `fn rd(r: &Plain) -> int { return r.id; } fn f(p: Plain) crosses -> TaskResult<int> { let r: &Plain = &p; return on pool { ret rd(r); }; }`, "SEM3165"},
		{"nosend_capture", `fn pk(v: own LocalOnly) -> int { return v.id; } fn f(v: own LocalOnly) crosses -> TaskResult<int> { return on pool { ret pk(own v); }; }`, "SEM3166"},
		{"pinned_capture", `fn f(c: own TcpConn) crosses -> TaskResult<int> { return on pool { let _ = c; ret 1; }; }`, "SEM3167"},
		{"unmarked_capture", `fn use(p: own Plain) -> int { return p.id; } fn f(p: own Plain) crosses -> TaskResult<int> { return on pool { ret use(own p); }; }`, "SEM3168"},

		// Anchor + control-only (ON-ANCHOR / ON-TCP).
		{"unanchored_far", `fn f(a: far Channel<int>, b: far Channel<int>) crosses -> TaskResult<nothing> { return on a { b.close(); ret nothing; }; }`, "SEM3150"},
		{"tcp_close_ok", `fn f(conn: far TcpConn) crosses -> TaskResult<nothing> { return on conn { conn.close(); ret nothing; }; }`, ""},
		{"tcp_read_rejected", `fn f(conn: far TcpConn) crosses -> TaskResult<nothing> { return on conn { conn.read(); ret nothing; }; }`, "SEM3151"},
		{"far_op_outside_on", `fn f(conn: far TcpConn) crosses -> nothing { conn.close(); return nothing; }`, "SEM3142"},

		// Effect + structure (ON-CROSS / ON-NEST).
		{"missing_crosses", `fn f() -> TaskResult<int> { return on pool { ret 1; }; }`, "SEM3162"},
		{"nested_on", `fn f() crosses -> TaskResult<int> { return on pool { let inner: TaskResult<int> = on distributed { ret 1; }; ret 1; }; }`, "SEM3153"},
		{"suspend_in_blocking", `fn f() crosses -> TaskResult<int> { blocking { let _ = on pool { ret 1; }; ret nothing; }; return on pool { ret 1; }; }`, "SEM3152"},
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
