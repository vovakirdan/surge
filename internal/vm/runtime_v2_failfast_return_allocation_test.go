//go:build runtime_v2_pending

package vm_test

import (
	"strconv"
	"strings"
	"testing"
)

// Both children must be cancelled and awaited before the owner prepares its
// return. The failfast join then discards that value. This proves the reachable
// cancellation edge; it does not require the join to suspend before answering.
func TestRuntimeV2FailfastReturnReleasesPreparedValue(t *testing.T) {
	for _, variant := range []struct {
		name       string
		resultType string
		result     string
	}{
		{name: "plain-int64", resultType: "int64", result: "7:int64"},
		{name: "counted-int", resultType: "int", result: "1208925819614629174706183"}, // 2^80 + 7
	} {
		t.Run(variant.name, func(t *testing.T) {
			source := strings.NewReplacer(
				"RESULT_TYPE", variant.resultType,
				"RESULT_VALUE", variant.result,
			).Replace(failfastReturnAllocationSource)
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
							// Every helper call must precede the final CANCELLED marker.
							// Missing evaluation cannot pass as successful cancellation.
							marker := strings.Repeat("FAILFAST_RESULT_COMPUTED\n", rounds) + "FAILFAST_RETURN_CANCELLED"
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

const failfastReturnAllocationSource = `
fn make_heap_result() -> RESULT_TYPE {
    print("FAILFAST_RESULT_COMPUTED");
    return RESULT_VALUE;
}

async fn wait_cancelled() -> int64 {
    while true {
        checkpoint().await();
    }
    return 0:int64;
}

async fn run(rounds: uint64) -> int {
    let mut index: uint64 = 0:uint64;
    while index < rounds {
        let r = (@failfast async {
            let slow = spawn wait_cancelled();
            let fast = spawn wait_cancelled();

            fast.cancel();
            let fast_res = fast.await();
            let fast_cancelled = compare fast_res {
                Cancelled() => true;
                Success(_) => false;
            };
            if !fast_cancelled {
                ret 1:RESULT_TYPE;
            }

            let slow_res = slow.await();
            let slow_cancelled = compare slow_res {
                Cancelled() => true;
                Success(_) => false;
            };
            if !slow_cancelled {
                ret 2:RESULT_TYPE;
            }

            ret make_heap_result();
        }).await();

        let cancelled_ok = compare r {
            Cancelled() => true;
            Success(_) => false;
        };
        if !cancelled_ok {
            return 3;
        }
        index = index + 1:uint64;
    }
    print("FAILFAST_RETURN_CANCELLED rounds=" + (index to string));
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
