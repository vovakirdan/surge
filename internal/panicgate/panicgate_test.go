package panicgate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("panicgate: get working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "Makefile")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "runtime", "native")); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("panicgate: no repository root above the working directory")
		}
		dir = parent
	}
}

func allSites(t *testing.T, root string) []Site {
	t.Helper()
	c, err := ScanCRuntime(root)
	if err != nil {
		t.Fatalf("scan the C runtime: %v", err)
	}
	g, err := ScanEmitter(root)
	if err != nil {
		t.Fatalf("scan the emitter: %v", err)
	}
	out := append(append([]Site{}, c...), g...)
	sortSites(out)
	return out
}

// The gate. Every place the compiler or the C runtime can raise a panic is
// either reached by a behavioural fixture that records the answer on BOTH
// backends, or carries a row in the ledger pointing at a reasoned disposition.
// The ledger is checked in both directions, so a row that has been overtaken by
// a fixture, or that no longer describes any raise, is as much a failure as an
// uncovered raise.
func TestPanicSitesAreCoveredOrExcused(t *testing.T) {
	root := repoRoot(t)
	sites := allSites(t, root)

	recorded, err := ReadRecordedPanics(root)
	if err != nil {
		t.Fatalf("read the recorded panics: %v", err)
	}
	coverage := CoverageIndex(recorded)

	list, err := LoadAllowlist(filepath.Join(root, AllowlistPath))
	if err != nil {
		t.Fatalf("load the ledger: %v", err)
	}

	f := Check(sites, coverage, list)
	for i := range f.Uncovered {
		s := &f.Uncovered[i]
		t.Errorf("uncovered panic site: %s\n"+
			"  no fixture records this message on both backends, and no ledger row excuses it.\n"+
			"  Write a fixture that reaches it, or add to %s:\n    %s",
			s, AllowlistPath, RowFor(s))
	}
	for _, r := range f.Renumbered {
		t.Errorf("ledger row %q has moved to %q (message %q is unchanged)\n"+
			"  a raise was added or removed above it in the same function; update the key",
			r.Row.Site, r.Site.Key(), r.Row.Message)
	}
	for _, d := range f.Drifted {
		t.Errorf("panic wording drifted at %s\n  ledger records %q\n  source raises  %q\n"+
			"  if the new wording is right, update the row; if a fixture now reaches it, delete the row",
			d.Row.Site, d.Recorded, d.Actual)
	}
	for _, r := range f.Stale {
		t.Errorf("stale ledger row %q (message %q): no raise there any more; delete it", r.Site, r.Message)
	}
	for _, r := range f.Redundant {
		t.Errorf("ledger row %q is redundant: a fixture now records %q on both backends. Delete the row",
			r.Site, r.Message)
	}
	for _, r := range f.UnknownGroup {
		t.Errorf("ledger row %q points at group %q, which is not defined", r.Site, r.Group)
	}
	for _, g := range f.UnusedGroup {
		t.Errorf("group %q is defined and no row uses it; delete it or the reason rots unread", g.ID)
	}
	for _, d := range f.Duplicate {
		t.Errorf("two ledger rows for site %q", d)
	}
	if f.Unsorted != "" {
		t.Errorf("ledger rows are out of order at %q; keep them sorted by site", f.Unsorted)
	}
}

// A recorded answer nothing executes is not coverage. This is checked
// separately from the ledger because it is a different failure: the corpus
// looking complete while a sweep quietly drops what it records.
func TestEveryRecordedFixtureIsActuallyRun(t *testing.T) {
	root := repoRoot(t)
	dead, err := DeadFixtures(root)
	if err != nil {
		t.Fatalf("check the corpus wiring: %v", err)
	}
	for _, d := range dead {
		t.Errorf("fixture %s has a recorded exit code and never runs: %s", d.Fixture, d.Reason)
	}
}

