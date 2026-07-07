// Package crossinggate wires the Epic 11 explicit-crossing golden fixtures into
// `go test` behind per-block gates.
//
// The fixtures live under testdata/golden/crossing/block0{1..4}/{valid,invalid}
// and are `_`-prefixed so the shell golden runner (scripts/golden_update.sh)
// skips them: they cannot yet produce their target diagnostics because the
// far / on / spawn on / crosses surface is not implemented. Until a block lands,
// its gate below stays false and its fixtures are skipped, so `make check` stays
// green. Flipping a gate to true activates that block's fixtures: every negative
// fixture must emit the diagnostic code named in its `// EXPECT-DIAG:` header and
// every positive fixture must be error-free at the sema stage (parse + sema only;
// Epic 11 execution scope).
//
// Flip the gates in the documented implementation order (Block 1, then the
// Block 4 grammar slice, then Blocks 2 and 3, then the Block 4 sema slice). See
// docs/runtime-v2-epics/11-tasks/README.md for the full flip procedure and the
// placeholder -> diagnostic-code mapping.
package crossinggate

// Per-block gates for the Epic 11 crossing fixtures. Each is independent; flip
// one to true when that block's parser + semantic analysis is implemented.
const (
	// Block1Enabled gates the `far` type-modifier fixtures.
	Block1Enabled = true
	// Block2Enabled gates the `on dst { ... }` placement-crossing fixtures.
	Block2Enabled = false
	// Block3Enabled gates the `spawn on dst { ... }` remote-spawn fixtures.
	Block3Enabled = false
	// Block4Enabled gates the crossing-contract fixtures (`crosses`,
	// `@shard_movable`, `@shard_pinned`).
	Block4Enabled = false
)
