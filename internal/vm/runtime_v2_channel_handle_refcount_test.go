//go:build runtime_v2_pending

package vm_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// A channel object lives exactly as long as something still names it
// (RV2-DEBT-155).
//
// `Channel<T>` is a copyable handle at the language surface: copying one
// retains, dropping a copy releases, and the last release destroys the object
// -- which drops every payload the channel still owns, because a channel is
// not a place values go to be forgotten. Nothing in the compiler emits those
// calls yet; the counter and its guard are what these rows drive directly, so
// that the flip that starts emitting them lands on a proven floor.
//
// The stand carries an element that OWNS a heap allocation, for the same
// reason the owned-element stand does: a value delivered twice frees one block
// twice, a value read after its move reads storage that was emptied, and a
// value destroyed instead of delivered is a NUMBER in the census rather than a
// wrong answer somewhere later.
func buildChannelHandleRefcountStand(t *testing.T, name string, extraFlags []string) string {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not installed; skipping the channel handle-count proof")
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
		filepath.Join(root, "internal", "vm", "testdata", "channel_handle_refcount.c"))
	for _, source := range sources {
		if filepath.Base(source) != "rt_entry.c" {
			args = append(args, source)
		}
	}
	cmd := exec.Command(clang, args...)
	cmd.Dir = root
	stdout, stderr, code := runCommand(t, cmd, "")
	if code != 0 {
		t.Fatalf("build channel handle-count stand failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
			code, stdout, stderr)
	}
	return bin
}

type channelHandleRow struct {
	name string
	mode string
	want string
}

// The three questions, and the exact sentence each must answer with.
//
// A row's `want` is a substring of the census the stand prints, so a number
// that moves is a failure with the two figures side by side rather than a
// boolean.
func channelHandleRows() []channelHandleRow {
	return []channelHandleRow{
		{
			// One handle, three values nobody receives: the single release IS
			// the reclaim, and the ring's contents are destroyed there. On a
			// tree whose last release does not reclaim, the drops are zero and
			// the three blocks are lost.
			name: "one-handle-release-reclaims",
			mode: "one-handle",
			want: "one handle: sent=3 received=0 reclaimed_drops=3 bad=0",
		},
		{
			// Two handles: the FIRST release must destroy nothing, which the
			// census states as a number taken at that instant, and the
			// surviving copy must still send and receive through the same
			// buffer. Without the retain the first release frees the object
			// and everything after it is a use-after-free.
			name: "second-handle-keeps-the-object",
			mode: "two-handles",
			want: "two handles: sent=4 received=1 drops_after_first_release=0 reclaimed_drops=3 bad=0",
		},
		{
			// Many handles, retained and released from four threads while the
			// channel is in use. The census cannot be a fixed pair of numbers
			// here -- a send meets a full buffer or does not -- so what it
			// asserts is the accounting: every value that entered the channel
			// left it exactly once, into a receiver or through the reclaim.
			name: "contended-handles-account-for-every-value",
			mode: "contended-handles",
			want: "bad=0 accounted=1",
		},
		{
			// A registration on one of the channel's waiter keys is a hold the
			// program cannot see. The last HANDLE goes while it stands, and the
			// first figure is what the channel still held at that instant:
			// nothing destroyed yet. On a tree where a registration takes no
			// pin the reclaim runs there instead, so the pair reads
			// drops_while_registered=3 reclaimed_drops=3 -- the same three
			// values, destroyed under a live registration whose key then names
			// freed storage.
			name: "registration-refuses-the-reclaim",
			mode: "waiter-pin",
			want: "waiter pin: sent=3 drops_while_registered=0 reclaimed_drops=3 bad=0",
		},
		{
			// The ORDER of the teardown, which no other row can see: the same
			// three values are destroyed either way, so a census of what was
			// destroyed is blind to it. This one is taken from INSIDE, by the
			// element's own drop, and each figure is one clause of the order
			// section 7 of docs/RUNTIME_V2.md prescribes -- sealed before
			// anything is destroyed, everything detached before the first drop
			// runs, and no scheduler lock held while it does. Rebuilt against
			// the pre-lane body -- drain in place, no owner lock, no mark --
			// the same three drops report `sealed_at_drop=0 still_attached=2`
			// and this row goes red; every other row above is byte-identical
			// between the two builds. The third figure confirms rather than
			// discriminates, since the drop dispatch already refuses to run
			// under a scheduler lock.
			name: "teardown-runs-in-the-prescribed-order",
			mode: "teardown-order",
			want: "teardown order: drops=3 sealed_at_drop=3 still_attached=0 locked_at_drop=0",
		},
	}
}

func channelHandleEnv(mode string, extra []string) []string {
	// One shard and one worker: rt_start_workers returns without creating a
	// thread in that configuration, so the only threads in the process are the
	// stand's own, which it joins. That is what keeps the leak census about
	// the channel rather than about the executor's teardown.
	env := overrideEnvVar(os.Environ(), "SURGE_CHANNEL_HANDLE_MODE", mode)
	env = overrideEnvVar(env, "SURGE_SHARDS", "1")
	env = overrideEnvVar(env, "SURGE_THREADS", "1")
	for _, value := range extra {
		parts := strings.SplitN(value, "=", 2)
		env = overrideEnvVar(env, parts[0], parts[1])
	}
	return env
}

