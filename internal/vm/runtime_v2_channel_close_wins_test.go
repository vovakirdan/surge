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

// Close-wins by owner-lane order (rt_channel_claim.h): the receiver a
// rendezvous pops stays owner-visible as the channel's claim until the sender
// commits or aborts, or close settles it first. The four orders the Go model
// proves in internal/asyncrt/channel_recv_claim_lifecycle_test.go, on the
// native channel, plus the dead-receiver recovery (RV2-DEBT-276) and the
// unpinned claim bracket (RV2-DEBT-279).

func buildChannelCloseWinsStand(t *testing.T, name string, extraFlags ...string) string {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not installed; skipping close-wins proof")
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
		filepath.Join(root, "internal", "vm", "testdata", "channel_claim_retry_state_modes.c"),
		filepath.Join(root, "internal", "vm", "testdata", "channel_close_wins_modes.c"))
	for _, source := range sources {
		if filepath.Base(source) != "rt_entry.c" {
			args = append(args, source)
		}
	}
	cmd := exec.Command(clang, args...)
	cmd.Dir = root
	stdout, stderr, code := runCommand(t, cmd, "")
	if code != 0 {
		t.Fatalf("build close-wins stand failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
			code, stdout, stderr)
	}
	return bin
}

func runChannelCloseWinsStand(t *testing.T, bin, mode string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(bin)
	env := overrideEnvVar(os.Environ(), "SURGE_SHARDS", "1")
	env = overrideEnvVar(env, "SURGE_THREADS", "1")
	env = overrideEnvVar(env, "SURGE_TRACE_EXEC", "1")
	env = overrideEnvVar(env, "SURGE_CLOSE_WINS_MODE", mode)
	cmd.Env = env
	return runCommand(t, cmd, "")
}

func TestRuntimeV2ChannelCloseWinsOrders(t *testing.T) {
	bin := buildChannelCloseWinsStand(t, "channel_close_wins")
	rows := []struct {
		name  string
		mode  string
		want  string
		trace []string
	}{
		{
			name: "reserve-close-commit-is-close-won",
			mode: "reserve-close-commit",
			want: "OK_CLOSE_COMMIT: order=reserve,close,commit receiver=closed drops=1 wakes=1 claim=retired",
			trace: []string{
				"channel_recv_claims_opened=1",
				"channel_recv_claims_close_won=1",
				"channel_recv_claims_aborted=0",
			},
		},
		{
			name:  "reserve-commit-close-keeps-the-published-value",
			mode:  "reserve-commit-close",
			want:  "OK_COMMIT_CLOSE: order=reserve,commit,close receiver=value drops=0 wakes=1",
			trace: []string{"channel_recv_claims_opened=1", "channel_recv_claims_close_won=0"},
		},
		{
			name: "reserve-close-abort-retires-the-close-won-claim",
			mode: "reserve-close-abort",
			want: "OK_CLOSE_ABORT: order=reserve,close,abort,abort receiver=closed requeued=0 drops=0 wakes=1",
			trace: []string{
				"channel_recv_claims_close_won=1",
				"channel_recv_claims_aborted=1",
			},
		},
		{
			name:  "claim-cannot-be-overtaken",
			mode:  "claim-not-overtaken",
			want:  "OK_NOT_OVERTAKEN: try_send=refused task_send=republished commit=first retry=second",
			trace: []string{"channel_claim_refusals_rendezvous=1", "channel_recv_claims_opened=2"},
		},
		{
			name: "dead-receiver-recovery-keeps-the-value",
			mode: "dead-receiver",
			want: "OK_DEAD_RECEIVER: candidate=dead recovery=kept_slot delivered=7277 destroyed=0",
			trace: []string{
				"channel_rendezvous_recoveries_dead_receiver=1",
				"channel_values_destroyed_in_recovery=0",
			},
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			stdout, stderr, code := runChannelCloseWinsStand(t, bin, row.mode)
			if code != 0 {
				t.Fatalf("close-wins stand failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
					code, stdout, stderr)
			}
			if !strings.Contains(stdout, row.want) {
				t.Fatalf("close-wins proof missing %q\nstdout:\n%s\nstderr:\n%s",
					row.want, stdout, stderr)
			}
			for _, field := range row.trace {
				if !strings.Contains(stderr, field) {
					t.Fatalf("close-wins trace missing %q\nstderr:\n%s", field, stderr)
				}
			}
		})
	}
}

