package vm_test

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
)

const asyncAllocationStackLimit = 500

type asyncAllocationFrame struct {
	Object   string `xml:"obj"`
	Function string `xml:"fn"`
	Dir      string `xml:"dir"`
	File     string `xml:"file"`
	Line     int    `xml:"line"`
}

type asyncAllocationRecord struct {
	Unique string `xml:"unique" json:"-"`
	Kind   string `xml:"kind"`
	What   struct {
		Bytes  uint64 `xml:"leakedbytes"`
		Blocks uint64 `xml:"leakedblocks"`
	} `xml:"xwhat"`
	Frames      []asyncAllocationFrame `xml:"stack>frame"`
	Suppression []struct{}             `xml:"suppression" json:"-"`
}

type asyncAllocationCount struct {
	Unique string `xml:"unique"`
	Count  uint64 `xml:"count"`
}

type asyncAllocationXML struct {
	XMLName      xml.Name `xml:"valgrindoutput"`
	Protocol     []int    `xml:"protocolversion"`
	ProtocolTool []string `xml:"protocoltool"`
	Tool         []string `xml:"tool"`
	Status       []struct {
		State string `xml:"state"`
	} `xml:"status"`
	Records []asyncAllocationRecord `xml:"error"`
	Counts  []struct {
		Pairs []asyncAllocationCount `xml:"pair"`
	} `xml:"errorcounts"`
	Suppressed []struct {
		Pairs []struct{} `xml:"pair"`
	} `xml:"suppcounts"`
	Fatal []struct{} `xml:"fatal_signal"`
}

func parseAsyncAllocationXML(data []byte) ([]asyncAllocationRecord, error) {
	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	var report asyncAllocationXML
	if err := decoder.Decode(&report); err != nil {
		return nil, fmt.Errorf("malformed Valgrind XML: %w", err)
	}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("trailing Valgrind XML: %w", err)
		}
		if text, ok := token.(xml.CharData); !ok || strings.TrimSpace(string(text)) != "" {
			return nil, fmt.Errorf("unexpected content after Valgrind XML root")
		}
	}
	if report.XMLName.Space != "" || len(report.Protocol) != 1 || report.Protocol[0] != 4 ||
		len(report.ProtocolTool) != 1 || report.ProtocolTool[0] != "memcheck" || len(report.Tool) != 1 || report.Tool[0] != "memcheck" {
		return nil, fmt.Errorf("expected Memcheck XML protocol 4")
	}
	if len(report.Status) != 2 || report.Status[0].State != "RUNNING" || report.Status[1].State != "FINISHED" {
		return nil, fmt.Errorf("expected one RUNNING and one FINISHED status")
	}
	if len(report.Fatal) != 0 || len(report.Suppressed) != 1 || len(report.Suppressed[0].Pairs) != 0 {
		return nil, fmt.Errorf("fatal signal or suppressed errors invalidate the allocation proof")
	}
	if len(report.Records) == 0 {
		return nil, fmt.Errorf("missing bootstrap allocation records")
	}
	seen := make(map[string]bool)
	for _, record := range report.Records {
		if record.Unique == "" || seen[record.Unique] {
			return nil, fmt.Errorf("missing or duplicate allocation record id %q", record.Unique)
		}
		seen[record.Unique] = true
		if record.Kind != "Leak_StillReachable" && record.Kind != "Leak_PossiblyLost" {
			return nil, fmt.Errorf("forbidden Memcheck error %s", record.Kind)
		}
		if record.What.Bytes == 0 || record.What.Blocks == 0 || len(record.Suppression) != 0 {
			return nil, fmt.Errorf("missing allocation amounts or suppressed record %s", record.Unique)
		}
		if len(record.Frames) == 0 || len(record.Frames) >= asyncAllocationStackLimit {
			return nil, fmt.Errorf("missing or truncated allocation stack %s", record.Unique)
		}
		for _, frame := range record.Frames {
			if frame.Object == "" || frame.Function == "" || frame.Line < 0 || (frame.Line > 0 && frame.File == "") {
				return nil, fmt.Errorf("incomplete symbolic allocation frame in %s", record.Unique)
			}
		}
	}
	if len(report.Counts) != 1 {
		return nil, fmt.Errorf("missing or duplicate errorcounts section")
	}
	counted := make(map[string]bool)
	for _, count := range report.Counts[0].Pairs {
		if count.Count == 0 || !seen[count.Unique] || counted[count.Unique] {
			return nil, fmt.Errorf("invalid Memcheck error count for %q", count.Unique)
		}
		counted[count.Unique] = true
	}
	return report.Records, nil
}

