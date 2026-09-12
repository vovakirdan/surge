package vm_test

import (
	"encoding/xml"
	"fmt"
	"strings"
	"testing"
)

// Synthetic XML exercises parser/oracle decisions only. The separate retained
// channel test compiles and leaks a real resource under the actual Valgrind.
func asyncAllocationXMLFixture(t *testing.T, records []asyncAllocationRecord) []byte {
	t.Helper()
	var body strings.Builder
	body.WriteString(`<valgrindoutput><protocolversion>4</protocolversion><protocoltool>memcheck</protocoltool><tool>memcheck</tool><status><state>RUNNING</state></status><status><state>FINISHED</state></status>`)
	for _, record := range records {
		node := struct {
			XMLName xml.Name `xml:"error"`
			asyncAllocationRecord
		}{asyncAllocationRecord: record}
		data, err := xml.Marshal(node)
		if err != nil {
			t.Fatal(err)
		}
		body.Write(data)
	}
	body.WriteString(`<errorcounts></errorcounts><suppcounts></suppcounts></valgrindoutput>`)
	return []byte(body.String())
}

func asyncAllocationFixtureRecord(chain string) asyncAllocationRecord {
	record := asyncAllocationRecord{Unique: "0x1", Kind: "Leak_StillReachable"}
	record.What.Bytes, record.What.Blocks = 640, 1
	for _, name := range strings.Split(chain+">main>_start", ">") {
		record.Frames = append(record.Frames, asyncAllocationFrame{
			Object: "/exact/program", Function: name, Dir: "/exact/source", File: "async_allocation_baseline.c", Line: 12,
		})
	}
	return record
}

const asyncAllocationFixtureChain = "malloc>rt_alloc>rt_realloc>rt_waiter_store_ensure_cap>allocation_baseline_bootstrap"

func TestRuntimeV2AsyncAllocationXMLRejectsMalformedReports(t *testing.T) {
	good := string(asyncAllocationXMLFixture(t, []asyncAllocationRecord{asyncAllocationFixtureRecord(asyncAllocationFixtureChain)}))
	mutations := []struct{ name, from, to, diagnosis string }{
		{"truncated-root", "</valgrindoutput>", "", "malformed"},
		{"wrong-root", "valgrindoutput", "other", "malformed"},
		{"second-root", "</valgrindoutput>", "</valgrindoutput><other/>", "after Valgrind XML root"},
		{"wrong-protocol", "<protocolversion>4", "<protocolversion>3", "protocol 4"},
		{"duplicate-protocol", "<protocolversion>4</protocolversion>", "<protocolversion>4</protocolversion><protocolversion>4</protocolversion>", "protocol 4"},
		{"wrong-tool", "<tool>memcheck", "<tool>other", "protocol 4"},
		{"missing-finished", "<status><state>FINISHED</state></status>", "", "FINISHED"},
		{"duplicate-finished", "<status><state>FINISHED</state></status>", "<status><state>FINISHED</state></status><status><state>FINISHED</state></status>", "FINISHED"},
		{"definite-leak", "Leak_StillReachable", "Leak_DefinitelyLost", "forbidden Memcheck error"},
		{"indirect-leak", "Leak_StillReachable", "Leak_IndirectlyLost", "forbidden Memcheck error"},
		{"invalid-read", "Leak_StillReachable", "InvalidRead", "forbidden Memcheck error"},
		{"unknown-error", "Leak_StillReachable", "UnknownError", "forbidden Memcheck error"},
		{"missing-bytes", "<leakedbytes>640</leakedbytes>", "", "allocation amounts"},
		{"missing-blocks", "<leakedblocks>1</leakedblocks>", "", "allocation amounts"},
		{"missing-id", "<unique>0x1</unique>", "", "record id"},
		{"missing-symbol", "<fn>malloc</fn>", "", "symbolic allocation frame"},
		{"suppressed-count", "<suppcounts></suppcounts>", "<suppcounts><pair><count>1</count><name>hidden</name></pair></suppcounts>", "suppressed errors"},
		{"suppressed-record", "</error>", "<suppression><sname>hidden</sname></suppression></error>", "suppressed record"},
		{"missing-suppcounts", "<suppcounts></suppcounts>", "", "suppressed errors"},
		{"missing-errorcounts", "<errorcounts></errorcounts>", "", "errorcounts"},
		{"unknown-error-count", "<errorcounts></errorcounts>", "<errorcounts><pair><count>1</count><unique>0x9</unique></pair></errorcounts>", "error count"},
		{"fatal-signal", "</valgrindoutput>", "<fatal_signal><signo>11</signo></fatal_signal></valgrindoutput>", "fatal signal"},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			if !strings.Contains(good, mutation.from) {
				t.Fatalf("negative mutation did not select fixture content %q", mutation.from)
			}
			_, err := parseAsyncAllocationXML([]byte(strings.ReplaceAll(good, mutation.from, mutation.to)))
			if err == nil || !strings.Contains(err.Error(), mutation.diagnosis) {
				t.Fatalf("mutation %s: got %v, want %s", mutation.name, err, mutation.diagnosis)
			}
		})
	}
	for _, scenario := range []string{"missing-records", "duplicate-id", "missing-stack", "saturated-stack"} {
		t.Run(scenario, func(t *testing.T) {
			record := asyncAllocationFixtureRecord(asyncAllocationFixtureChain)
			records := []asyncAllocationRecord{record}
			switch scenario {
			case "missing-records":
				records = nil
			case "duplicate-id":
				records = append(records, record)
			case "missing-stack":
				records[0].Frames = nil
			case "saturated-stack":
				records[0].Frames = make([]asyncAllocationFrame, asyncAllocationStackLimit)
			}
			if _, err := parseAsyncAllocationXML(asyncAllocationXMLFixture(t, records)); err == nil {
				t.Fatalf("accepted %s", scenario)
			}
		})
	}
}