// The census canary. A scan that silently found nothing would make every other
// check in this file pass, which is the shape of green gate this package exists
// to replace. The floors are far below the real counts; they catch a regex or a
// walker that stopped matching, not a panic that was legitimately removed.
func TestPanicScanFindsTheKnownSurface(t *testing.T) {
	root := repoRoot(t)
	c, err := ScanCRuntime(root)
	if err != nil {
		t.Fatalf("scan the C runtime: %v", err)
	}
	g, err := ScanEmitter(root)
	if err != nil {
		t.Fatalf("scan the emitter: %v", err)
	}
	if len(c) < 120 {
		t.Errorf("the C runtime scan found only %d raises; the walker or the raiser table drifted", len(c))
	}
	if len(g) < 90 {
		t.Errorf("the emitter scan found only %d raises; the walker or the raiser table drifted", len(g))
	}

	// Named landmarks, one per way a raise is found: a literal handed to a
	// discovered wrapper, a message read from a local assignment, the bounds
	// reporter's kind table, a reporter reached through two levels of
	// forwarding, and an emitter helper's interned constant.
	want := map[string]string{
		"runtime/native/rt_array.c::array_panic_view_not_resizable#1":           "array view is not resizable",
		"runtime/native/rt_string.c::rt_string_concat#1":                        "string concat length out of range",
		"runtime/native/rt_array.c::array_panic_out_of_bounds#1":                "index {} out of bounds for length {}",
		"runtime/native/rt_ready_queue.c::ready_push_task_locked#1":             "async: local queue overflow",
		"runtime/native/rt_bignum_int_api.c::rt_bigint_shl#1":                   "integer overflow",
		"internal/backend/llvm/emit_helpers_overflow.go::emitShiftCountGuard#1": "integer overflow",
		"internal/backend/llvm/emit_async.go::emitPollDispatch#1":               "missing poll function",
		"internal/backend/llvm/emit_intrinsics_net.go::emitNetListen#1":         "net listen port out of range",
	}
	found := map[string]string{}
	for _, s := range append(append([]Site{}, c...), g...) {
		found[s.Key()] = s.Message
	}
	for key, msg := range want {
		got, ok := found[key]
		if !ok {
			t.Errorf("the scan no longer finds the raise at %s", key)
			continue
		}
		if got != msg {
			t.Errorf("%s raises %q; the census expected %q", key, got, msg)
		}
	}
}

// Every reporter that writes a panic or fatal line and exits must be one the
// scan follows. rt_bignum_panic.c and rt_fatal.c format their own reports
// rather than forwarding through another reporter, which is exactly the way a
// reporter gets missed, so the scan's seed table is checked against the tree
// instead of trusted.
func TestPanicReportersAreAllKnown(t *testing.T) {
	const helperWriter = `
static void write_all(void) { write(STDERR_FILENO, "x", 1); }
static void fatal(void) { write_all(); _exit(1); }
static void not_a_reporter(void) { _exit(1); }
`
	canary := cReporters(helperWriter)
	if !canary["fatal"] || canary["not_a_reporter"] {
		t.Fatalf("reporter scan does not follow a raw stderr-write helper exactly: %#v", canary)
	}

	root := repoRoot(t)
	dir := filepath.Join(root, "runtime", "native")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read the runtime: %v", err)
	}
	known := primitiveRaisers()
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".c") {
			continue
		}
		raw, readErr := os.ReadFile(filepath.Join(dir, ent.Name())) // #nosec G304 -- repository-owned path
		if readErr != nil {
			t.Fatalf("read %s: %v", ent.Name(), readErr)
		}
		for fn := range cReporters(stripCComments(string(raw))) {
			if _, ok := known[fn]; ok {
				continue
			}
			t.Errorf("%s: %s writes to stderr and exits the process but is not a known reporter; "+
				"seed it in primitiveRaisers or every raise beneath it is invisible", ent.Name(), fn)
		}
	}
}

// The scan looks at one emitter package. If a second one starts writing panic
// calls into a module, everything it raises is outside the census.
func TestEmitterRaisesOnlyFromTheKnownPackage(t *testing.T) {
	root := repoRoot(t)
	var offenders []string
	err := filepath.Walk(filepath.Join(root, "internal"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		// The emitter itself, and this package, which quotes the same text in
		// order to look for it.
		if strings.HasPrefix(rel, emitterDir+"/") || strings.HasPrefix(rel, "internal/panicgate/") {
			return nil
		}
		raw, readErr := os.ReadFile(path) // #nosec G304 -- walked from the repository root
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(raw), "@rt_panic") || strings.Contains(string(raw), "@rt_fatal_static") {
			offenders = append(offenders, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal: %v", err)
	}
	for _, o := range offenders {
		t.Errorf("%s writes a panic call into a module but is outside %s, so the scan never sees it", o, emitterDir)
	}
}
