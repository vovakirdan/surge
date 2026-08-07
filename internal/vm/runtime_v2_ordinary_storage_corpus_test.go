//go:build !golden
// +build !golden

package vm_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The ordinary-storage corpus: one shared set of Surge sources that both
// backend workstreams drive, owned by neither.
//
// Every source is written to be representation-free. It names no box, no
// pointer, no count and no allocation total, and each row asserts a property of
// the LANGUAGE that has to hold whatever the storage underneath looks like. A
// row that could tell the current representation from the next one would be
// pinning the thing that is about to change, so the corpus is green before the
// change, green after it, and neither backend has to edit a fixture to cut over.
//
// Each source self-checks and reports through its exit code and one line of
// stdout, so a divergence between two backends is a diff of two small strings
// rather than an inspection of two runtimes.

const ordinaryStorageCorpusDir = "testdata/runtime-v2-ordinary-storage"

// ordinaryStorageFixture is one corpus row. Name is also the built binary's
// name, so it must stay unique across the whole corpus.
type ordinaryStorageFixture struct {
	name string
	dir  string
	want string
}

func (f ordinaryStorageFixture) relPath() string {
	return filepath.ToSlash(filepath.Join(ordinaryStorageCorpusDir, f.dir, f.name+".sg"))
}

// The frozen nested family — a struct holding a tuple, a two-arm tagged union
// with a composite payload, and a fixed array of composites — driven through
// every operation the ordinary destination protocol has to carry, followed by
// the rows that exist for their layout rather than their operation.
//
// The last two layout rows were quarantined when the corpus was frozen: on the
// boxed representation both produced the right answer and then aborted the
// native process while releasing a zero-sized member. Neither aborts now — a
// zero-sized value has no storage of its own to be released twice — so they are
// ordinary rows, and their quarantine is gone rather than adapted.
func ordinaryStorageFixtures() []ordinaryStorageFixture {
	return []ordinaryStorageFixture{
		{name: "storage_construct", dir: "ops", want: "construct ok"},
		{name: "storage_copy", dir: "ops", want: "copy ok"},
		{name: "storage_move", dir: "ops", want: "move ok"},
		{name: "storage_borrow", dir: "ops", want: "borrow ok"},
		{name: "storage_partial_move", dir: "ops", want: "partial move ok"},
		{name: "storage_overwrite", dir: "ops", want: "overwrite ok"},
		{name: "storage_direct_call", dir: "ops", want: "direct call ok"},
		{name: "storage_indirect_call", dir: "ops", want: "indirect call ok"},
		{name: "storage_return", dir: "ops", want: "return ok"},
		{name: "storage_early_exit", dir: "ops", want: "early exit ok"},
		{name: "storage_partial_init", dir: "ops", want: "partial init ok"},

		{name: "storage_packed_field", dir: "layout", want: "packed field ok"},
		{name: "storage_over_aligned", dir: "layout", want: "over aligned ok"},
		{name: "storage_padding_union", dir: "layout", want: "padding union ok"},
		{name: "storage_zero_sized", dir: "layout", want: "zero sized ok"},
		{name: "storage_zero_sized_member_order", dir: "layout", want: "zero sized member order ok"},
		{name: "storage_zero_sized_array", dir: "layout", want: "zero sized array ok"},
	}
}

// TestRuntimeV2OrdinaryStorageCorpusIsComplete fails when a source appears in
// the corpus tree without a row, or a row names a source that is not there.
//
// The corpus is shared by two workstreams that will each be tempted to drop one
// more file into it, and a fixture nothing runs is worse than no fixture: it
// reads like coverage. This is what keeps the tree and the table the same set.
func TestRuntimeV2OrdinaryStorageCorpusIsComplete(t *testing.T) {
	root := repoRoot(t)

	declared := make(map[string]bool)
	for _, fixture := range ordinaryStorageFixtures() {
		if declared[fixture.name] {
			t.Fatalf("duplicate corpus row %q", fixture.name)
		}
		declared[fixture.name] = true
	}

	found := make(map[string]bool)
	for _, dir := range []string{"ops", "layout"} {
		base := filepath.Join(root, ordinaryStorageCorpusDir, dir)
		entries, err := os.ReadDir(base)
		if err != nil {
			t.Fatalf("read corpus directory %s: %v", dir, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sg") {
				t.Fatalf("unexpected entry %q in %s", entry.Name(), dir)
			}
			found[strings.TrimSuffix(entry.Name(), ".sg")] = true
		}
	}

	var missing, extra []string
	for name := range declared {
		if !found[name] {
			missing = append(missing, name)
		}
	}
	for name := range found {
		if !declared[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) != 0 {
		t.Errorf("corpus rows without a source: %s", strings.Join(missing, ", "))
	}
	if len(extra) != 0 {
		t.Errorf("corpus sources without a row: %s", strings.Join(extra, ", "))
	}
}

// TestRuntimeV2OrdinaryStorageCorpusOnVM runs every corpus row on the VM
// backend. It must be green on the representation that exists today: that is
// what makes the corpus usable as a before-and-after, rather than a wish list
// written against the representation that has not landed yet.
func TestRuntimeV2OrdinaryStorageCorpusOnVM(t *testing.T) {
	requireVMBackend(t)
	root := repoRoot(t)
	surge := buildSurgeBinary(t, root)
	env := envForParity(root)

	for _, fixture := range ordinaryStorageFixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			stdout, stderr, code := runSurgeWithEnv(t, root, surge, env, "run", "--backend=vm", fixture.relPath())
			if code != 0 {
				t.Fatalf("exit code = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
			}
			if got := strings.TrimSpace(stdout); got != fixture.want {
				t.Fatalf("stdout = %q, want %q\nstderr:\n%s", got, fixture.want, stderr)
			}
			if strings.TrimSpace(stderr) != "" {
				t.Fatalf("unexpected stderr:\n%s", stderr)
			}
		})
	}
}
