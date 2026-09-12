//go:build runtime_v2_pending

package vm_test

import (
	_ "embed"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// This native-only observer runs inside the real descriptor callback. The VM
// has no unlocked native sender claim or scheduler task pin to inspect.
func TestRuntimeV2ChannelSendOfferClaimKeepsSenderAlive(t *testing.T) {
	skip := strings.TrimSpace(os.Getenv("SURGE_SKIP_TIMEOUT_TESTS"))
	if testing.Short() || (skip != "" && skip != "0" && !strings.EqualFold(skip, "false")) {
		t.Fatal("sender claim proof requires -short=false and SURGE_SKIP_TIMEOUT_TESTS unset, 0, or false")
	}
	if _, err := exec.LookPath("clang"); err != nil {
		t.Fatalf("required native sender claim compiler unavailable: %v", err)
	}
	bin := buildRuntimeV2LifecycleHarness(t)
	for _, workers := range []string{"1", "8"} {
		for _, route := range []string{"async-direct", "async-refill", "sync-refill"} {
			t.Run("workers-"+workers+"/"+route, func(t *testing.T) {
				env := lifecycleEnv("SURGE_SHARDS=1", "SURGE_THREADS="+workers,
					"SURGE_BLOCKING_THREADS=1", "SURGE_OFFER_CLAIM_ROUTE="+route)
				stdout, stderr, code := runLifecycleHarness(t, bin, "send-offer-claim", env)
				want := "OK_OFFER_CLAIM: route=" + route + " workers=" + workers +
					" rounds=32 callbacks=32 cancelled=32 original=32 final_refs=0"
				if code != 0 || !strings.Contains(stdout, want) || strings.TrimSpace(stderr) != "" {
					t.Fatalf("sender claim proof failed (route=%s workers=%s code=%d), want %q\nstdout:\n%s\nstderr:\n%s",
						route, workers, code, want, stdout, stderr)
				}
				t.Log(stdout)
			})
		}
	}
}

//go:embed testdata/channel_send_offer_claim.c
var lifecycleHarnessSendOfferClaimModes string
