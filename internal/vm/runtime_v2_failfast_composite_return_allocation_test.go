//go:build runtime_v2_pending

package vm_test

import (
	"strconv"
	"strings"
	"testing"
)

// Packet is Copy but owns its counted field. A successful join transfers that
// owner to the caller; a failed join must recursively drop the prepared Packet.
func TestRuntimeV2FailfastReturnReleasesPreparedComposite(t *testing.T) {
	for _, outcome := range []struct {
		name       string
		attribute  string
		cancelSlow string
		check      string
		marker     string
	}{
		{
			name:       "success",
			cancelSlow: "slow.cancel();",
			check: `let packet: Packet = compare r {
            Success(value) => value;
            Cancelled() => { return 3; };
        };
        if packet.value != 1208925819614629174706183 { return 4; }`,
			marker: "FAILFAST_PACKET_SUCCESS",
		},
		{
			name:      "failfast-cancel",
			attribute: "@failfast ",
			check: `let cancelled_ok = compare r {
            Cancelled() => true;
            Success(_) => false;
        };
        if !cancelled_ok { return 3; }`,
			marker: "FAILFAST_PACKET_CANCELLED",
		},
	} {
		t.Run(outcome.name, func(t *testing.T) {
			source := strings.NewReplacer(
				"SCOPE_ATTRIBUTE", outcome.attribute,
				"CANCEL_SLOW", outcome.cancelSlow,
				"CHECK_RESULT", outcome.check,
				"COMPLETION_MARKER", outcome.marker,
			).Replace(failfastCompositeReturnAllocationSource)
			bin := buildAsyncAllocationProgram(t, source)
			for _, workers := range []string{"1", "8"} {
				t.Run("workers-"+workers, func(t *testing.T) {
					env := asyncAllocationEnvironment(t, workers)
					baseline := runAsyncAllocationXML(t, bin, env, "control", "1", "")
					if err := validateAsyncAllocationBaseline(baseline); err != nil {
						t.Fatalf("unapproved allocation baseline: %v", err)
					}
					for _, rounds := range []int{1, 16} {
						t.Run("rounds-"+strconv.Itoa(rounds), func(t *testing.T) {
							marker := strings.Repeat("FAILFAST_PACKET_COMPUTED\n", rounds) + outcome.marker
							subject := runAsyncAllocationXML(t, bin, env, "subject", strconv.Itoa(rounds), marker)
							if err := compareAsyncAllocationRecords(baseline, subject); err != nil {
								t.Fatal(err)
							}
						})
					}
				})
			}
		})
	}
}

const failfastCompositeReturnAllocationSource = `
@copy type Packet = { value: int };

fn make_packet() -> Packet {
    print("FAILFAST_PACKET_COMPUTED");
    return Packet { value = 1208925819614629174706183 };
}

async fn wait_cancelled() -> int64 {
    // No sender or closer can complete this receive.
    let parked = Channel::<int64>::new(0:uint);
    parked.recv();
    return 0:int64;
}

async fn run(rounds: uint64) -> int {
    let mut index: uint64 = 0:uint64;
    while index < rounds {
        let r = (SCOPE_ATTRIBUTEasync {
            let slow = spawn wait_cancelled();
            let fast = spawn wait_cancelled();
            fast.cancel();
            CANCEL_SLOW

            let fast_res = fast.await();
            let fast_cancelled = compare fast_res {
                Cancelled() => true;
                Success(_) => false;
            };
            if !fast_cancelled {
                ret Packet { value = 1 };
            }

            let slow_res = slow.await();
            let slow_cancelled = compare slow_res {
                Cancelled() => true;
                Success(_) => false;
            };
            if !slow_cancelled {
                ret Packet { value = 2 };
            }

            ret make_packet();
        }).await();

        CHECK_RESULT
        index = index + 1:uint64;
    }
    print("COMPLETION_MARKER rounds=" + (index to string));
    return 0;
}

@entrypoint("argv")
fn main(rounds: uint) -> int {
    return compare run(rounds to uint64).await() {
        Success(code) => code;
        Cancelled() => 90;
    };
}
`
