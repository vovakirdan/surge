//go:build runtime_v2_pending

package vm_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// The overwritten-value obligation, measured per place shape on both lanes.
//
// Assigning a heap-owning value over a place must free what it displaces. This
// runs testdata/place_overwrite_leak.sg, whose exit code is a bitmask naming
// every shape that did not.
//
// IT IS RED ON NATIVE TODAY, ON PURPOSE. That is the defect the gate exists to
// hold, and it is why the file carries a build tag: `make test` is a plain
// `go test ./...`, so a tagged test stays out of it and out of `make check`,
// and a deliberately-red gate can be committed without a red tree. It runs
// from its own Makefile target instead.
//
// The VM lane is green and is not a formality: it is the proof that the
// obligation is RECORDED and only the native discharge is missing. Both lanes
// run the same sema; if the record were absent, the VM would leak too.

type placeShape struct {
	bit  int
	name string
}

// Bit order matches the fixture. The last entry is the fixture's self-check
// rather than a shape: it fires when the twin loop freed nothing at all, so an
// instrument that stopped measuring fails instead of reading as a clean sheet.
var placeShapes = []placeShape{
	{1, "struct field"},
	{2, "nested struct field"},
	{4, "tuple element"},
	{8, "store through &mut"},
	{16, "array element"},
	{32, "nested array element"},
	{64, "INSTRUMENT DEAD: the twin loop freed nothing, so no shape was measured"},
}

func TestRuntimeV2PlaceOverwriteReleasesDisplacedValue(t *testing.T) {
	root := repoRoot(t)
	surge := buildSurgeBinary(t, root)
	sgRel := filepath.ToSlash(filepath.Join("internal", "vm", "testdata", "place_overwrite_leak.sg"))

	for _, backend := range []string{"vm", "llvm"} {
		t.Run(backend, func(t *testing.T) {
			stdout, stderr, code := runSurgeWithEnv(
				t, root, surge, envWithStdlib(root), "run", "--backend="+backend, sgRel,
			)
			if code == 0 {
				return
			}
			// A code outside the mask is not this gate's answer — a compile
			// refusal or a panic exits with something the bitmask cannot mean,
			// and reporting it as "these shapes leaked" would be a lie.
			if code < 0 || code > 127 {
				t.Fatalf("%s: the probe did not run to completion (exit %d)\nstdout:\n%s\nstderr:\n%s",
					backend, code, stdout, stderr)
			}
			var leaked []string
			for _, shape := range placeShapes {
				if code&shape.bit != 0 {
					leaked = append(leaked, shape.name)
				}
			}
			t.Fatalf(
				"%s: assigning over a place did not free what it displaced (exit %d)\n  %s\n"+
					"Each shape is compared against a whole-binding twin in the same run, so this is\n"+
					"a shortfall in frees rather than an absolute budget.\nstdout:\n%s\nstderr:\n%s",
				backend, code, strings.Join(leaked, "\n  "), stdout, stderr,
			)
		})
	}
}
