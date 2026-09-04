package llvm

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"surge/internal/sema"
)

// A far select's SEND arm ships its payload under the ELEMENT type of the far
// channel it sends into, and the recv arm under 0 (it stages nothing).
//
// Until 2026-09-04 the send arm shipped 0 as well: channelElemType answered
// NoTypeID for `far Channel<T>` because resolveValueType stops at the far
// qualifier, so the runtime built the arm's cell over the opaque-word
// descriptor, whose drop is a no-op. The committing path moves through the
// channel's own descriptor and never noticed; a staged non-copy payload the
// select never committed -- the caller cancelled while the select was parked
// -- was never destroyed (RV2-DEBT-332, found by the Epic 21 Task 9 matrix at
// two and eight shards). The mutant is the old unwrap back: the send arm's
// stored id reads 0 and this test names it.
func TestEmitFarSelectSendArmNamesTheChannelsElementType(t *testing.T) {
	sourceCode := `
async fn chooser(a: far Channel<string>, b: far Channel<int>) -> int {
    let mut job = "job-";
    job = job + "payload";
    let winner: int = select { a.send(own job) => 1; b.recv() => 2; };
    return winner;
}
`
	mod, result := lowerCrossingMIRFromSource(t, sourceCode, sema.CrossingLoweringChannelSelect)
	ir, err := EmitModule(mod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	body := findLLVMFuncBody(t, ir, "fn."+itoaMIRFuncID(findMIRFunc(t, mod, "chooser$poll").ID))

	// The arm type-id table is a [2 x i64] whose slots are filled by a GEP
	// and, a line or two later (the arm's value slot is stored in between),
	// the store into that same temp; read the two stores by arm.
	gep := regexp.MustCompile(`^\s*(%t\d+) = getelementptr inbounds \[2 x i64\], ptr %t\d+, i64 0, i64 (\d)$`)
	store := regexp.MustCompile(`^\s*store i64 (\d+), ptr (%t\d+)$`)
	lines := strings.Split(body, "\n")
	typeIDs := map[string]string{}
	for i, line := range lines {
		g := gep.FindStringSubmatch(line)
		if g == nil {
			continue
		}
		for j := i + 1; j < len(lines) && j <= i+4; j++ {
			s := store.FindStringSubmatch(lines[j])
			if s != nil && s[2] == g[1] {
				typeIDs[g[2]] = s[1]
				break
			}
		}
	}
	sendID, ok := typeIDs["0"]
	if !ok {
		t.Fatalf("send arm's type id store missing from the poll body:\n%s", body)
	}
	id, convErr := strconv.ParseUint(sendID, 10, 64)
	if convErr != nil || id == 0 {
		t.Fatalf("far select send arm shipped its payload as type id %q, want the channel's element (nonzero):\n%s", sendID, body)
	}
	lookup := findLLVMFuncBody(t, ir, "__surge_value_ops_for")
	if !strings.Contains(lookup, "i64 "+sendID+", label %value_ops."+sendID) {
		t.Fatalf("send arm type id %d has no descriptor in __surge_value_ops_for:\n%s", id, lookup)
	}
	if recvID, ok := typeIDs["1"]; !ok || recvID != "0" {
		t.Fatalf("recv arm must ship type id 0 (it stages nothing), got %q:\n%s", recvID, body)
	}
}
