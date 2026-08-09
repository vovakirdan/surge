package sema

import "testing"

// TestSpawnOnDiagnostics covers the Block 3 `spawn on dst { ... }`
// remote-spawn surface and the `far Task<T>` lifecycle. It reuses the
// placement-crossing prelude (onCrossingPrelude) and the onCrossingCodes helper
// from on_crossing_test.go; extra declarations are supplied inline per case.
func TestSpawnOnDiagnostics(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string // expected diagnostic code; "" means no error expected
	}{
		// Destinations (D01-D13).
		{"dest_distributed_ok", `fn f() -> far Task<int> { return spawn on distributed { ret 1; }; }`, ""},
		{"dest_pool_ok", `fn f() -> far Task<int> { return spawn on pool { ret 1; }; }`, ""},
		{"dest_shard_ok", `fn f(id: ShardId) -> far Task<int> { return spawn on shard(id) { ret 1; }; }`, ""},
		{"dest_placement_var_ok", `fn f(p: Placement) -> far Task<int> { return spawn on p { ret 1; }; }`, ""},
		{"dest_route_fn_ok", `fn route_for(id: ShardId) -> Placement { return pool; } fn f(id: ShardId) -> far Task<int> { return spawn on route_for(id) { ret 1; }; }`, ""},
		{"dest_nonplacement_literal", `fn f() -> far Task<int> { return spawn on 1 { ret 1; }; }`, "SEM3154"},
		{"dest_type_name", `fn f() -> far Task<int> { return spawn on Plain { ret 1; }; }`, "SEM3155"},
		{"dest_bare_fn", `fn route_for(id: ShardId) -> Placement { return pool; } fn f() -> far Task<int> { return spawn on route_for { ret 1; }; }`, "SEM3156"},
		{"dest_nonplacement_route_fn", `fn bad(id: ShardId) -> int { return 1; } fn f(id: ShardId) -> far Task<int> { return spawn on bad(id) { ret 1; }; }`, "SEM3154"},
		{"dest_far_channel", `fn f(ch: far Channel<int>) -> far Task<int> { return spawn on ch { ret 1; }; }`, "SEM3157"},
		{"dest_far_tcp", `fn f(conn: far TcpConn) -> far Task<int> { return spawn on conn { ret 1; }; }`, "SEM3157"},
		{"dest_far_task", `fn f(t: far Task<int>) -> far Task<int> { return spawn on t { ret 1; }; }`, "SEM3158"},
		{"dest_blocking", `fn f() -> far Task<int> { return spawn on blocking { ret 1; }; }`, "FUT7013"},

		// Body (B01-B06).
		{"body_ret_nothing_ok", `fn f() -> far Task<nothing> { return spawn on pool { ret nothing; }; }`, ""},
		{"body_return_inside", `fn f() -> far Task<int> { return spawn on pool { return 1; }; }`, "SEM3159"},
		{"body_missing_ret", `fn f() -> far Task<int> { return spawn on pool { let x: int = 1; let _ = x; }; }`, "SEM3160"},
		{"body_unreachable_ret", `fn f() -> far Task<int> { return spawn on pool { ret 1; ret 2; }; }`, "SEM3161"},

		// Type identity (T02, T04, T11, T12) — `far Task<T>` is distinct from `Task<T>`.
		{"type_local_task_annotation", `fn f() -> nothing { let t: Task<int> = spawn on distributed { ret 1; }; let _ = t.await(); return nothing; }`, "SEM3015"},
		{"type_return_local_task", `fn f() -> Task<int> { return spawn on distributed { ret 1; }; }`, "SEM3015"},
		{"far_task_await_result_mismatch", `fn f(t: far Task<int>) -> int { return t.await(); }`, "SEM3015"},
		{"far_task_cancel_result_mismatch", `fn f(t: far Task<int>) -> nothing { return t.cancel(); }`, "SEM3015"},

		// far Task<T> operations (T05-T08, T10).
		// await/cancel are valid without a `crosses` marker (the effect is inferred;
		// T07/T08/SEM3164 retired).
		{"far_task_await_ok", `fn f(t: far Task<int>) -> TaskResult<int> { return t.await(); }`, ""},
		{"far_task_cancel_ok", `fn f(t: far Task<int>) -> TaskResult<nothing> { return t.cancel(); }`, ""},

		// Affine lifecycle (L01-L04).
		{"far_task_dropped", `fn f() -> nothing { let t: far Task<int> = spawn on pool { ret 1; }; return nothing; }`, "SEM3107"},
		{"far_task_double_await", `fn f(t: far Task<int>) -> TaskResult<int> { let a: TaskResult<int> = t.await(); let b: TaskResult<int> = t.await(); return a; }`, "SEM3130"},
		{"far_task_await_after_cancel", `fn f(t: far Task<int>) -> TaskResult<int> { t.cancel(); return t.await(); }`, "SEM3130"},
		{"far_task_return_ok", `fn f(t: far Task<int>) -> far Task<int> { return t; }`, ""},

		// Captures (C01-C10, B07).
		{"capture_copy_ok", `fn dbl(x: int) -> int { return x; } fn f(n: int) -> far Task<int> { return spawn on pool { ret dbl(n); }; }`, ""},
		{"capture_movable_ok", `fn use(m: own Movable) -> int { return m.id; } fn f(m: own Movable) -> far Task<int> { return spawn on pool { ret use(own m); }; }`, ""},
		{"capture_placement_ok", `fn f(p: Placement) -> far Task<int> { return spawn on pool { let q: Placement = p; let _ = q; ret 1; }; }`, ""},
		{"capture_borrow", `fn rd(r: &Plain) -> int { return r.id; } fn f(p: Plain) -> far Task<int> { let r: &Plain = &p; return spawn on pool { ret rd(r); }; }`, "SEM3165"},
		{"capture_unmarked", `fn use(p: own Plain) -> int { return p.id; } fn f(p: own Plain) -> far Task<int> { return spawn on pool { ret use(own p); }; }`, "SEM3168"},
		{"capture_nosend", `fn pk(v: own LocalOnly) -> int { return v.id; } fn f(v: own LocalOnly) -> far Task<int> { return spawn on pool { ret pk(own v); }; }`, "SEM3166"},
		{"capture_pinned", `fn f(c: own TcpConn) -> far Task<int> { return spawn on pool { let _ = c; ret 1; }; }`, "SEM3167"},
		{"capture_send_only", `@send type SendOnly = { id: int }; fn use(s: own SendOnly) -> int { return s.id; } fn f(s: own SendOnly) -> far Task<int> { return spawn on pool { ret use(own s); }; }`, "SEM3169"},
		{"capture_local_task", `fn f(lt: Task<int>) -> far Task<int> { return spawn on pool { let _ = lt; ret 1; }; }`, "SEM3168"},

		// @local (S07), missing-on (S06). `spawn on` in a plain (non-`crosses`)
		// function is valid — the crosses requirement (X03/SEM3162, X04/SEM3163) is
		// retired; the positive `dest_pool_ok` above already exercises this.
		{"local_spawn_on", `fn dbl(x: int) -> int { return x; } fn f(n: int) -> far Task<int> { return @local spawn on distributed { ret dbl(n); }; }`, "SEM3174"},
		// S06: `spawn distributed { ... }` stays on the local-spawn grammar and
		// reaches SemaSpawnNotTask (3111) because `distributed` is a `Placement`
		// value, not a `Task`. Statement position keeps the trailing block from
		// being parsed as a struct literal (matching the parser's own S06 test).
		{"spawn_missing_on", `fn f() -> far Task<int> { spawn distributed { ret 1; }; return spawn on pool { ret 1; }; }`, "SEM3111"},

		// Routing split (mandated regression): non-far-Task far operations still
		// reach the generic far-handle path (SEM3194), not the far Task await/cancel
		// branch.
		{"far_channel_send_routing", `fn f(ch: far Channel<int>) -> nothing { ch.send(1); return nothing; }`, "SEM3194"},
		{"far_tcp_close_routing", `fn f(conn: far TcpConn) -> nothing { conn.close(); return nothing; }`, "SEM3194"},
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