func TestRuntimeV2ChannelCloseWinsUnpinnedClaimIsRefused(t *testing.T) {
	bin := buildChannelCloseWinsStand(t, "channel_close_wins_unpinned")
	stdout, stderr, code := runChannelCloseWinsStand(t, bin, "unpinned-claim")
	if code == 0 {
		t.Fatalf("an unpinned claim bracket was admitted\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	want := "a claimed channel operation ran without a pin"
	if !strings.Contains(stderr+stdout, want) {
		t.Fatalf("unpinned claim failed for the wrong reason; want %q\nstdout:\n%s\nstderr:\n%s",
			want, stdout, stderr)
	}
}

// The send lane's own rendezvous, held by a sync point with the owner lane
// released for the move: the receiver popped a moment earlier must already be
// the channel's claim (pop and open are one hold), a close crossing the window
// settles it, and the sender's commit then finds close won -- the process dies
// on "send on closed channel", which is the answer the row expects.
const channelCloseWinsWindowPoint = "SP_CHANNEL_RENDEZVOUS_CLAIM_BEFORE_MOVE:block"

func runChannelCloseWinsWindowStand(t *testing.T, bin string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(bin)
	env := overrideEnvVar(os.Environ(), "SURGE_SHARDS", "1")
	env = overrideEnvVar(env, "SURGE_THREADS", "1")
	env = overrideEnvVar(env, "SURGE_CLOSE_WINS_MODE", "claim-window-close")
	env = overrideEnvVar(env, "SURGE_SYNC_POINT", channelCloseWinsWindowPoint)
	cmd.Env = env
	return runCommand(t, cmd, "")
}

func TestRuntimeV2ChannelCloseWinsClaimWindow(t *testing.T) {
	t.Run("pop-and-open-are-one-hold", func(t *testing.T) {
		bin := buildChannelCloseWinsStand(t, "channel_close_wins_window", "-DRT_TEST_SYNC_POINTS")
		stdout, stderr, code := runChannelCloseWinsWindowStand(t, bin)
		if code == 0 {
			t.Fatalf("the sender returned from a rendezvous close had won\nstdout:\n%s\nstderr:\n%s",
				stdout, stderr)
		}
		want := "OK_CLAIM_WINDOW: claim=open_at_window close=settled receiver=closed wakes=1"
		if !strings.Contains(stdout, want) {
			t.Fatalf("claim-window proof missing %q\nstdout:\n%s\nstderr:\n%s", want, stdout, stderr)
		}
		if !strings.Contains(stderr+stdout, "send on closed channel") {
			t.Fatalf("the sender did not answer for the closed channel\nstdout:\n%s\nstderr:\n%s",
				stdout, stderr)
		}
	})
	t.Run("open-after-the-stage-leaves-the-receiver-in-no-store", func(t *testing.T) {
		bin := buildChannelCloseWinsStand(t,
			"channel_close_wins_window_negative",
			"-DRT_TEST_SYNC_POINTS",
			"-DRV2_CLAIM_OPEN_AFTER_STAGE_NEGATIVE_CONTROL")
		stdout, stderr, code := runChannelCloseWinsWindowStand(t, bin)
		if code == 0 {
			t.Fatalf("negative control unexpectedly passed\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
		}
		want := "FAIL: the claim was not open while the lane was released"
		if !strings.Contains(stdout, want) {
			t.Fatalf("negative control failed for the wrong reason; want %q\nstdout:\n%s\nstderr:\n%s",
				want, stdout, stderr)
		}
	})
}

func TestRuntimeV2ChannelCloseWinsNegativeControls(t *testing.T) {
	rows := []struct {
		name string
		flag string
		mode string
		want string
	}{
		{
			name: "close-must-settle-the-claim",
			flag: "-DRV2_CLOSE_WINS_NEGATIVE_CONTROL",
			mode: "reserve-close-commit",
			want: "FAIL: a receiver was handed a value on a closed channel",
		},
		{
			name: "an-open-claim-must-refuse-later-sends",
			flag: "-DRV2_CLAIM_OVERTAKE_NEGATIVE_CONTROL",
			mode: "claim-not-overtaken",
			want: "FAIL: a later send overtook the active rendezvous",
		},
		{
			name: "recovery-must-not-destroy-the-value",
			flag: "-DRV2_DEBT_276_NEGATIVE_CONTROL",
			mode: "dead-receiver",
			want: "FAIL: recovery destroyed the value",
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bin := buildChannelCloseWinsStand(t, "channel_close_wins_negative", row.flag)
			stdout, stderr, code := runChannelCloseWinsStand(t, bin, row.mode)
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
