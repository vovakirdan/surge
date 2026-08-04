package vm_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const carrierBenchCounterHarness = `
#include "rt_carrier_bench.h"

#include <pthread.h>
#include <sched.h>
#include <signal.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

static void* close_marker(void* unused) {
    (void)unused;
    rt_carrier_bench_marker();
    return NULL;
}

int main(int argc, char** argv) {
    if (rt_carrier_bench_init() != 0) {
        return 1;
    }
    const char* mode = argc > 1 ? argv[1] : "valid";
    if (strcmp(mode, "disabled") == 0) {
        rt_carrier_bench_marker();
        rt_carrier_bench_record_copy(1);
        return rt_carrier_bench_finish();
    }
    if (strcmp(mode, "exit-zero") == 0) {
        exit(0);
    }
    if (strcmp(mode, "signal") == 0) {
        raise(SIGTERM);
    }
    if (strcmp(mode, "missing") == 0) {
        return rt_carrier_bench_finish();
    }
    rt_carrier_bench_marker();
    if (strcmp(mode, "one") == 0) {
        return rt_carrier_bench_finish();
    }
    if (strcmp(mode, "overflow") == 0) {
        rt_carrier_bench_record_copy(UINT64_MAX);
        rt_carrier_bench_record_copy(1);
    } else if (strcmp(mode, "underflow") == 0) {
        rt_carrier_bench_transport_release(1);
    } else if (strcmp(mode, "concurrent") == 0) {
        if (!rt_carrier_bench_test_hook_enter()) {
            return 91;
        }
        pthread_t thread;
        if (pthread_create(&thread, NULL, close_marker, NULL) != 0) {
            return 92;
        }
        while (rt_carrier_bench_test_state() != 3) {
            sched_yield();
        }
        rt_carrier_bench_marker();
        rt_carrier_bench_test_hook_leave();
        if (pthread_join(thread, NULL) != 0) {
            return 93;
        }
        return rt_carrier_bench_finish();
    } else {
        rt_carrier_bench_record_copy(11);
        rt_carrier_bench_record_move(12);
        rt_carrier_bench_record_callback();
        rt_carrier_bench_record_credit_stall();
        rt_carrier_bench_transport_acquire(9);
        rt_carrier_bench_transport_release(9);
    }
    rt_carrier_bench_marker();
    if (strcmp(mode, "extra") == 0) {
        rt_carrier_bench_marker();
    } else if (strcmp(mode, "late") == 0) {
        rt_carrier_bench_record_move(1);
    }
    return rt_carrier_bench_finish();
}
`

type carrierBenchCounterRecord struct {
	SchemaVersion  int               `json:"schema_version"`
	Status         string            `json:"status"`
	Probe          string            `json:"probe"`
	Nonce          string            `json:"nonce"`
	ProtocolSHA256 string            `json:"protocol_sha256"`
	Metrics        map[string]uint64 `json:"metrics"`
	Error          *string           `json:"error"`
}

