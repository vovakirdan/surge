//go:build runtime_v2_pending

package vm_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runChannelCloseCancelStand(t *testing.T, sanitize bool) {
	t.Helper()
	if testing.Short() || strings.TrimSpace(os.Getenv("SURGE_SKIP_TIMEOUT_TESTS")) != "0" {
		t.Fatal("close/cancel proof requires non-short mode and explicit SURGE_SKIP_TIMEOUT_TESTS=0")
	}
	if _, err := exec.LookPath("clang"); err != nil {
		t.Fatalf("close/cancel proof requires clang: %v", err)
	}
	flags := []string{"-g", "-O0", "-fno-omit-frame-pointer"}
	if sanitize {
		flags = append(flags, "-fsanitize=address,undefined", "-fno-sanitize-recover=all")
	}
	bin := buildFrameReleaseStand(t, frameReleaseStandBuild{
		bin:      filepath.Join(t.TempDir(), "channel-close-cancel"),
		fixtures: []string{"channel_close_cancel.c"}, flags: flags,
	})
	for _, workers := range []string{"2", "8"} {
		for _, route := range []string{"same", "foreign"} {
			t.Run(route+"-shard-workers-"+workers, func(t *testing.T) {
				// Multishard configuration requires THREADS == SHARDS. Each
				// shard has one background worker; main never polls tasks.
				env := lifecycleEnv("SURGE_SHARDS="+workers, "SURGE_THREADS="+workers,
					"SURGE_BLOCKING_THREADS=1")
				if sanitize {
					// This lane proves access safety, not process-wide zero leaks.
					env = overrideEnvVar(env, "ASAN_OPTIONS", "detect_leaks=0:halt_on_error=1:abort_on_error=1")
					env = overrideEnvVar(env, "UBSAN_OPTIONS", "halt_on_error=1:print_stacktrace=1")
				}
				cmd := exec.Command(bin, route, workers, "32")
				cmd.Dir, cmd.Env = repoRoot(t), env
				stdout, stderr, code := runCommand(t, cmd, "")
				channelOwner := 0
				if route == "foreign" {
					channelOwner = 1
				}
				want := fmt.Sprintf("CLOSE_CANCEL: route=%s shards=%s workers=%s sender_owner=0 channel_owner=%d rounds=32 closed_delivery=32 polls=64 unused_drops=32 staged_drops=32 final_payload_drops=96 payload_frees=32", route, workers, workers, channelOwner)
				if code != 0 || strings.TrimSpace(stdout) != want || strings.TrimSpace(stderr) != "" {
					t.Fatalf("close/cancel failed (exit=%d)\nwant: %s\nstdout:\n%s\nstderr:\n%s", code, want, stdout, stderr)
				}
			})
		}
	}
}

// A real closed-channel delivery must preserve the parked sender's capability
// until its cancelled poll can retire the staged value. Driver-owned state
// deliberately keeps the Channel alive after await so teardown cannot hide it.
func TestRuntimeV2ChannelCloseThenCancelRetainsStagedSend(t *testing.T) {
	runChannelCloseCancelStand(t, false)
}

func TestRuntimeV2ChannelCloseThenCancelUnderAddressAndUndefinedSanitizers(t *testing.T) {
	runChannelCloseCancelStand(t, true)
}
