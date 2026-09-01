package llvm

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"surge/internal/mir"
	"surge/internal/types"
)

// bareMemberProgram assigns a heap string into a union that admits it as a BARE
// type member — the shape whose contents used to leak, measured at 58 bytes.
const bareMemberProgram = `
tag ResOk<T>(T);
type Result<T, E> = ResOk(T) | E;

@entrypoint
fn main() -> int {
    let s = "this string is owned by the union member" + "!";
    let r: Result<int, string> = s;
    return 0;
}
`

// TestUnionMembershipMatchesTheLayoutEnumeration is the invariant the whole
// migration rests on: the canonical membership and the physical case list are
// two views of ONE enumeration, so an index from either means the same variant.
//
// Before the membership existed, the only list available was the flattened tag
// view, whose index means neither — it can be longer than the physical list and
// it is not injective.
func TestUnionMembershipMatchesTheLayoutEnumeration(t *testing.T) {
	mirMod, result := lowerMIRFromSource(t, bareMemberProgram)
	if mirMod.Meta == nil || len(mirMod.Meta.UnionCases) == 0 {
		t.Fatal("the lowering published no union membership")
	}
	e := &Emitter{mod: mirMod, types: result.Sema.TypeInterner, syms: result.Symbols.Table}

	checkedMixed := false
	for id, cases := range mirMod.Meta.UnionCases {
		facts, err := e.layoutOf(id)
		if err != nil {
			continue
		}
		if got, want := len(facts.UnionCases()), len(cases); got != want {
			t.Errorf("type#%d: %d physical cases against %d members — the enumerations disagree", id, got, want)
			continue
		}
		bare := 0
		for index := range cases {
			c := &cases[index]
			if c.PhysicalCaseIndex != index {
				t.Errorf("type#%d: member %d carries physical index %d", id, index, c.PhysicalCaseIndex)
			}
			if _, ok := facts.UnionCase(c.PhysicalCaseIndex); !ok {
				t.Errorf("type#%d: the layout has no case %d for member %q", id, c.PhysicalCaseIndex, c.Name)
			}
			if c.Kind == mir.UnionCaseBareType {
				bare++
				if c.BareType == types.NoTypeID {
					t.Errorf("type#%d: bare member %q admits no type", id, c.Name)
				}
			}
		}
		// The union under test mixes a tag with a bare member; if the probe
		// stopped producing one, this test would pass while asserting nothing.
		if bare > 0 && len(cases) > bare {
			checkedMixed = true
		}
	}
	if !checkedMixed {
		t.Fatal("no mixed union was examined, so this asserted nothing")
	}
}

// TestBareMemberIsMaterialisedWithItsDiscriminant reads the emitted IR.
//
// A value assigned into a union through a bare member used to pass through
// untouched: no discriminant was written, and the destination's spelling won, so
// the union's whole size was copied out of the member's handle. The drop glue
// then found a tag word holding whatever the pointee's first four bytes were.
func TestBareMemberIsMaterialisedWithItsDiscriminant(t *testing.T) {
	ir := emitLLVMFromSource(t, bareMemberProgram)

	// Find the union's own member index rather than hard-coding 1: the point is
	// that the STORED value is the direct-member index, not that it is any
	// particular number.
	mirMod, result := lowerMIRFromSource(t, bareMemberProgram)
	wantIndex := -1
	wantSize := uint64(0)
	wantAlign := uint64(0)
	e := &Emitter{mod: mirMod, types: result.Sema.TypeInterner}
	for id, cases := range mirMod.Meta.UnionCases {
		for index := range cases {
			if cases[index].Kind == mir.UnionCaseBareType {
				wantIndex = cases[index].PhysicalCaseIndex
				facts, err := e.layoutOf(id)
				if err != nil {
					t.Fatalf("resolve mixed-union layout: %v", err)
				}
				wantSize, wantAlign = facts.Size, facts.Align
			}
		}
	}
	if wantIndex < 0 {
		t.Fatal("the probe carries no bare member")
	}

	pattern := regexp.MustCompile(`store i32 ` + itoa(wantIndex) + `, ptr (%l\d+)`)
	stored := pattern.FindStringSubmatch(ir)
	if len(stored) != 2 {
		t.Errorf("no discriminant %d is stored for the bare member; the union is not materialised:\n%s",
			wantIndex, extractMain(ir))
		return
	}
	zero := "call void @llvm.memset.p0.i64(ptr align " + itoa(int(wantAlign)) + " " + stored[1] +
		", i8 0, i64 " + itoa(int(wantSize)) + ", i1 false)"
	zeroAt := strings.Index(ir, zero)
	tagAt := strings.Index(ir, stored[0])
	if zeroAt < 0 || zeroAt > tagAt {
		t.Errorf("the %d-byte union destination was not deterministically initialized before case %d:\n%s",
			wantSize, wantIndex, extractMain(ir))
	}
}

func TestEveryUnionDiscriminantStartsFromDeterministicStorage(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read LLVM emitter directory: %v", err)
	}
	const directDiscriminant = `store i32 %d, ptr %s`
	var findings []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		content, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for range strings.Count(string(content), directDiscriminant) {
			findings = append(findings, name)
		}
	}
	if len(findings) != 1 {
		t.Fatalf("direct union-discriminant emitters = %v, want one shared deterministic initializer", findings)
	}
}

// TestUnionDropWalksBareMembers pins the drop side.
//
// The glue used to be built from the flattened tag view, in which a bare member
// has no entry at all — so its contents were never released, and the leak was
// silent because the program still exited 0.
func TestUnionDropWalksBareMembers(t *testing.T) {
	ir := emitLLVMFromSource(t, bareMemberProgram)
	mirMod, _ := lowerMIRFromSource(t, bareMemberProgram)

	unionType := types.NoTypeID
	bareIndex := -1
	for id, cases := range mirMod.Meta.UnionCases {
		for index := range cases {
			if cases[index].Kind == mir.UnionCaseBareType && len(cases) > 1 {
				unionType, bareIndex = id, cases[index].PhysicalCaseIndex
			}
		}
	}
	if bareIndex < 0 {
		t.Fatal("the probe carries no mixed union")
	}

	marker := "define void @drop.type" + itoa(int(unionType)) + "(ptr %p) {"
	start := strings.Index(ir, marker)
	if start < 0 {
		t.Fatalf("no drop glue was generated for the union that owns a string:\n%s", extractMain(ir))
	}
	end := strings.Index(ir[start:], "\n}\n")
	body := ir[start : start+end]

	// The switch must have an arm for the bare member's own index.
	if !strings.Contains(body, "i32 "+itoa(bareIndex)+", label") {
		t.Errorf("the drop switch has no arm for bare member %d, so what it owns is never released:\n%s",
			bareIndex, body)
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var digits []byte
	for v > 0 {
		digits = append([]byte{byte('0' + v%10)}, digits...)
		v /= 10
	}
	if neg {
		return "-" + string(digits)
	}
	return string(digits)
}

// extractMain returns the entry function's body for a readable failure message.
func extractMain(ir string) string {
	start := strings.Index(ir, "define ptr @fn.")
	if start < 0 {
		return ir
	}
	end := strings.Index(ir[start:], "\n}\n")
	if end < 0 {
		return ir[start:]
	}
	return ir[start : start+end]
}
