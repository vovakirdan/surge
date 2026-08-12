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
// It was RED on native and carried a build tag for exactly that reason: a
// deliberately-red gate has to stay out of `make test` — a plain `go test ./...`
// — or no commit can be made while it is red. Both lanes report zero now, so
// the tag is gone and this runs on every `make check` and every commit, which
// is where a regression in any of the six shapes gets caught earliest.
//
// The VM lane was green throughout and was never a formality. Both lanes run
// the same sema, so a green VM against a red native lane says the shortfall is
// in the DISCHARGE and not in the record — which is what sent the fix to sema
// (the obligation was recorded for no place at all, and the VM merely happened
// to release from inside its own store) instead of to the native emitter.

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
