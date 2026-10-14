//go:build runtime_v2_pending

package vm_test

import (
	"fmt"
	"strings"
	"testing"
)

func runRuntimeV2ScopeTeardownSerializerProofs(t *testing.T, release string) {
	t.Helper()
	positive := buildRuntimeV2LifecycleHarnessWithFlags(
		t, "scope_teardown_serializer", []string{"-DRT_TEST_SYNC_POINTS"})

	t.Run("register-before-owner-event-drain", func(t *testing.T) {
		for _, shards := range []int{2, 4, 8} {
			t.Run(fmt.Sprintf("shards-%d", shards), func(t *testing.T) {
				env := lifecycleEnv(
					fmt.Sprintf("SURGE_SHARDS=%d", shards),
					"SURGE_BLOCKING_THREADS=1",
					"SURGE_SYNC_POINT=SP_SCOPE_TEARDOWN_BEFORE_REGISTER:block")
				stdout, stderr, code := runLifecycleHarness(
					t, positive, "scope-cancelled-poll-teardown", env)
				if code != 0 || !strings.Contains(stderr, "scope teardown serialized: active=1 child=3") {
					t.Fatalf("scope teardown verify failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
						code, stdout, stderr)
				}
			})
		}
	})

	t.Run("stamped-route-after-exit", func(t *testing.T) {
		env := lifecycleEnv(
			"SURGE_SHARDS=2", "SURGE_BLOCKING_THREADS=1",
			"SURGE_SYNC_POINT=SP_SCOPE_TEARDOWN_BEFORE_REGISTER:block")
		stdout, stderr, code := runLifecycleHarness(
			t, release, "scope-cancelled-poll-teardown", env)
		if code != 0 {
			t.Fatalf("stamped scope route failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
				code, stdout, stderr)
		}
	})

	t.Run("negative-control-refuses-ownerless-scope", func(t *testing.T) {
		negative := buildRuntimeV2LifecycleHarnessWithFlags(
			t, "scope_key_unstamped", []string{"-DRV2_DEBT_283_NEGATIVE_CONTROL"})
		env := lifecycleEnv("SURGE_SHARDS=2", "SURGE_BLOCKING_THREADS=1")
		_, stderr, code := runLifecycleHarness(t, negative, "scope-cancelled-poll-teardown", env)
		const refusal = "async: scope has no owner shard"
		if code == 0 || !strings.Contains(stderr, refusal) {
			t.Fatalf("negative control did not fail closed on ownerless scope routing (code=%d)\nstderr:\n%s",
				code, stderr)
		}
	})

	t.Run("tsan", func(t *testing.T) {
		bin := buildRuntimeV2LifecycleHarnessWithFlags(t, "scope_teardown_tsan", []string{
			"-DRT_TEST_SYNC_POINTS", "-fsanitize=thread", "-g", "-O1",
		})
		env := lifecycleEnv(
			"SURGE_SHARDS=2", "SURGE_BLOCKING_THREADS=1",
			"SURGE_SYNC_POINT=SP_SCOPE_TEARDOWN_BEFORE_REGISTER:block")
		stdout, stderr, code := runLifecycleHarness(t, bin, "scope-cancelled-poll-teardown", env)
		if strings.Contains(stderr, "unexpected memory mapping") {
			t.Skipf("ThreadSanitizer unavailable in this environment: %s", stderr)
		}
		if code != 0 {
			t.Fatalf("TSan scope teardown failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
				code, stdout, stderr)
		}
	})
}
