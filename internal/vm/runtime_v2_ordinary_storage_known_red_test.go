//go:build !golden
// +build !golden

package vm_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// The known-red corner of the ordinary-storage corpus.
//
// Two zero-sized shapes produce the right answer on both backends and then
// abort the native process while the value is released. Leaving them out of the
// corpus would lose them; leaving them in would make the corpus red before any
// of the storage work starts, and a harness that is red for a reason nobody is
// working on stops being read.
//
// So they are here, and the rows assert the DIVERGENCE rather than the value:
// the VM must be clean, the native binary must abort. That makes the defect
// executable evidence with a name instead of a paragraph, and it makes the fix
// self-announcing — when the native run stops aborting, the row fails and says
// to move the source into the green corpus.

// ordinaryStorageKnownRedFixtures lists the sources that are excluded from the
// green corpus, each with the defect it pins.
func ordinaryStorageKnownRedFixtures() []ordinaryStorageFixture {
	return []ordinaryStorageFixture{
		{
			name: "storage_zero_sized_member_order",
			dir:  "known_red",
			want: "zero sized member order ok",
		},
		{
			name: "storage_zero_sized_array",
			dir:  "known_red",
			want: "zero sized array ok",
		},
	}
}

// TestRuntimeV2OrdinaryStorageKnownRedOnVM pins that the known-red sources are
// correct programs: the VM runs them to completion with the expected output. If
// this fails, the source is wrong and the native abort proves nothing.
func TestRuntimeV2OrdinaryStorageKnownRedOnVM(t *testing.T) {
	requireVMBackend(t)
	root := repoRoot(t)
	surge := buildSurgeBinary(t, root)
	env := envForParity(root)

	for _, fixture := range ordinaryStorageKnownRedFixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			stdout, stderr, code := runSurgeWithEnv(t, root, surge, env, "run", "--backend=vm", fixture.relPath())
			if code != 0 {
				t.Fatalf("exit code = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
			}
			if got := strings.TrimSpace(stdout); got != fixture.want {
				t.Fatalf("stdout = %q, want %q", got, fixture.want)
			}
		})
	}
}

// TestRuntimeV2OrdinaryStorageKnownRedAbortsOnLLVM pins the current native
// behaviour: the program prints its success line and then dies releasing the
// value.
//
// The assertion is deliberately exact about WHERE it dies. A native run that
// never printed the success line would be a different defect wearing the same
// exit code, and this row would otherwise call it expected.
func TestRuntimeV2OrdinaryStorageKnownRedAbortsOnLLVM(t *testing.T) {
	ensureLLVMToolchain(t)
	root := repoRoot(t)
	surge := buildSurgeBinary(t, root)
	env := envForParity(root)

	for _, fixture := range ordinaryStorageKnownRedFixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			buildOut, buildErr, buildCode := runSurgeWithEnv(t, root, surge, env, "build", fixture.relPath())
			if buildCode != 0 {
				t.Fatalf("build failed (code=%d)\nstdout:\n%s\nstderr:\n%s", buildCode, buildOut, buildErr)
			}

			binPath := filepath.Join(root, "target", "debug", fixture.name)
			stdout, stderr, code := runBinary(t, binPath)

			if got := strings.TrimSpace(stdout); got != fixture.want {
				t.Fatalf("native stdout = %q, want %q: the program did not reach its"+
					" success line, so the abort below is a different defect\nstderr:\n%s",
					got, fixture.want, stderr)
			}
			if code == 0 {
				t.Fatalf("native run exited 0: the zero-sized release defect this row"+
					" pins appears to be fixed, so move testdata/%s into layout/ and"+
					" add it to ordinaryStorageFixtures", fixture.relPath())
			}
			if !strings.Contains(stderr, "free()") {
				t.Fatalf("native run exited %d but not through the allocator: expected the"+
					" double free this row pins\nstderr:\n%s", code, stderr)
			}
		})
	}
}
