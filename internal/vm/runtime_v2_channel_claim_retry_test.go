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

// The RV2-DEBT-277 bounded claim-retry stand (internal/vm/testdata/
// channel_claim_retry*.c): the channel's single-transfer claim is refused,
// never queued, and a refused operation goes back to selection at most eight
// times before it parks on the channel's own retry key and is woken by the
// release of the claim that refused it.

func buildChannelClaimRetryStand(t *testing.T, name string, extraFlags ...string) string {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not installed; skipping channel claim-retry proof")
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
		filepath.Join(root, "internal", "vm", "testdata", "channel_claim_retry.c"),
		filepath.Join(root, "internal", "vm", "testdata", "channel_claim_retry_modes.c"),
		filepath.Join(root, "internal", "vm", "testdata", "channel_claim_retry_state_modes.c"),
		filepath.Join(root, "internal", "vm", "testdata", "channel_claim_retry_verify_modes.c"))
	for _, source := range sources {
		if filepath.Base(source) != "rt_entry.c" {
			args = append(args, source)
		}
	}
	cmd := exec.Command(clang, args...)
	cmd.Dir = root
	stdout, stderr, code := runCommand(t, cmd, "")
	if code != 0 {
		t.Fatalf("build channel claim-retry stand failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
			code, stdout, stderr)
	}
	return bin
}

func runChannelClaimRetryStand(t *testing.T, bin, mode string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(bin)
	env := overrideEnvVar(os.Environ(), "SURGE_SHARDS", "1")
	env = overrideEnvVar(env, "SURGE_THREADS", "1")
	env = overrideEnvVar(env, "SURGE_TRACE_EXEC", "1")
	if mode != "" {
		env = overrideEnvVar(env, "SURGE_CHANNEL_RETRY_MODE", mode)
	}
	cmd.Env = env
	return runCommand(t, cmd, "")
}

func TestRuntimeV2ChannelClaimRetryBudgetAndWake(t *testing.T) {
	bin := buildChannelClaimRetryStand(t, "channel_claim_retry")
	rows := []struct {
		name     string
		mode     string
		want     string
		releases string
		pushes   string
		pops     string
	}{
		{
			name:     "direct-send",
			want:     "OK_DIRECT: refusals=8 republications=7 exhaustions=1 max_retries=8 woke=1 completed=1",
			releases: "channel_claim_releases=2",
			pushes:   "channel_claim_refusals_ring_push=8",
			pops:     "channel_claim_refusals_ring_pop=0",
		},
		{
			name:     "direct-recv",
			mode:     "recv",
			want:     "OK_RECV: refusals=8 republications=7 exhaustions=1 max_retries=8 woke=1 completed=1",
			releases: "channel_claim_releases=4",
			pushes:   "channel_claim_refusals_ring_push=0",
			pops:     "channel_claim_refusals_ring_pop=8",
		},
		{
			name:     "select-send",
			mode:     "select",
			want:     "OK_SELECT: refusals=8 republications=7 exhaustions=1 max_retries=8 woke=1 completed=1",
			releases: "channel_claim_releases=2",
			pushes:   "channel_claim_refusals_ring_push=8",
			pops:     "channel_claim_refusals_ring_pop=0",
		},
		{
			name:     "close-terminates-retry",
			mode:     "close",
			want:     "OK_CLOSE: retry_park_terminated=1 resume=closed",
			releases: "channel_claim_releases=1",
			pushes:   "channel_claim_refusals_ring_push=8",
			pops:     "channel_claim_refusals_ring_pop=0",
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			stdout, stderr, code := runChannelClaimRetryStand(t, bin, row.mode)
			if code != 0 {
				t.Fatalf("channel claim-retry stand failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
					code, stdout, stderr)
			}
			if !strings.Contains(stdout, row.want) {
				t.Fatalf("channel claim-retry census missing %q\nstdout:\n%s\nstderr:\n%s",
					row.want, stdout, stderr)
			}
			traceFields := []string{
				row.pushes,
				row.pops,
				"channel_claim_refusals_park_take=0",
				"channel_retry_republications=7",
				"channel_retry_budget_exhaustions=1",
				"channel_max_retries_per_operation=8",
				row.releases,
			}
			for _, field := range traceFields {
				if !strings.Contains(stderr, field) {
					t.Fatalf("channel claim-retry trace missing %q\nstderr:\n%s", field, stderr)
				}
			}
		})
	}
}