// Exact allocation-function chains from allocator to our shared bootstrap.
// These are origins, not a byte budget or an arbitrary ancestor allowance.
// Every frame (including source location) is then compared with the subject.
// A libc/toolchain origin change must be reviewed, never silently stripped.
var asyncAllocationBootstrapOrigins = func() map[string]string {
	initTail := ">exec_init_once>__pthread_once_slow>ensure_exec>allocation_baseline_bootstrap"
	origins := map[string]string{
		"malloc>rt_alloc>rt_shard_scheduler_init>rt_runtime_init_shard_schedulers" + initTail:                             "scheduler arrays",
		"malloc>rt_alloc>deque_reserve>deque_prepare>rt_shard_scheduler_init>rt_runtime_init_shard_schedulers" + initTail: "initial ready queues",
		"malloc>rt_alloc>rt_remote_task_state_init" + initTail:                                                            "remote task state",
		"malloc>rt_alloc>rt_far_channel_state_init" + initTail:                                                            "far channel state",
		"malloc>rt_alloc>rt_blocking_init" + initTail:                                                                     "blocking thread table",
		"calloc>rt_blocking_init" + initTail:                                                                              "blocking worker contexts",
		"calloc>allocate_cells>rt_heap_accounting_prepare_cells" + initTail:                                               "heap accounting cells",
		"malloc>rt_alloc>rt_start_workers" + initTail:                                                                     "worker thread table and contexts",
		"malloc>rt_alloc>ensure_segment_locked>ensure_task_cap>allocation_baseline_bootstrap":                             "first task segment",
		"malloc>rt_alloc>ensure_scope_segment_locked>ensure_scope_cap>allocation_baseline_bootstrap":                      "first scope segment",
		"malloc>rt_alloc>rt_realloc>rt_waiter_store_ensure_cap>allocation_baseline_bootstrap":                             "first join waiter store",
	}
	tls := "calloc>calloc>allocate_dtv>_dl_allocate_tls>allocate_stack>pthread_create@@GLIBC_2.34>"
	origins[tls+"rt_blocking_init"+initTail] = "glibc blocking worker TLS"
	origins[tls+"rt_start_workers"+initTail] = "glibc scheduler/io worker TLS"
	return origins
}()

func asyncAllocationOrigin(record asyncAllocationRecord) (string, error) {
	var functions []string
	for index, frame := range record.Frames {
		functions = append(functions, frame.Function)
		if frame.Function != "allocation_baseline_bootstrap" {
			continue
		}
		if filepath.Base(frame.File) != "async_allocation_baseline.c" ||
			index+1 >= len(record.Frames) || record.Frames[index+1].Function != "main" {
			return "", fmt.Errorf("bootstrap must be the driver's direct main call")
		}
		origin, ok := asyncAllocationBootstrapOrigins[strings.Join(functions, ">")]
		if !ok {
			return "", fmt.Errorf("unapproved bootstrap allocation origin: %s", strings.Join(functions, ">"))
		}
		isTLS := strings.HasPrefix(origin, "glibc ")
		if (record.Kind == "Leak_PossiblyLost") != isTLS {
			return "", fmt.Errorf("allocation kind %s disagrees with bootstrap origin %s", record.Kind, origin)
		}
		return origin, nil
	}
	return "", fmt.Errorf("allocation did not originate in the shared bootstrap")
}

func validateAsyncAllocationBaseline(records []asyncAllocationRecord) error {
	origins := make(map[string]bool)
	for _, record := range records {
		origin, err := asyncAllocationOrigin(record)
		if err != nil {
			return err
		}
		origins[origin] = true
	}
	for _, required := range []string{"first task segment", "first scope segment", "first join waiter store", "heap accounting cells", "initial ready queues"} {
		if !origins[required] {
			return fmt.Errorf("baseline is missing required origin %s", required)
		}
	}
	return nil
}

func asyncAllocationMultiset(records []asyncAllocationRecord) map[string]int {
	out := make(map[string]int)
	for _, record := range records {
		// XML record IDs and frame addresses are process metadata. Symbol names,
		// every source location, object paths, leak kind, bytes and blocks remain.
		key, _ := json.Marshal(record)
		out[string(key)]++
	}
	return out
}

func compareAsyncAllocationRecords(baseline, subject []asyncAllocationRecord) error {
	want, got := asyncAllocationMultiset(baseline), asyncAllocationMultiset(subject)
	var differences []string
	for key, count := range want {
		if got[key] != count {
			differences = append(differences, fmt.Sprintf("baseline=%d subject=%d %s", count, got[key], key))
		}
	}
	for key, count := range got {
		if want[key] == 0 {
			differences = append(differences, fmt.Sprintf("extra subject=%d %s", count, key))
		}
	}
	if len(differences) != 0 {
		sort.Strings(differences)
		return fmt.Errorf("allocation baseline mismatch:\n%s", strings.Join(differences, "\n"))
	}
	return nil
}