func TestRuntimeV2AsyncAllocationXMLExactMultiset(t *testing.T) {
	baseline := []asyncAllocationRecord{asyncAllocationFixtureRecord(asyncAllocationFixtureChain)}
	for _, mutation := range []string{"missing", "extra", "duplicate-stack", "bytes", "blocks", "kind", "deep-symbol", "deep-file", "deep-line", "object", "directory"} {
		t.Run(mutation, func(t *testing.T) {
			subject := []asyncAllocationRecord{asyncAllocationFixtureRecord(asyncAllocationFixtureChain)}
			switch mutation {
			case "missing":
				subject = nil
			case "extra", "duplicate-stack":
				extra := asyncAllocationFixtureRecord(asyncAllocationFixtureChain)
				extra.Unique = "0x2"
				if mutation == "extra" {
					extra.Frames[2].Function = "rt_channel_new"
				}
				subject = append(subject, extra)
			case "bytes":
				subject[0].What.Bytes++
			case "blocks":
				subject[0].What.Blocks++
			case "kind":
				subject[0].Kind = "Leak_PossiblyLost"
			case "deep-symbol":
				subject[0].Frames[len(subject[0].Frames)-1].Function = "different_start"
			case "deep-file":
				subject[0].Frames[len(subject[0].Frames)-1].File = "other.c"
			case "deep-line":
				subject[0].Frames[len(subject[0].Frames)-1].Line++
			case "object":
				subject[0].Frames[0].Object = "/other/program"
			case "directory":
				subject[0].Frames[0].Dir = "/other/source"
			}
			if err := compareAsyncAllocationRecords(baseline, subject); err == nil {
				t.Fatalf("accepted %s allocation mutation", mutation)
			}
		})
	}
	t.Run("full-stack-tail", func(t *testing.T) {
		first := asyncAllocationFixtureRecord(asyncAllocationFixtureChain)
		second := asyncAllocationFixtureRecord(asyncAllocationFixtureChain)
		// A first-12-frames comparison must fail this test. Keep the changed
		// frame beyond that bound, as real compiler/task call stacks can be.
		for range 32 {
			first.Frames = append(first.Frames, first.Frames[0])
			second.Frames = append(second.Frames, second.Frames[0])
		}
		second.Frames[len(second.Frames)-1].Line++
		if err := compareAsyncAllocationRecords([]asyncAllocationRecord{first}, []asyncAllocationRecord{second}); err == nil {
			t.Fatal("allocation comparison truncated its symbolic stack")
		}
	})
	t.Run("record-order", func(t *testing.T) {
		first := asyncAllocationFixtureRecord(asyncAllocationFixtureChain)
		second := asyncAllocationFixtureRecord(asyncAllocationFixtureChain)
		second.What.Bytes++
		if err := compareAsyncAllocationRecords([]asyncAllocationRecord{first, second}, []asyncAllocationRecord{second, first}); err != nil {
			t.Fatalf("record ordering changed the multiset: %v", err)
		}
	})
	good := string(asyncAllocationXMLFixture(t, baseline))
	first, err := parseAsyncAllocationXML([]byte(strings.Replace(good, "<frame>", "<frame><ip>0x123</ip>", 1)))
	if err != nil {
		t.Fatal(err)
	}
	secondXML := strings.ReplaceAll(good, "0x1", "0x999")
	secondXML = strings.Replace(secondXML, "<frame>", "<frame><ip>0x456</ip>", 1)
	secondXML = strings.Replace(secondXML, "<tool>", "<pid>42</pid><ppid>41</ppid><tool>", 1)
	second, err := parseAsyncAllocationXML([]byte(secondXML))
	if err != nil {
		t.Fatal(err)
	}
	if err := compareAsyncAllocationRecords(first, second); err != nil {
		t.Fatalf("process ids/addresses changed the symbolic multiset: %v", err)
	}
}

func TestRuntimeV2AsyncAllocationBaselineOrigins(t *testing.T) {
	var records []asyncAllocationRecord
	for chain, name := range asyncAllocationBootstrapOrigins {
		record := asyncAllocationFixtureRecord(chain)
		record.Unique = fmt.Sprintf("0x%x", len(records)+1)
		if strings.HasPrefix(name, "glibc ") {
			record.Kind = "Leak_PossiblyLost"
		}
		records = append(records, record)
		if origin, err := asyncAllocationOrigin(record); err != nil || origin != name {
			t.Fatalf("approved origin %s: %s, %v", name, origin, err)
		}
	}
	if err := validateAsyncAllocationBaseline(records); err != nil {
		t.Fatal(err)
	}
	if err := validateAsyncAllocationBaseline(nil); err == nil {
		t.Fatal("accepted an empty baseline without its prewarmed capacities")
	}
	for _, mutation := range []string{"extra-ancestor", "fake-bootstrap", "missing-bootstrap", "ordinary-possibly-lost"} {
		t.Run(mutation, func(t *testing.T) {
			record := asyncAllocationFixtureRecord(asyncAllocationFixtureChain)
			switch mutation {
			case "extra-ancestor":
				record.Frames = append(record.Frames[:4], append([]asyncAllocationFrame{record.Frames[0]}, record.Frames[4:]...)...)
			case "fake-bootstrap":
				record.Frames[4].File = "user_subject.c"
			case "missing-bootstrap":
				record.Frames[4].Function = "subject"
			case "ordinary-possibly-lost":
				record.Kind = "Leak_PossiblyLost"
			}
			if _, err := asyncAllocationOrigin(record); err == nil {
				t.Fatalf("accepted %s", mutation)
			}
		})
	}
}
