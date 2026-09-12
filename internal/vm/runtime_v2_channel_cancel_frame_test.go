//go:build runtime_v2_pending

package vm_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const channelCancelFrameMarker = "CHANNEL_CANCEL_FRAME: polls=2 staging_drops=1 frame_drops=1 payload_drops=2 frees=1/1/1"

// This is a native protocol stand with a real allocated PACKED frame and
// descriptor lookup. It does not claim to execute compiler-generated polls.
func buildChannelCancelFrame(t *testing.T, sanitize bool) string {
	t.Helper()
	if testing.Short() || strings.TrimSpace(os.Getenv("SURGE_SKIP_TIMEOUT_TESTS")) != "0" {
		t.Fatal("channel frame proof requires non-short mode and explicit SURGE_SKIP_TIMEOUT_TESTS=0")
	}
	if _, err := exec.LookPath("clang"); err != nil {
		t.Fatalf("channel frame proof requires clang: %v", err)
	}
	proofs := filepath.Join(repoRoot(t), "target", "debug", ".proofs")
	if err := os.MkdirAll(proofs, 0o700); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(proofs, "channel-cancel-frame-")
	if err != nil {
		t.Fatal(err)
	}
	flags := []string{"-g", "-O0", "-fno-omit-frame-pointer", "-Wl,--wrap=rt_free"}
	if sanitize {
		flags = append(flags, "-fsanitize=address,undefined", "-fno-sanitize-recover=all")
	}
	return buildFrameReleaseStand(t, frameReleaseStandBuild{
		bin:      filepath.Join(dir, "channel-cancel-frame"),
		fixtures: []string{"async_allocation_baseline.c", "channel_cancel_frame.c"},
		flags:    flags,
	})
}

func runChannelCancelFrame(t *testing.T, bin string, sanitize bool) {
	t.Helper()
	for _, workers := range []string{"1", "8"} {
		t.Run("workers-"+workers, func(t *testing.T) {
			env := asyncAllocationEnvironment(t, workers)
			if sanitize {
				// This lane checks invalid accesses and UB. The separate exact
				// Valgrind differential checks leaks against process bootstrap.
				env = overrideEnvVar(env, "ASAN_OPTIONS", "detect_leaks=0:halt_on_error=1:abort_on_error=1")
				env = overrideEnvVar(env, "UBSAN_OPTIONS", "halt_on_error=1:print_stacktrace=1")
			}
			cmd := exec.Command(bin, "subject", "32")
			cmd.Dir, cmd.Env = repoRoot(t), env
			stdout, stderr, code := runCommand(t, cmd, "")
			want := channelCancelFrameMarker + " rounds=32\n" + asyncAllocationCensusMarker
			if code != 0 || strings.TrimSpace(stdout) != want || strings.TrimSpace(stderr) != "" {
				t.Fatalf("channel frame stand failed (exit=%d)\nwant: %s\nstdout:\n%s\nstderr:\n%s", code, want, stdout, stderr)
			}
		})
	}
}

func TestRuntimeV2ChannelCancelKeepsLastFrameHandle(t *testing.T) {
	runChannelCancelFrame(t, buildChannelCancelFrame(t, false), false)
}

func TestRuntimeV2ChannelCancelFrameUnderAddressAndUndefinedSanitizers(t *testing.T) {
	runChannelCancelFrame(t, buildChannelCancelFrame(t, true), true)
}

func TestRuntimeV2ChannelCancelFrameValgrindBaseline(t *testing.T) {
	if _, err := exec.LookPath("valgrind"); err != nil {
		t.Fatalf("channel frame allocation proof requires valgrind: %v", err)
	}
	bin := buildChannelCancelFrame(t, false)
	for _, workers := range []string{"1", "8"} {
		t.Run("workers-"+workers, func(t *testing.T) {
			runAsyncAllocationBaseline(t, bin, channelCancelFrameMarker, asyncAllocationEnvironment(t, workers))
		})
	}
}