func TestRuntimeV2ChannelClaimRetryIdentityAndReset(t *testing.T) {
	bin := buildChannelClaimRetryStand(t, "channel_claim_retry_identity_reset")
	rows := []struct {
		name      string
		mode      string
		want      string
		wantTrace string
	}{
		{
			name: "select-identity-survives-handle-address-rotation",
			mode: "select-identity",
			want: "OK_SELECT_IDENTITY: addresses=2 refusals=8 woke=1 completed=1",
		},
		{
			name: "select-parks-on-every-refusing-arm-not-only-the-eighth",
			mode: "select-prefix",
			want: "OK_SELECT_PREFIX: arms=2 refusals=8 remembered=2 released=first woke=1 completed=0",
		},
		{
			name: "select-with-default-never-parks-on-a-refused-claim",
			mode: "select-default",
			want: "OK_SELECT_DEFAULT: polls=9 default_wins=9 parked=0 budget=reset",
		},
		{
			name: "same-owner-recovery-resets-before-next-operation",
			mode: "recovery-reset",
			want: "OK_RECOVERY_RESET: path=same recovery=completed next_operation_refusals=1 parked=0",
		},
		{
			name: "foreign-owner-recovery-resets-before-next-operation",
			mode: "recovery-reset-foreign",
			want: "OK_RECOVERY_RESET: path=foreign recovery=completed next_operation_refusals=1 parked=0",
		},
		{
			name:      "value-bearing-finish-release-wakes-park-take-refusal",
			mode:      "park-finish-release",
			want:      "OK_PARK_FINISH_RELEASE: park_take_refusals=8 wake_before_park=1",
			wantTrace: "channel_claim_refusals_park_take=8",
		},
		{
			name: "pre-park-select-does-not-consume-handoff",
			mode: "handoff-running",
			want: "OK_HANDOFF: select=running registrations=2->0 pins=2->0 direct_woke=1",
		},
		{
			name: "parked-select-does-not-consume-handoff",
			mode: "handoff-waiting",
			want: "OK_HANDOFF: select=waiting registrations=2->0 pins=2->0 direct_woke=1",
		},
		{
			name: "ready-select-does-not-consume-or-duplicate-handoff",
			mode: "handoff-ready",
			want: "OK_HANDOFF: select=ready registrations=2->0 pins=2->0 direct_woke=1",
		},
		{
			name: "direct-then-select-drains-both-classes",
			mode: "handoff-direct-first",
			want: "OK_HANDOFF_ORDER: rows=D1,S registrations=2->0 pins=2->0",
		},
		{
			name: "direct-select-direct-preserves-second-direct",
			mode: "handoff-direct-select-direct",
			want: "OK_HANDOFF_ORDER: rows=D1,S,D2 registrations=3->1 pins=3->1",
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			stdout, stderr, code := runChannelClaimRetryStand(t, bin, row.mode)
			if code != 0 {
				t.Fatalf("channel retry state stand failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
					code, stdout, stderr)
			}
			if !strings.Contains(stdout, row.want) {
				t.Fatalf("channel retry state proof missing %q\nstdout:\n%s\nstderr:\n%s",
					row.want, stdout, stderr)
			}
			if row.wantTrace != "" && !strings.Contains(stderr, row.wantTrace) {
				t.Fatalf("channel retry state trace missing %q\nstderr:\n%s",
					row.wantTrace, stderr)
			}
		})
	}
}

func TestRuntimeV2ChannelClaimRetryNegativeControls(t *testing.T) {
	rows := []struct {
		name string
		flag string
		mode string
		want string
	}{
		{
			name: "eighth-refusal-must-park",
			flag: "-DRV2_DEBT_277_RETRY_NEGATIVE_CONTROL",
			want: "FAIL: operation did not park on the eighth refusal",
		},
		{
			name: "claim-release-must-wake",
			flag: "-DRV2_DEBT_277_WAKE_NEGATIVE_CONTROL",
			want: "FAIL: claim release did not wake the retry park",
		},
		{
			name: "close-must-drain-retry-key",
			flag: "-DRV2_DEBT_277_CLOSE_NEGATIVE_CONTROL",
			mode: "close",
			want: "FAIL: close did not terminate the retry park",
		},
		{
			name: "select-identity-must-ignore-handle-address",
			flag: "-DRV2_DEBT_277_SELECT_IDENTITY_NEGATIVE_CONTROL",
			mode: "select-identity",
			want: "FAIL: select address rotation reset the retry budget",
		},
		{
			name: "select-must-park-on-every-refusing-arm",
			flag: "-DRV2_DEBT_277_PREFIX_NEGATIVE_CONTROL",
			mode: "select-prefix",
			want: "FAIL: release on an earlier refusing arm did not wake the select",
		},
		{
			name: "select-with-default-must-not-republish",
			flag: "-DRV2_DEBT_277_SELECT_DEFAULT_NEGATIVE_CONTROL",
			mode: "select-default",
			want: "FAIL: select with a default republished on a refused claim",
		},
		{
			name: "same-owner-recovery-must-reset",
			flag: "-DRV2_DEBT_277_RECOVERY_RESET_NEGATIVE_CONTROL",
			mode: "recovery-reset",
			want: "FAIL: new send inherited completed retry budget",
		},
		{
			name: "foreign-owner-recovery-must-reset",
			flag: "-DRV2_DEBT_277_RECOVERY_RESET_NEGATIVE_CONTROL",
			mode: "recovery-reset-foreign",
			want: "FAIL: new send inherited completed retry budget",
		},
		{
			name: "value-bearing-finish-release-must-wake",
			flag: "-DRV2_DEBT_277_PARK_RELEASE_WAKE_NEGATIVE_CONTROL",
			mode: "park-finish-release",
			want: "FAIL: finish-release did not preserve retry wake",
		},
		{
			name: "select-sibling-must-not-consume-handoff",
			flag: "-DRV2_DEBT_277_RETRY_HANDOFF_NEGATIVE_CONTROL",
			mode: "handoff-ready",
			want: "FAIL: claim release stopped at select sibling",
		},
		{
			name: "direct-must-not-stop-select-drain",
			flag: "-DRV2_DEBT_277_RETRY_HANDOFF_NEGATIVE_CONTROL",
			mode: "handoff-direct-first",
			want: "FAIL: claim release left select retry subscription behind",
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bin := buildChannelClaimRetryStand(t, "channel_claim_retry_negative", row.flag)
			stdout, stderr, code := runChannelClaimRetryStand(t, bin, row.mode)
			if code == 0 {
				t.Fatalf("negative control unexpectedly passed\nstdout:\n%s\nstderr:\n%s",
					stdout, stderr)
			}
			if !strings.Contains(stdout, row.want) {
				t.Fatalf("negative control failed for the wrong reason; want %q\nstdout:\n%s\nstderr:\n%s",
					row.want, stdout, stderr)
			}
		})
	}
}
