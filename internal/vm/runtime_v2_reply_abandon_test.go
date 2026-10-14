//go:build runtime_v2_pending

package vm_test

import (
	"strings"
	"testing"
	"time"
)

func TestRuntimeV2ReplyCancellationLifetime(t *testing.T) {
	bin := buildRemoteTaskBehaviorHarness(t)
	t.Run("listed-final-release-is-refused", func(t *testing.T) {
		_, stderr, code := runRemotePublicationHarness(t, bin, "pending-listed-final-refused", remotePublicationEnv("SURGE_SHARDS=2"))
		if code == 0 || !strings.Contains(stderr, "remote request: final reference released before registry retirement") {
			t.Fatalf("registry retirement guard: exit=%d stderr=%q", code, stderr)
		}
	})
	for _, mode := range []string{"reply-before-caller-abandon", "reply-after-caller-abandon", "reply-shutdown-after-caller-abandon", "channel-reply-abandon", "pending-reclaim-detached"} {
		t.Run(mode, func(t *testing.T) {
			env := remotePublicationEnv("SURGE_SHARDS=2")
			if mode == "channel-reply-abandon" {
				env = append(env, "SURGE_TEST_HOLD_TRANSPORT_DRAIN=1")
			}
			stdout, stderr, code := runRemotePublicationHarness(t, bin, mode, env)
			if code != 0 {
				t.Fatalf("reply lifecycle: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			stdout, stderr, code = runBinaryUnderValgrind(t, bin, env, 60*time.Second, mode)
			if code != 0 || stdout != "" || hasValgrindMemcheckError(stderr) || !strings.Contains(stderr, "All heap blocks were freed") {
				t.Fatalf("reply lifetime: exit=%d stdout=%q\n%s", code, stdout, stderr)
			}
		})
	}
}

func TestRuntimeV2ReplyCancellationNegativeControl(t *testing.T) {
	bin := buildRemoteTaskBehaviorHarnessWithFlags(t, "reply_unlinks_caller", []string{"-DRV2_REPLY_UNLINK_NEGATIVE_CONTROL"})
	for _, row := range []struct{ mode, want string }{
		{"reply-before-caller-abandon", "landed reply hid the caller-owned pending"},
		{"reply-shutdown-after-caller-abandon", "caller teardown did not unlink and release"},
	} {
		_, stderr, code := runRemotePublicationHarness(t, bin, row.mode, remotePublicationEnv("SURGE_SHARDS=2"))
		if code == 0 || !strings.Contains(stderr, row.want) {
			t.Fatalf("negative control %s missed the lost cleanup route: exit=%d stderr=%q", row.mode, code, stderr)
		}
	}
	leaseBin := buildRemoteTaskBehaviorHarnessWithFlags(t, "reply_keeps_channel_lease", []string{"-DRV2_REPLY_CHANNEL_LEASE_NEGATIVE_CONTROL"})
	_, stderr, code := runRemotePublicationHarness(t, leaseBin, "channel-reply-abandon", remotePublicationEnv("SURGE_SHARDS=2", "SURGE_TEST_HOLD_TRANSPORT_DRAIN=1"))
	if code == 0 || !strings.Contains(stderr, "minted reply lease survived before shutdown") {
		t.Fatalf("channel lease negative control missed the leak: exit=%d stderr=%q", code, stderr)
	}
	lockedBin := buildRemoteTaskBehaviorHarnessWithFlags(t, "reply_drops_under_lock", []string{"-DRV2_REPLY_RECLAIM_LOCK_NEGATIVE_CONTROL"})
	_, stderr, code = runRemotePublicationHarness(t, lockedBin, "pending-reclaim-detached", remotePublicationEnv("SURGE_SHARDS=2"))
	if code == 0 || !strings.Contains(stderr, "was dispatched while a runtime lock is held") {
		t.Fatalf("locked reclaim negative control missed the invalid drop: exit=%d stderr=%q", code, stderr)
	}
}