func TestRuntimeV2CarrierBenchCounterMatrix(t *testing.T) {
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Fatal("clang is required for the fail-closed carrier benchmark gate")
	}
	root := repoRoot(t)
	temporary := t.TempDir()
	harness := filepath.Join(temporary, "carrier_bench_counter.c")
	binary := filepath.Join(temporary, "carrier_bench_counter")
	if err := os.WriteFile(harness, []byte(carrierBenchCounterHarness), 0o600); err != nil {
		t.Fatalf("write carrier benchmark harness: %v", err)
	}
	compile := exec.Command(clang,
		"-std=c11", "-O3", "-Wall", "-Wextra", "-Werror", "-pthread",
		"-DRT_CARRIER_BENCH_TESTING", "-I", filepath.Join(root, "runtime", "native"),
		filepath.Join(root, "runtime", "native", "rt_carrier_bench.c"),
		filepath.Join(root, "runtime", "native", "rt_carrier_bench_report.c"),
		harness, "-o", binary,
	)
	if output, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("compile carrier benchmark harness: %v\n%s", err, output)
	}

	valid := runCarrierBenchCounter(t, binary, "valid", true, true)
	wantMetrics := map[string]uint64{
		"bytes_copied":         11,
		"bytes_moved":          12,
		"callback_count":       1,
		"credit_stalls":        1,
		"peak_transport_bytes": 9,
	}
	if !reflect.DeepEqual(valid.Metrics, wantMetrics) {
		t.Fatalf("valid metrics = %v, want %v", valid.Metrics, wantMetrics)
	}

	for mode, wantError := range map[string]string{
		"missing":    "missing_or_unclosed_marker",
		"one":        "missing_or_unclosed_marker",
		"extra":      "extra_marker",
		"concurrent": "concurrent_marker",
		"late":       "late_carrier_event",
		"overflow":   "counter_overflow",
		"underflow":  "transport_underflow",
		"exit-zero":  "missing_or_unclosed_marker",
		"signal":     "abnormal_exit",
	} {
		t.Run(mode, func(t *testing.T) {
			record := runCarrierBenchCounter(t, binary, mode, true, false)
			if record.Error == nil || *record.Error != wantError {
				t.Fatalf("error = %v, want %q", record.Error, wantError)
			}
		})
	}

	t.Run("invalid-environment", func(t *testing.T) {
		command := exec.Command(binary, "valid")
		command.Env = append(os.Environ(),
			"SURGE_CARRIER_BENCH_COUNTERS=1",
			"SURGE_CARRIER_BENCH_PROBE=probe",
			"SURGE_CARRIER_BENCH_NONCE=wrong",
			"SURGE_CARRIER_BENCH_PROTOCOL_SHA256="+strings.Repeat("a", 64),
		)
		output, err := command.CombinedOutput()
		if err == nil {
			t.Fatal("malformed counter identity unexpectedly succeeded")
		}
		record := parseCarrierBenchCounterRecord(t, string(output))
		if record.Error == nil || *record.Error != "invalid_environment" {
			t.Fatalf("invalid environment error = %v", record.Error)
		}
	})

	t.Run("disabled", func(t *testing.T) {
		command := exec.Command(binary, "disabled")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("disabled bridge failed: %v\n%s", err, output)
		}
		if len(output) != 0 {
			t.Fatalf("disabled bridge emitted output: %q", output)
		}
	})
}

func runCarrierBenchCounter(
	t *testing.T, binary string, mode string, enabled bool, wantSuccess bool,
) carrierBenchCounterRecord {
	t.Helper()
	command := exec.Command(binary, mode)
	command.Env = os.Environ()
	if enabled {
		command.Env = append(command.Env,
			"SURGE_CARRIER_BENCH_COUNTERS=1",
			"SURGE_CARRIER_BENCH_PROBE=probe",
			"SURGE_CARRIER_BENCH_NONCE="+strings.Repeat("b", 32),
			"SURGE_CARRIER_BENCH_PROTOCOL_SHA256="+strings.Repeat("a", 64),
		)
	}
	output, err := command.CombinedOutput()
	if wantSuccess && err != nil {
		t.Fatalf("%s failed: %v\n%s", mode, err, output)
	}
	if !wantSuccess && err == nil {
		t.Fatalf("%s unexpectedly succeeded\n%s", mode, output)
	}
	record := parseCarrierBenchCounterRecord(t, string(output))
	if enabled && record.Probe != "probe" {
		t.Fatalf("counter probe identity drifted: %+v", record)
	}
	return record
}

func parseCarrierBenchCounterRecord(t *testing.T, output string) carrierBenchCounterRecord {
	t.Helper()
	const prefix = "SURGE_CARRIER_COUNTERS "
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	if len(lines) != 1 || !strings.HasPrefix(lines[0], prefix) {
		t.Fatalf("counter output must be exactly one prefixed record: %q", output)
	}
	var record carrierBenchCounterRecord
	if err := json.Unmarshal([]byte(strings.TrimPrefix(lines[0], prefix)), &record); err != nil {
		t.Fatalf("parse counter record: %v\n%s", err, output)
	}
	if record.SchemaVersion != 1 {
		t.Fatalf("counter identity drifted: %+v", record)
	}
	return record
}

func TestRuntimeV2CarrierBenchBridgeHasNoHotPathEnvironmentLookup(t *testing.T) {
	root := repoRoot(t)
	bridge, err := os.ReadFile(filepath.Join(root, "runtime", "native", "rt_carrier_bench.c"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(bridge), "getenv(") {
		t.Fatal("carrier event/marker module must not read the environment")
	}

	reclaim, err := os.ReadFile(filepath.Join(root, "runtime", "native", "rt_array_reclaim.c"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(reclaim), "rt_carrier_bench_marker();") != 1 {
		t.Fatal("existing uint64 marker must have exactly one benchmark hook")
	}

	entry, err := os.ReadFile(filepath.Join(root, "runtime", "native", "rt_entry.c"))
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range []string{"rt_carrier_bench_init()", "rt_carrier_bench_finish()"} {
		if strings.Count(string(entry), call) != 1 {
			t.Fatalf("entry must call %s exactly once", call)
		}
	}
}
