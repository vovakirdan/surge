// Package crossinggate wires the Epic 11 crossing-surface golden fixtures into
// `go test` behind per-block gates.
//
// The active fixtures live under
// testdata/golden/crossing/block0{1..4}/{valid,invalid}. Before a block lands,
// its fixtures stay hidden behind `_` filenames and its gate below stays false,
// so `make check` stays green. Flipping a gate to true activates that block's
// contract: every negative fixture must emit the diagnostic code named in its
// `// EXPECT-DIAG:` header and every positive fixture must be error-free at the
// sema stage (parse + sema only; Epic 11 execution scope).
//
// Flip the gates in the documented implementation order (Block 1, then Blocks
// 2 and 3, then the inferred-effect / shard-attribute contracts in Block 4).
// See docs/runtime-v2-epics/11-tasks/README.md for the full flip procedure and
// the placeholder -> diagnostic-code mapping.
package crossinggate

// Per-block gates for the Epic 11 crossing fixtures. Each is independent; flip
// one to true when that block's parser + semantic analysis is implemented.
const (
	// Block1Enabled gates the `far` type-modifier fixtures.
	Block1Enabled = true
	// Block2Enabled gates the `on dst { ... }` placement-crossing fixtures.
	Block2Enabled = true
	// Block3Enabled gates the `spawn on dst { ... }` remote-spawn fixtures.
	Block3Enabled = true
	// Block4Enabled gates the inferred crossing-effect and shard-attribute
	// contract fixtures.
	Block4Enabled = true
)
