//go:build runtime_v2_pending

package vm_test

import (
	"fmt"
	"strings"
	"testing"
)

func TestRuntimeV2ChannelSendOfferTakeOrDrop(t *testing.T) {
	bin := buildChannelClaimRetryStand(t, "channel_send_offer")
	for _, branch := range []string{"ack", "cancel-before-take", "claim-refusal", "pool-full",
		"ring", "rendezvous", "recovery-continue", "staged-repoll"} {
		for _, yield := range []bool{false, true} {
			for _, clearSource := range []bool{false, true} {
				mode := branch
				if clearSource {
					mode = "clear-" + mode
				}
				if yield {
					mode = "yield-" + mode
				}
				t.Run(mode, func(t *testing.T) {
					stdout, stderr, code := runChannelClaimRetryStand(t, bin, "offer-"+mode)
					want := fmt.Sprintf("OK_OFFER: mode=%s yield=%d clear=%d original=1 final_refs=0",
						branch, offerBoolInt(yield), offerBoolInt(clearSource))
					if code != 0 || !strings.Contains(stdout, want) {
						t.Fatalf("offer branch failed (code=%d), want %q\nstdout:\n%s\nstderr:\n%s", code, want, stdout, stderr)
					}
				})
			}
		}
	}
}

func TestRuntimeV2ChannelSendOfferOldAPINegativeControl(t *testing.T) {
	bin := buildChannelClaimRetryStand(t, "channel_send_offer_old_api", "-DRV2_SEND_OFFER_OLD_API_NEGATIVE_CONTROL")
	for _, mode := range []string{"offer-ack", "offer-yield-clear-ack"} {
		t.Run(mode, func(t *testing.T) {
			stdout, stderr, code := runChannelClaimRetryStand(t, bin, mode)
			if code == 0 || !strings.Contains(stdout, "FAIL: offer must transfer or drop exactly one reference") ||
				!strings.Contains(stderr, "offer census: take=0 moves=0 drops=0") {
				t.Fatalf("old API failed without the named missing-release census (code=%d)\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
			}
		})
	}
}

func offerBoolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
