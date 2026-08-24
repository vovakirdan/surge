//go:build runtime_v2_pending

package vm_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// A channel's value has exactly one owner at every instant, and the buffer
// hands values out in the order it took them.
//
// The stand this drives carries an element whose move EMPTIES its source and
// whose drop is counted, and it runs more senders than the channel has park
// slots. Three defects are visible through that arrangement and through no
// cheaper one:
//
//   - a send that re-reads its caller's storage after a park delivers a value
//     that has already been moved from, which arrives as a zero marker;
//   - a sender parked holding its own value and then ACKED believes a send
//     completed that delivered nothing, so that value never arrives;
//   - a value handed to a parked receiver while the buffer holds older ones
//     arrives ahead of them, which the sibling FIFO row sees as an order
//     violation and this one sees as nothing at all -- which is why both rows
//     exist.
func buildChannelOwnedElementStand(t *testing.T) string {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not installed; skipping the owned-element channel proof")
	}
	root := repoRoot(t)
	bin := filepath.Join(t.TempDir(), "channel_owned_element")
	sources, err := filepath.Glob(filepath.Join(root, "runtime", "native", "*.c"))
	if err != nil {
		t.Fatalf("glob runtime sources: %v", err)
	}
	sort.Strings(sources)
	args := []string{
		"-std=c11", "-Wall", "-Wextra", "-Werror", "-pthread",
		"-I" + filepath.Join(root, "runtime", "native"),
		"-o", bin,
		filepath.Join(root, "internal", "vm", "testdata", "channel_owned_element.c"),
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
		t.Fatalf("build owned-element stand failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
			code, stdout, stderr)
	}
	return bin
}

func TestRuntimeV2ChannelOwnedElementArrivesExactlyOnce(t *testing.T) {
	bin := buildChannelOwnedElementStand(t)
	for _, tc := range []struct {
		name string
		env  []string
	}{
		{name: "shards-1", env: []string{"SURGE_SHARDS=1", "SURGE_THREADS=2"}},
		{name: "shards-3", env: []string{"SURGE_SHARDS=3", "SURGE_THREADS=3"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(bin, "owned-element")
			env := os.Environ()
			for _, value := range tc.env {
				parts := strings.SplitN(value, "=", 2)
				env = overrideEnvVar(env, parts[0], parts[1])
			}
			cmd.Env = env
			stdout, stderr, code := runCommand(t, cmd, "")
			if code != 0 {
				t.Fatalf("owned-element stand failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
					code, stdout, stderr)
			}
			if !strings.Contains(stdout, "received=200 bad=0 drops=0 missing=0 duplicated=0 closed=1") {
				t.Fatalf("owned-element stand reported an unexpected census\nstdout:\n%s\nstderr:\n%s",
					stdout, stderr)
			}
		})
	}
}
