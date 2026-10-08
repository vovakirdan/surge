//go:build runtime_v2_pending

package vm_test

import (
	"fmt"
	"strings"
	"testing"
)

// Reservation refusal and pool exhaustion are pending WITHOUT source transfer;
// rendezvous handoff and staging may be pending WITH transfer. The descriptor's
// move census, independent of the runtime result, distinguishes these cases.
func TestChannelSendTrackedReportsActualTransfer(t *testing.T) {
	bin := buildChannelClaimRetryStand(t, "channel_send_tracked")
	for _, branch := range []string{"ack", "cancel-before-take", "claim-refusal", "pool-full",
		"ring", "rendezvous", "recovery-continue", "staged-repoll",
		"retry-source", "resume-without-source", "ack-window"} {
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
					stdout, stderr, code := runChannelClaimRetryStand(t, bin, "offer-tracked-"+mode)
					want := fmt.Sprintf("OK_OFFER: mode=%s yield=%d clear=%d original=1 final_refs=0",
						branch, offerBoolInt(yield), offerBoolInt(clearSource))
					if code != 0 || !strings.Contains(stdout, want) {
						t.Fatalf("source transfer report failed: code=%d stdout=%s stderr=%s", code, stdout, stderr)
					}
				})
			}
		}
	}
}
