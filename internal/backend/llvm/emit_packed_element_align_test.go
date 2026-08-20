package llvm

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// A fixed array's elements are only as aligned as the array itself is, and this
// file is the only place that can say so.
//
// The defect it pins is invisible everywhere else. A `@packed` container puts a
// wide fixed array at an offset its element type does not divide, so every
// element address is odd; an access there that claimed the ELEMENT TYPE's
// alignment promised the hardware something the address never keeps. Both lanes
// still exit 0 — x86 tolerates the unaligned access, so a behavioural fixture
// sees agreement and prints the right answer while the undefined behaviour
// stays. The claim only exists in the emitted IR, so that is where it is read.

// alignmentWitness replays what an emitted function's pointers are aligned to,
// following only the chains it can see the whole of.
//
// A pointer's alignment is known when the emission RESERVED the storage
// (`alloca`, which always carries its alignment here) and stays known across
// address arithmetic: a constant byte offset folds the way a field projection
// does, and a `mul`-by-stride offset folds the way an index does, because one
// claim has to hold for every index at once.
//
// A pointer that arrives any other way — loaded out of memory, handed in as a
// parameter, returned by the runtime — is left UNKNOWN and every access through
// it is skipped. That is what keeps the checker sound in the direction that
// matters: it never invents a guarantee, so a complaint is always a real
// over-claim rather than a gap in the replay.
type alignmentWitness struct {
	fn      string
	ptr     map[string]uint64
	stride  map[string]uint64
	checked int
	narrow  int
}

var (
	irDefine     = regexp.MustCompile(`^define\s.*@([A-Za-z0-9_.$]+)\(`)
	irAlloca     = regexp.MustCompile(`^\s*%(\S+) = alloca .*, align (\d+)\s*$`)
	irGepConst   = regexp.MustCompile(`^\s*%(\S+) = getelementptr inbounds i8, ptr %(\S+), i64 (\d+)\s*$`)
	irGepStrided = regexp.MustCompile(`^\s*%(\S+) = getelementptr inbounds i8, ptr %(\S+), i64 %(\S+)\s*$`)
	irStrideMul  = regexp.MustCompile(`^\s*%(\S+) = mul i64 \S+, (\d+)\s*$`)
	irStore      = regexp.MustCompile(`^\s*store (\S+) [^,]+, ptr %(\S+), align (\d+)\s*$`)
	irLoad       = regexp.MustCompile(`^\s*%\S+ = load (\S+), ptr %(\S+), align (\d+)\s*$`)
	irBareMemOp  = regexp.MustCompile(`^\s*(?:store \S+ [^,]+, ptr %\S+|%\S+ = load \S+, ptr %\S+)\s*$`)
)

// overclaims reports every memory operation whose stated alignment is larger
// than the address it goes through can guarantee, and counts how many operations
// were checked at all.
func overclaims(ir string) (offences []string, checked, narrowed int) {
	w := &alignmentWitness{ptr: map[string]uint64{}, stride: map[string]uint64{}}
	for i, line := range strings.Split(ir, "\n") {
		lineNo := i + 1
		switch {
		case irDefine.MatchString(line):
			m := irDefine.FindStringSubmatch(line)
			// SSA names are function-local, so the replay restarts with the
			// function. Carrying a name across would attach one function's
			// guarantee to another function's pointer.
			w = &alignmentWitness{fn: m[1], ptr: map[string]uint64{}, stride: map[string]uint64{}, checked: w.checked, narrow: w.narrow}
		case irAlloca.MatchString(line):
			m := irAlloca.FindStringSubmatch(line)
			w.ptr[m[1]] = mustUint(m[2])
		case irStrideMul.MatchString(line):
			m := irStrideMul.FindStringSubmatch(line)
			w.stride[m[1]] = mustUint(m[2])
		case irGepConst.MatchString(line):
			m := irGepConst.FindStringSubmatch(line)
			if base, ok := w.ptr[m[2]]; ok {
				w.ptr[m[1]] = memberAccessAlign(base, mustUint(m[3]))
			}
		case irGepStrided.MatchString(line):
			m := irGepStrided.FindStringSubmatch(line)
			base, baseKnown := w.ptr[m[2]]
			stride, strideKnown := w.stride[m[3]]
			if baseKnown && strideKnown {
				w.ptr[m[1]] = memberAccessAlign(base, stride)
			}
		case irBareMemOp.MatchString(line):
			// An operation with no alignment operand at all promises the
			// type's PREFERRED alignment, which is the same over-claim made by
			// omission. Only addresses this replay can follow are judged; the
			// runtime-word paths that state nothing are a separate rule with a
			// gate of its own (TestOrdinaryStorageEmittersUseTheAlignedHelpers).
			if target, ok := bareMemOperandTarget(line); ok {
				if have, known := w.ptr[target]; known && have < alignWord {
					offences = append(offences, fmt.Sprintf(
						"%s:%d states no alignment at all through an address that guarantees %d: %s",
						w.fn, lineNo, have, strings.TrimSpace(line)))
				}
			}
		default:
			opTy, target, claim, ok := memOperand(line)
			if !ok {
				continue
			}
			have, known := w.ptr[target]
			if !known {
				continue
			}
			w.checked++
			if claim > have {
				offences = append(offences, fmt.Sprintf(
					"%s:%d claims align %d through an address that guarantees %d: %s",
					w.fn, lineNo, claim, have, strings.TrimSpace(line)))
				continue
			}
			// An access narrowed below what its own type would take is the
			// evidence that a container's placement reached it.
			if natural, err := naturalAlign(opTy); err == nil && claim < natural {
				w.narrow++
			}
		}
	}
	return offences, w.checked, w.narrow
}

