//go:build runtime_v2_pending

package vm_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// The register-then-verify half of the select side of RV2-DEBT-277: the
// eighth refusal is observed under the channel owner lane, but the claim
// helper releases that lane before select can subscribe, so a release or a
// close can cross the still-empty retry key. The sync point holds the select
// in exactly that gap.

const channelClaimRetryVerifyPoint = "SP_CHANNEL_SELECT_REFUSED_BEFORE_RETRY_REGISTER:block"

func runChannelClaimRetryVerifyStand(t *testing.T, bin, mode string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(bin)
	env := overrideEnvVar(os.Environ(), "SURGE_SHARDS", "1")
	env = overrideEnvVar(env, "SURGE_THREADS", "1")
	env = overrideEnvVar(env, "SURGE_CHANNEL_RETRY_MODE", mode)
	env = overrideEnvVar(env, "SURGE_SYNC_POINT", channelClaimRetryVerifyPoint)
	cmd.Env = env
	return runCommand(t, cmd, "")
}

func TestRuntimeV2ChannelClaimRetryRegisterVerify(t *testing.T) {
	bin := buildChannelClaimRetryStand(t, "channel_claim_retry_verify", "-DRT_TEST_SYNC_POINTS")
	rows := []struct {
		name string
		mode string
		want string
	}{
		{
			name: "release-crosses-empty-retry-key",
			mode: "verify-release",
			want: "OK_REGISTER_VERIFY: action=release status=ready " +
				"registrations=2->0 pins=2->0",
		},
		{
			name: "close-crosses-empty-retry-key",
			mode: "verify-close",
			want: "OK_REGISTER_VERIFY: action=close status=ready " +
				"registrations=2->0 pins=2->0",
		},
		{
			name: "unchanged-exact-refusal-really-parks",
			mode: "verify-busy",
			want: "OK_REGISTER_VERIFY: action=busy status=waiting->ready " +
				"registrations=2->1->0 pins=2->1->0",
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			stdout, stderr, code := runChannelClaimRetryVerifyStand(t, bin, row.mode)
			if code != 0 {
				t.Fatalf("register-verify stand failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
					code, stdout, stderr)
			}
			if !strings.Contains(stdout, row.want) {
				t.Fatalf("register-verify proof missing %q\nstdout:\n%s\nstderr:\n%s",
					row.want, stdout, stderr)
			}
		})
	}
}

func TestRuntimeV2ChannelClaimRetryRegisterVerifyNegativeControl(t *testing.T) {
	bin := buildChannelClaimRetryStand(t,
		"channel_claim_retry_verify_negative",
		"-DRT_TEST_SYNC_POINTS",
		"-DRV2_DEBT_277_SELECT_VERIFY_NEGATIVE_CONTROL")
	stdout, stderr, code := runChannelClaimRetryVerifyStand(t, bin, "verify-release")
	if code == 0 {
		t.Fatalf("register-verify negative control unexpectedly passed\nstdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}
	want := "FAIL: select parked after release crossed empty retry key"
	if !strings.Contains(stdout, want) {
		t.Fatalf("register-verify negative control failed for wrong reason; want %q\n"+
			"stdout:\n%s\nstderr:\n%s", want, stdout, stderr)
	}
}
