package carriergate

import (
	"reflect"
	"strings"
	"testing"
)

// The two shapes the scanner was blind to while the ratchet stood green over
// them: a blocking job's captures described by two integers instead of a type,
// and a suspension frame whose storage compiled code reserves and releases.
//
// Every token is asserted twice over — once where the source really does the
// thing, and once where a comment or a literal only says its name — because a
// scanner that counted the second would go green the day someone documented
// the carrier instead of the day someone removed it.
const (
	frameOwnerGoFixture = `package llvm

// emitRuntimeOwnedStorage, requireSuspensionFrameRelease,
// emitSuspensionFrameReleaseBody, emitAsyncStateFreeIntrinsic and
// mir.AsyncStateFreeBuiltin named here are a comment, not a frame.
func emitRuntimeOwnedStorage(id int) int { return id }

func emitValueStorage(id int) int { return emitRuntimeOwnedStorage(id) }

func requireSuspensionFrameRelease(id int) string { return "a release symbol" }

func emitSuspensionFrameReleaseBody(id int) error { return nil }

func emitAsyncStateFreeIntrinsic(call int) bool {
	return call == mir.AsyncStateFreeBuiltin
}

var _ = "emitSuspensionFrameReleaseBody spelled inside an IR string"
`

	frameOwnerCFixture = `/* state_size state_align abandoned_state abandoned_state_type_id */
// state_size and abandoned_state in a line comment describe nothing
const char* note = "state_align abandoned_state_type_id";
void* rt_blocking_submit(uint64_t fn_id, void* state, uint64_t state_size, uint64_t state_align);
static void release(rt_blocking_job* job) {
    rt_free((uint8_t*)job->state, job->state_size, job->state_align);
}
uint64_t my_state_size;
void* abandoned_state;
uint64_t abandoned_state_type_id;
uint64_t abandoned_state_drop_fn_id;
static void stash_abandoned_state(void* state) { current->abandoned_state = state; }
`
)

func TestScanSeesUntypedCapturesAndACompiledCodeOwnedFrame(t *testing.T) {
	root := buildFixtureTree(t, map[string][]byte{
		"internal/backend/llvm/frame.go": []byte(frameOwnerGoFixture),
		// The same identifier in the interpreter lane is a different engine's
		// frame and a different migration; this category does not claim it.
		"internal/vm/frame.go":      []byte("package vm\n\nvar _ = mir.AsyncStateFreeBuiltin\n"),
		"runtime/native/frame.c":    []byte(frameOwnerCFixture),
		"internal/vm/frame_test.go": []byte("package vm\n\nvar _ = mir.AsyncStateFreeBuiltin\n"),
	}, false)
	findings, err := Scan(root)
	if err != nil {
		t.Fatalf("scan fixture: %v", err)
	}
	want := map[string]int{
		categoryFrameOwner + ":emitRuntimeOwnedStorage":        2,
		categoryFrameOwner + ":requireSuspensionFrameRelease":  1,
		categoryFrameOwner + ":emitSuspensionFrameReleaseBody": 1,
		categoryFrameOwner + ":emitAsyncStateFreeIntrinsic":    1,
		categoryFrameOwner + ":AsyncStateFreeBuiltin":          1,
		categoryFrameOwner + ":abandoned_state":                2,
		categoryFrameOwner + ":abandoned_state_type_id":        1,
		categoryUntypedCaptureState + ":state_size":            2,
		categoryUntypedCaptureState + ":state_align":           2,
		categoryNumericDrop + ":abandoned_state_drop_fn_id":    1,
	}
	got := make(map[string]int)
	for _, finding := range findings {
		got[finding.Category+":"+finding.Token]++
		if strings.Contains(finding.Evidence, "//") || strings.Contains(finding.Evidence, "/*") {
			t.Fatalf("comment became a finding: %+v", finding)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tokens = %#v, want %#v", got, want)
	}
}

// A carrier name is a whole identifier. `my_state_size` is a different field
// and `stash_abandoned_state` is a different function; counting either would
// put a row in the census that no migration can ever retire.
func TestScanRefusesCarrierNamesInsideLongerIdentifiers(t *testing.T) {
	source := `uint64_t my_state_size;
uint64_t job_state_align;
static void stash_abandoned_state(void* state) { (void)state; }
uint64_t abandoned_state_drop_fn_id;
`
	for _, finding := range scanCFile("runtime/native/example.c", []byte(source)) {
		if finding.Category == categoryFrameOwner || finding.Category == categoryUntypedCaptureState {
			t.Fatalf("a longer identifier became a carrier: %+v", finding)
		}
	}
}

// The frame's numeric drop id and the frame itself are two different carriers
// with two different owners, so they must not be counted into one another.
func TestFrameOwnerAndNumericDropStayDistinct(t *testing.T) {
	source := "uint64_t abandoned_state_drop_fn_id;\nvoid* abandoned_state;\n"
	categories := make(map[string]string)
	for _, finding := range scanCFile("runtime/native/example.c", []byte(source)) {
		categories[finding.Token] = finding.Category
	}
	want := map[string]string{
		"abandoned_state_drop_fn_id": categoryNumericDrop,
		"abandoned_state":            categoryFrameOwner,
	}
	if !reflect.DeepEqual(categories, want) {
		t.Fatalf("categories = %#v, want %#v", categories, want)
	}
}