var irBareMemTarget = regexp.MustCompile(`ptr %(\S+)\s*$`)

func bareMemOperandTarget(line string) (string, bool) {
	m := irBareMemTarget.FindStringSubmatch(line)
	if m == nil {
		return "", false
	}
	return m[1], true
}

func memOperand(line string) (opTy, target string, claim uint64, ok bool) {
	if m := irStore.FindStringSubmatch(line); m != nil {
		return m[1], m[2], mustUint(m[3]), true
	}
	if m := irLoad.FindStringSubmatch(line); m != nil {
		return m[1], m[2], mustUint(m[3]), true
	}
	return "", "", 0, false
}

func mustUint(s string) uint64 {
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// Every shape below reaches a fixed array's elements through a `@packed`
// container by a DIFFERENT emitter, which is the point: the alignment used to
// be re-derived from the element type once per emitter, so a fixture that only
// wrote one way proved only one of them.
var packedFixedArrayAccesses = []struct {
	name   string
	source string
}{
	{
		// The write and the read of a wide element: `p.cells[1] = v` lowers to
		// the `__index_set` intrinsic, and reading it back lowers to `__index`.
		// This is the exact program RV2-DEBT-226 quotes.
		name: "wide element written and read",
		source: `
@packed type Packed = { flag: bool, cells: int64[3], tail: bool };

@entrypoint
fn main() -> int {
    let mut p = Packed { flag = true, cells = [10:int64, 20:int64, 30:int64], tail = true };
    p.cells[1] = 55:int64;
    if p.cells[1] != 55:int64 { return 1; }
    if p.cells[0] != 10:int64 || p.cells[2] != 30:int64 { return 2; }
    return 0;
}
`,
	},
	{
		// A run-time index, so no arm of the emitter can be right by folding a
		// constant: the claim has to hold for every index the loop reaches.
		name: "run-time index",
		source: `
@packed type Packed = { flag: bool, cells: int64[3], tail: bool };

@entrypoint
fn main() -> int {
    let mut p = Packed { flag = true, cells = [10:int64, 20:int64, 30:int64], tail = true };
    let mut i: int = 0;
    while i < 3 {
        p.cells[i] = 7:int64;
        i = i + 1;
    }
    return 0;
}
`,
	},
	{
		// A further projection after the index, which is what sends the write
		// through the PLACE WALK rather than through `__index_set`. The walk
		// folds an alignment through field projections already; it used to
		// throw that answer away and re-derive at the index.
		name: "field of a struct element",
		source: `
type Cell = { a: int64, b: int64 };
@packed type Outer = { pad: bool, cells: Cell[2], tail: bool };

@entrypoint
fn main() -> int {
    let mut o = Outer { pad = true, cells = [Cell{a=1:int64,b=1:int64}, Cell{a=2:int64,b=2:int64}], tail = true };
    o.cells[1].b = 9:int64;
    if o.cells[1].b != 9:int64 { return 1; }
    return 0;
}
`,
	},
}

// Reaching the elements through a BORROW of the container is deliberately not
// in the table. The element address is minted correctly there, then spilled
// into the borrow's slot and loaded back, so the access happens through a
// pointer this replay cannot follow — and the alignment it claims comes from
// the borrow's pointee TYPE, not from the address. Adding the case would put a
// row here that passes because nothing looked at it. What an access through a
// borrow of a packed member may claim is an open question, not something this
// gate settles.

// No access in an emitted module may claim more alignment than the address it
// goes through can guarantee.
//
// This is stated as one rule over every memory operation rather than as a
// pattern match on the store RV2-DEBT-226 quotes, because the defect was never
// one store: the same alignment was re-derived from the element type at every
// site that reaches an element, and a test that pinned one spelling would go
// green the moment a program took a different emitter to the same address.
func TestNoAccessClaimsMoreAlignmentThanItsAddressHas(t *testing.T) {
	for _, tc := range packedFixedArrayAccesses {
		t.Run(tc.name, func(t *testing.T) {
			ir := emitLLVMFromSource(t, tc.source)
			offences, checked, narrowed := overclaims(ir)
			if len(offences) > 0 {
				t.Errorf("%d over-claimed access(es):\n%s", len(offences), strings.Join(offences, "\n"))
			}
			// Without this the test would pass on a module it never managed to
			// follow: an emitter reshaped so no address chain stays visible
			// would report zero offences and zero coverage alike.
			if checked == 0 {
				t.Fatalf("followed no address to any access; the checker proved nothing")
			}
			if narrowed == 0 {
				t.Fatalf(
					"followed %d access(es) but none was narrowed below its own type's alignment, "+
						"so none of them reached the packed container's elements",
					checked)
			}
		})
	}
}

// The write RV2-DEBT-226 quotes, read straight out of the IR.
//
// The rule above is the durable assertion; this one is the witness that the
// rule is aimed at the right instruction. `p.cells[1] = 55:int64` inside a
// `@packed` container must reach `base+1 + i*8` and store there at align 1 —
// the row recorded `store i64 …, align 8` at that address, which x86 tolerates
// and LLVM is entitled not to.
func TestPackedWideElementWriteStoresAtAlignOne(t *testing.T) {
	ir := emitLLVMFromSource(t, packedFixedArrayAccesses[0].source)

	packedMember := map[string]bool{}
	wideStride := map[string]bool{}
	elemAddr := map[string]bool{}
	claims := map[string]string{}

	for _, line := range strings.Split(ir, "\n") {
		if m := irGepConst.FindStringSubmatch(line); m != nil {
			if m[3] == "1" {
				// The array sits one byte into the container, which is the
				// placement that makes every element address odd.
				packedMember[m[1]] = true
			}
			continue
		}
		if m := irStrideMul.FindStringSubmatch(line); m != nil {
			if m[2] == "8" {
				wideStride[m[1]] = true
			}
			continue
		}
		if m := irGepStrided.FindStringSubmatch(line); m != nil {
			if packedMember[m[2]] && wideStride[m[3]] {
				elemAddr[m[1]] = true
			}
			continue
		}
		if m := irStore.FindStringSubmatch(line); m != nil && m[1] == "i64" {
			if elemAddr[m[2]] {
				claims[m[2]] = m[3]
			}
		}
	}

	if len(elemAddr) == 0 {
		t.Fatalf("no wide element address off a member at offset 1: the fixture no longer reaches one\n%s", ir)
	}
	if len(claims) == 0 {
		t.Fatalf("found %d element address(es) but no i64 store through any: the fixture no longer writes one", len(elemAddr))
	}
	for addr, claim := range claims {
		if claim != "1" {
			t.Errorf(
				"the write through %%%s claims align %s at an address congruent to 1 mod 8 "+
					"(container member at offset 1, element at +i*8)", addr, claim)
		}
	}
}

// The alignment a fixed array's elements may claim has to hold for EVERY index,
// not for the one an emitter happened to fold.
//
// This is the arithmetic the whole fix rests on: element i sits at
// base + i*stride, so the guarantee shared by all of them is the largest power
// of two dividing the stride, capped by the base — and capped again by what the
// element type itself needs, so the answer is only ever a narrowing of what
// used to be claimed. Checking it against every index rather than against a
// formula is what would catch a fold that is right for index 0 and wrong after.
func TestInlineElementAlignmentHoldsForEveryIndex(t *testing.T) {
	for _, tc := range []struct {
		baseAlign uint64
		stride    uint64
		natural   uint64
		want      uint64
	}{
		// A wide element in a packed container: the array lands at an odd
		// offset, so no index is 8-aligned, not even index 0.
		{baseAlign: 1, stride: 8, natural: 8, want: 1},
		// The same array in an ordinary container keeps the type's alignment.
		{baseAlign: 8, stride: 8, natural: 8, want: 8},
		// A narrow element never claimed more than 1 to begin with, which is
		// why the pre-existing packed fixture could not see the defect: the
		// wrong answer and the right answer coincide.
		{baseAlign: 1, stride: 1, natural: 1, want: 1},
		// A stride wider than the base guarantees is bounded by the base.
		{baseAlign: 2, stride: 16, natural: 8, want: 2},
		// A stride narrower than the base bounds the base.
		{baseAlign: 8, stride: 4, natural: 4, want: 4},
		// The element's own need caps it from the other side, so the claim is
		// never widened past what the type asks for.
		{baseAlign: 8, stride: 16, natural: 8, want: 8},
	} {
		got := memberAccessAlign(tc.baseAlign, tc.stride)
		if tc.natural < got {
			got = tc.natural
		}
		if got != tc.want {
			t.Errorf("base %d, stride %d, natural %d: folded to %d, want %d",
				tc.baseAlign, tc.stride, tc.natural, got, tc.want)
		}
		// Every index, not just the fold.
		for i := range uint64(8) {
			addr := i * tc.stride
			actual := tc.baseAlign
			if addr != 0 {
				lowBit := addr & (^addr + 1)
				if lowBit < actual {
					actual = lowBit
				}
			}
			if got > actual {
				t.Errorf("base %d, stride %d: claimed %d but index %d sits at +%d, which is only %d-aligned",
					tc.baseAlign, tc.stride, got, i, addr, actual)
			}
		}
	}
}
