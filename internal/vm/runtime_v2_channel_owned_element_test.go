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
// The stand these rows drive carries an element that OWNS a heap allocation:
// its move empties the source, its drop frees the block and is counted, and the
// receiver frees what it was handed. Three questions become answerable that an
// inert machine word cannot ask:
//
//   - a send that re-reads its caller's storage after a park delivers a value
//     that has already been moved from, which arrives with a null obligation;
//   - a sender parked holding its own value and then ACKED believes a send
//     completed that delivered nothing, so that value never arrives;
//   - a value destroyed instead of delivered, or destroyed twice, shows up in
//     the drop census -- and under a sanitizer, as a double free.
//
// It also runs more senders than the channel has park slots, which is what
// makes the "parked holding its own value" path ordinary rather than exotic.
func buildChannelOwnedElementStand(t *testing.T, name string, extraFlags []string) string {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not installed; skipping the owned-element channel proof")
	}
	root := repoRoot(t)
	bin := filepath.Join(t.TempDir(), name)
	sources, err := filepath.Glob(filepath.Join(root, "runtime", "native", "*.c"))
	if err != nil {
		t.Fatalf("glob runtime sources: %v", err)
	}
	sort.Strings(sources)
	args := []string{
		"-std=c11", "-Wall", "-Wextra", "-Werror", "-pthread",
		"-I" + filepath.Join(root, "runtime", "native"),
	}
	args = append(args, extraFlags...)
	args = append(args, "-o", bin,
		filepath.Join(root, "internal", "vm", "testdata", "channel_owned_element.c"))
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

type ownedElementRow struct {
	name string
	mode string
	env  []string
	want string
}

func ownedElementRows() []ownedElementRow {
	const fullCensus = "received=200 bad=0 drops=0 missing=0 duplicated=0 closed=1"
	return []ownedElementRow{
		{
			name: "buffered-shards-1",
			mode: "owned-element",
			env:  []string{"SURGE_SHARDS=1", "SURGE_THREADS=2"},
			want: fullCensus,
		},
		{
			name: "buffered-shards-3",
			mode: "owned-element",
			env:  []string{"SURGE_SHARDS=3", "SURGE_THREADS=3"},
			want: fullCensus,
		},
		{
			name: "unbuffered-shards-3",
			mode: "owned-unbuffered",
			env:  []string{"SURGE_SHARDS=3", "SURGE_THREADS=3"},
			want: fullCensus,
		},
		{
			// The channel outlives every park on it, so a cancelled sender's
			// staged value is destroyed by the channel's own drain -- once,
			// and only there. This row is the one instrument that can see it:
			// the drop is counted from inside the program, which a leak
			// summary cannot do (RV2-DEBT-245).
			name: "cancelled-sender-keeps-one-drop",
			mode: "owned-cancelled-sender",
			env:  []string{"SURGE_SHARDS=3", "SURGE_THREADS=3"},
			want: "cancelled sender: drops=1 received=0",
		},
	}
}

func runOwnedElementRows(t *testing.T, bin string, extraEnv []string) {
	t.Helper()
	for _, row := range ownedElementRows() {
		t.Run(row.name, func(t *testing.T) {
			cmd := exec.Command(bin, row.mode)
			env := os.Environ()
			for _, value := range append(append([]string{}, row.env...), extraEnv...) {
				parts := strings.SplitN(value, "=", 2)
				env = overrideEnvVar(env, parts[0], parts[1])
			}
			cmd.Env = env
			stdout, stderr, code := runCommand(t, cmd, "")
			if code != 0 {
				t.Fatalf("owned-element stand mode %q failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
					row.mode, code, stdout, stderr)
			}
			if !strings.Contains(stdout, row.want) {
				t.Fatalf("owned-element stand mode %q reported an unexpected census; want %q\nstdout:\n%s\nstderr:\n%s",
					row.mode, row.want, stdout, stderr)
			}
		})
	}
}

func TestRuntimeV2ChannelOwnedElementArrivesExactlyOnce(t *testing.T) {
	runOwnedElementRows(t, buildChannelOwnedElementStand(t, "channel_owned_element", nil), nil)
}

// The same rows under AddressSanitizer and UndefinedBehaviorSanitizer, where a
// value delivered twice frees one block twice and a value read after its move
// reads freed storage. Leak detection is OFF and that is deliberate: this stand
// starts the real executor, whose task and shard allocations outlive main by
// design, so a leak report here would be about the runtime's teardown rather
// than about the channel. What the channel leaks is its own question, and the
// drop census above is what asks it.
func TestRuntimeV2ChannelOwnedElementUnderAddressAndUndefinedSanitizers(t *testing.T) {
	bin := buildChannelOwnedElementStand(t, "channel_owned_element_asan", []string{
		"-fsanitize=address,undefined",
		"-fno-sanitize-recover=all",
		"-fno-omit-frame-pointer",
		"-O1",
		"-g",
	})
	runOwnedElementRows(t, bin, []string{
		"ASAN_OPTIONS=abort_on_error=1:detect_leaks=0",
		"UBSAN_OPTIONS=halt_on_error=1:print_stacktrace=1",
	})
}

// And under ThreadSanitizer, because the ownership handoffs this stand drives
// are exactly the ones that cross a lock boundary: a staged value written by a
// sender and read by a receiver on another shard.
func TestRuntimeV2ChannelOwnedElementUnderThreadSanitizer(t *testing.T) {
	bin := buildChannelOwnedElementStand(t, "channel_owned_element_tsan", []string{
		"-fsanitize=thread",
		"-fno-omit-frame-pointer",
		"-O1",
		"-g",
	})
	runOwnedElementRows(t, bin, []string{"TSAN_OPTIONS=halt_on_error=1"})
}
