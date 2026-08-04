package carriergate

import (
	"reflect"
	"testing"
)

func TestScanCTracksSemanticWordAliases(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   []string
	}{
		{
			name: "direct carrier source",
			source: `void f(void) {
    uint64_t carrier = sender->resume_bits;
    uint64_t reply = op ? task->result_bits : 0;
    uint64_t call_result = __surge_blocking_call(job->fn_id, job->state);
}`,
			want: []string{"call_result", "carrier", "reply"},
		},
		{
			name: "zero initialized dataflow",
			source: `void f(void) {
    uint64_t buffered = 0;
    if (buf_pop(ch, &buffered)) { use(buffered); }
    uint64_t staged = 0;
    if (live) { staged = sender->resume_bits; }
}`,
			want: []string{"buffered", "staged"},
		},
		{
			name: "carrier signatures",
			source: `void rt_async_return(void* state, uint64_t word);
bool map_key_eq(const Map* map, uint64_t key_bits, uint64_t entry_word);
bool rt_map_insert(void* map, uint64_t key_bits, uint64_t value_bits, uint64_t* previous);
static int select_return_arms(Pending* pending, uint64_t* returned, uint64_t count);
`,
			want: []string{"entry_word", "previous", "returned", "word"},
		},
		{
			name: "carrier array",
			source: `void f(void) {
    uint64_t words[MAX_ARMS];
    for (uint64_t i = 0; i < count; i++) { words[i] = arms[i].send_bits; }
}`,
			want: []string{"words"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := nativeWordTokens(scanCFile("runtime/native/example.c", []byte(test.source)))
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("native word tokens = %v, want %v", got, test.want)
			}
		})
	}
}

func TestScanCRejectsNumericAndDisconnectedWordAliases(t *testing.T) {
	source := `void f(void) {
// uint64_t commented = sender->result_bits;
const char* text = "uint64_t quoted = __surge_blocking_call(1, state);";
uint64_t result = clock_ticks();
uint64_t result_bits = task->result_bits;
uint64_t val = 0;
if (buf_pop(ch, &other)) { use(val); }
uint64_t bits = 0;
other = sender->resume_bits;
void unrelated(void* state, uint64_t word);
bool map_find(const Map* map, uint64_t key, uint64_t entry);
bool rt_map_contains(void* map, uint64_t key, uint64_t* previous);
uint64_t values[MAX_ARMS];
values[i] = i;
}
`
	if got := nativeWordTokens(scanCFile("runtime/native/example.c", []byte(source))); len(got) != 0 {
		t.Fatalf("numeric/disconnected aliases became carriers: %v", got)
	}
}

func nativeWordTokens(findings []rawFinding) []string {
	tokens := make([]string, 0)
	for _, finding := range findings {
		if finding.Category == categoryNativeWord && finding.Token != "key_bits" {
			tokens = append(tokens, finding.Token)
		}
	}
	slicesSort(tokens)
	return tokens
}
