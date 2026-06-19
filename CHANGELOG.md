# Changelog

This changelog is generated with `scripts/update_changelog.sh`.
It keeps the latest release notes; older sections may be replaced.

## [0.1.12] - 2026-06-19

### CLI / Tooling

- Add release archive installer
- Add surge doctor
- Validate stdlib tree in doctor

### Documentation

- Use installed surge in Russian quickstart

## [0.1.11] - 2026-06-19

### Language

- Change exit function return type from nothing to T
- Standardize spacing in attribute declarations
- Accept exhaustive compare return blocks
- Handle shorthand casts consistently
- Diagnose compare scrutinee reuse

### Diagnostics

- Surface channel send diagnostics correctly
- Diagnose blocking channel calls in nonblocking functions

### Compiler

- Load task handles before runtime task calls
- Restore Erring<Option<T>, E> pipeline
- Resolve imported JsonValue method params
- Split method call checking units
- Consolidate command target resolution logic
- Discard control-flow results in side-effect contexts
- Preserve typed default arg lowering (#76)
- Lower time monotonic_now
- Tighten monotonic_now follow-up
- Avoid double deref for field reborrows
- Lower concrete clone methods (closes #85) (#86)
- Reject wrong explicit module import paths (#90)
- Short-circuit logical operators (closes #87)
- Lower fields through array element refs (closes #88)
- Lower imported magic methods (closes #98)

### Runtime

- Prune completed async scope children
- VM async exit poll
- Preserve borrowed async frame locals
- Reborrow shared calls from map mut refs
- Retain async borrow backing selectively
- Address review follow-ups
- Roll back task-state pin collection safely
- Wake net poller on new waiters
- Enable TCP_NODELAY on TCP sockets
- Preserve poll context for single-thread awaits
- Keep channel fanout progressing
- Exclude channel waits from running workers
- Keep channel fanout progressing under small thread counts
- Add native channel request-reply baseline
- Trace native net poll counters
- Expose native heap stats
- Trace compensation high-water mark
- Include trace counters in channel benchmark
- Add channel scheduler matrix
- Add channel wake placement experiment
- Support live async trace dumps
- Make live trace snapshots reliable
- Include task kind counts in trace snapshots
- Break down trace tasks by status
- Prioritize net join continuations
- Trace task-context channel blocking
- Hint channel request handoffs
- Inject channel handoff receivers
- Avoid wake signal on yielded tasks
- Defer channel recv ack wake signals
- Coallocate buffered channel storage
- Avoid signal on buffered channel refill
- Reuse native listener addresses
- Address async scheduler review

### Standard Library

- Entropy random UUID
- Add safe random helpers
- Lower duration intrinsics (closes #82)
- Add stable hash module
- Address hash review feedback
- Buffer tcp reads and writes

### CLI / Tooling

- Implement git module synchronization and update functionality
- Unify version metadata and checks
- Add Surge Issue Fixer skill and agent configuration
- Update .gitignore to include scribe/
- Resolve shadowed call targets (#89)
- Ignore local skvc package

### Documentation

- Remove hash planning drafts
- Document scheduler invariants
- Document async runtime diagnostics
- Document native async runtime

### Tests

- Cover async borrow capture rejection
- Update golden files
- Allow empty file-size check set
- Add native executor trace diagnostics
- Cover compensation scheduler counters
- Cover non-yielding channel handoffs
