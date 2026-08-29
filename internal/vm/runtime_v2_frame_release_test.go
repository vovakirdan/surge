package vm_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"surge/internal/mir"
)

// The suspension frame's two ends, held to one authority.
//
// A frame is reserved by compiled code and given back from two different
// places, and until it carried a lifecycle word the size came from the emitter
// at one end and from the type's descriptor at the other, while WHICH release
// ran was decided by the call site. Both halves are now the frame's own: the
// descriptor states the width at both ends, and the word states whether the
// members are still live.
//
// These rows are the two things that can silently come apart — a constant that
// drifts between the two languages, and a release that stops reading the word.

// TestFrameLifecycleWordsAgreeAcrossTheBoundary pins the pairing.
//
// The compiler writes the word and the runtime reads it, so the two spellings
// are one contract with two homes. A drift is invisible at run time in the
// worst possible way: the reader would find "not PACKED" for every frame and
// silently stop walking the abandoned ones, which leaks rather than crashes.
func TestFrameLifecycleWordsAgreeAcrossTheBoundary(t *testing.T) {
	header := filepath.Join(repoRoot(t), "runtime", "native", "rt_frame.h")
	raw, err := os.ReadFile(header) // #nosec G304 -- repository-owned path
	if err != nil {
		t.Fatalf("read the frame header: %v", err)
	}
	defineRe := regexp.MustCompile(`(?m)^#define\s+(SURGE_FRAME_STATE_[A-Z]+)\s+0x([0-9A-Fa-f]+)`)
	found := map[string]int64{}
	for _, m := range defineRe.FindAllStringSubmatch(string(raw), -1) {
		var value int64
		if _, scanErr := fmt.Sscanf(m[2], "%x", &value); scanErr != nil {
			t.Fatalf("%s carries %q, which is not the hexadecimal this header writes", m[1], m[2])
		}
		found[m[1]] = value
	}
	// A header that stopped declaring them would leave the loop below with
	// nothing to compare, which is the shape of a check that passes by reading
	// nothing at all.
	if len(found) != 2 {
		t.Fatalf("%s declares %d lifecycle words, want 2: %v", header, len(found), found)
	}
	for name, want := range map[string]int64{
		"SURGE_FRAME_STATE_PACKED": mir.FrameStatePacked,
		"SURGE_FRAME_STATE_SPENT":  mir.FrameStateSpent,
	} {
		if found[name] != want {
			t.Errorf("%s is 0x%X in %s and 0x%X in internal/mir; the writer and the reader "+
				"of one word disagree", name, found[name], header, want)
		}
	}
}

// TestFrameReleaseReadsTheFrameNotTheCallSite drives rt_frame_release directly.
//
// The three rows are the three answers a frame can give, and only one of them
// walks. Doing it from C rather than from a Surge program is what makes the
// SPENT row an assertion at all: a compiled cancellation reaches this release
// with a spent frame, and the failure of walking it is a double free somewhere
// else, at a time and place a leak summary cannot name.
func TestFrameReleaseReadsTheFrameNotTheCallSite(t *testing.T) {
	bin := buildFrameReleaseStand(t)
	stdout, stderr, code := runCommand(t, exec.Command(bin), "")
	if code != 0 || stderr != "" {
		t.Fatalf("the frame-release stand exited %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	const want = "packed=1 spent=0 fresh=0 zeroed=1"
	if got := strings.TrimSpace(stdout); got != want {
		t.Fatalf("frame release census = %q, want %q\n"+
			"  packed=0 means an abandoned frame's members were never destroyed;\n"+
			"  spent=1 means a frame the poll had already emptied was walked, which frees "+
			"what the resumed locals own a second time", got, want)
	}
}

func buildFrameReleaseStand(t *testing.T) string {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not installed; skipping the frame-release proof")
	}
	root := repoRoot(t)
	bin := filepath.Join(t.TempDir(), "frame-release")
	sources, err := filepath.Glob(filepath.Join(root, "runtime", "native", "*.c"))
	if err != nil {
		t.Fatalf("glob runtime sources: %v", err)
	}
	sort.Strings(sources)
	args := []string{
		"-std=c11", "-Wall", "-Wextra", "-Werror", "-pthread",
		"-I" + filepath.Join(root, "runtime", "native"),
		"-o", bin,
		filepath.Join(root, "internal", "vm", "testdata", "frame_release_cases.c"),
	}
	for _, source := range sources {
		if filepath.Base(source) != "rt_entry.c" {
			args = append(args, source)
		}
	}
	cmd := exec.Command(clang, args...)
	cmd.Dir = root
	stdout, stderr, code := runCommand(t, cmd, "")
	if code != 0 {
		t.Fatalf("build frame-release stand failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
			code, stdout, stderr)
	}
	return bin
}