func runChannelHandleRows(t *testing.T, bin string, extraEnv []string) {
	t.Helper()
	for _, row := range channelHandleRows() {
		t.Run(row.name, func(t *testing.T) {
			cmd := exec.Command(bin)
			cmd.Env = channelHandleEnv(row.mode, extraEnv)
			stdout, stderr, code := runCommand(t, cmd, "")
			if code != 0 {
				t.Fatalf("channel handle stand mode %q failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
					row.mode, code, stdout, stderr)
			}
			if !strings.Contains(stdout, row.want) {
				t.Fatalf("channel handle stand mode %q reported an unexpected census; want %q\nstdout:\n%s\nstderr:\n%s",
					row.mode, row.want, stdout, stderr)
			}
		})
	}
}

func TestRuntimeV2ChannelHandleRefcountCensus(t *testing.T) {
	runChannelHandleRows(t, buildChannelHandleRefcountStand(t, "channel_handle_refcount", nil), nil)
}

// The same rows under valgrind, at strict zero.
//
// The census above can say a payload was destroyed; only this can say the
// CHANNEL was. A channel whose last release does not reclaim leaks its own
// allocation -- one block, the header plus the inline ring and park pool -- and
// every payload still inside it leaks indirectly from there. Both figures are
// asserted at zero, because the stand creates exactly one channel per row and
// releases every handle it took.
func TestRuntimeV2ChannelHandleRefcountValgrindZero(t *testing.T) {
	bin := buildChannelHandleRefcountStand(t, "channel_handle_refcount_valgrind", []string{"-g"})
	for _, row := range channelHandleRows() {
		t.Run(row.name, func(t *testing.T) {
			env := channelHandleEnv(row.mode, nil)
			stdout, stderr, exitCode := runBinaryUnderValgrind(t, bin, env, 180*time.Second)
			if exitCode != 0 {
				t.Fatalf("channel handle stand mode %q failed under valgrind (code=%d)\nstdout:\n%s\nstderr:\n%s",
					row.mode, exitCode, stdout, stderr)
			}
			if !strings.Contains(stdout, row.want) {
				t.Fatalf("channel handle stand mode %q reported an unexpected census under valgrind; want %q\nstdout:\n%s",
					row.mode, row.want, stdout)
			}
			if hasValgrindMemcheckError(stderr) {
				t.Fatalf("channel handle stand mode %q reported a memcheck error\nstderr:\n%s",
					row.mode, stderr)
			}
			definiteBytes, definiteBlocks := parseValgrindLeakMatch(valgrindDefiniteLeakRE, stderr)
			indirectBytes, indirectBlocks := parseValgrindLeakMatch(valgrindIndirectLeakRE, stderr)
			if definiteBytes != 0 || definiteBlocks != 0 || indirectBytes != 0 || indirectBlocks != 0 {
				t.Fatalf("channel handle stand mode %q leaked: %d bytes in %d blocks definitely, "+
					"%d bytes in %d blocks indirectly; want strict zero on both\nstderr:\n%s",
					row.mode, definiteBytes, definiteBlocks, indirectBytes, indirectBlocks, stderr)
			}
		})
	}
}

// Under AddressSanitizer and UndefinedBehaviorSanitizer, which is where the
// two-handle row stops being an accounting question: a release that frees the
// object while a second copy still names it makes every later send, receive
// and release a heap-use-after-free, at the instruction that does it. Leak
// detection is off for the reason the owned-element stand gives -- the
// executor's own allocations outlive main by design -- and the leak question
// is asked by the valgrind rows above instead.
func TestRuntimeV2ChannelHandleRefcountUnderAddressAndUndefinedSanitizers(t *testing.T) {
	bin := buildChannelHandleRefcountStand(t, "channel_handle_refcount_asan", []string{
		"-fsanitize=address,undefined",
		"-fno-sanitize-recover=all",
		"-fno-omit-frame-pointer",
		"-O1",
		"-g",
	})
	runChannelHandleRows(t, bin, []string{
		"ASAN_OPTIONS=abort_on_error=1:detect_leaks=0",
		"UBSAN_OPTIONS=halt_on_error=1:print_stacktrace=1",
	})
}

// And under ThreadSanitizer, which is the instrument the contended row exists
// for: the count is shared state that four threads retain and release while
// the channel is in use, and a release that is not one atomic
// read-modify-write is a data race at that instruction whether or not this
// particular run loses an update.
func TestRuntimeV2ChannelHandleRefcountUnderThreadSanitizer(t *testing.T) {
	bin := buildChannelHandleRefcountStand(t, "channel_handle_refcount_tsan", []string{
		"-fsanitize=thread",
		"-fno-omit-frame-pointer",
		"-O1",
		"-g",
	})
	runChannelHandleRows(t, bin, []string{"TSAN_OPTIONS=halt_on_error=1"})
}
