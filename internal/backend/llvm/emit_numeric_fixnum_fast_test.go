package llvm

import (
	"regexp"
	"strings"
	"testing"
)

// The inline fixnum paths are read from the actual CFG, not from a count of
// instructions: a fast arm is the one successor of a tag decision that calls
// nothing and rejoins the runtime arm at a block loading their shared slot.
// Without the fast paths every row below fails at fixnumFastShape with
// "no inline fixnum arm before ...".
const fixnumFastIRSource = `
fn cast_int_u64(value: int) -> uint64 { return value to uint64; }
fn cast_int_i64(value: int) -> int64 { return value to int64; }
fn cast_uint_u64(value: uint) -> uint64 { return value to uint64; }
fn add_int(a: int, b: int) -> int { return a + b; }
fn sub_int(a: int, b: int) -> int { return a - b; }
fn less_int(a: int, b: int) -> bool { return a < b; }
fn mul_int(a: int, b: int) -> int { return a * b; }
fn add_uint(a: uint, b: uint) -> uint { return a + b; }

// The emitter writes only functions reachable from a root.
@entrypoint
fn main() -> int {
    let a: uint64 = cast_int_u64(1);
    let b: int64 = cast_int_i64(2);
    let c: uint64 = cast_uint_u64(3:uint);
    let d: int = add_int(4, 5);
    let e: int = sub_int(6, 7);
    let f: bool = less_int(8, 9);
    let g: int = mul_int(10, 11);
    let h: uint = add_uint(12:uint, 13:uint);
    return 0;
}
`

var (
	fixnumDecodeRe  = regexp.MustCompile(`^(%[\w.]+) = (ashr|lshr) i64 (%[\w.]+), 1$`)
	fixnumWordRe    = regexp.MustCompile(`^%[\w.]+ = ptrtoint ptr (%[\w.]+) to i64$`)
	fixnumAddCallRe = regexp.MustCompile(`call ptr @rt_bigint_(?:add|sub)\(ptr (%[\w.]+), ptr (%[\w.]+)\)`)
)

type fixnumShape struct {
	decision, fast, slow, join *numericIRBlock
}

func fixnumFastBodies(t *testing.T) func(string) string {
	t.Helper()
	e, result := prepareEmitterAndResultForTest(t, fixnumFastIRSource)
	ir, err := EmitModule(e.mod, e.types, e.syms, result.FileSet)
	if err != nil {
		t.Fatal(err)
	}
	return func(name string) string {
		return findLLVMFuncBody(t, ir, "fn."+itoaMIRFuncID(findMIRFunc(t, e.mod, name).ID))
	}
}

func fixnumBlockHas(b *numericIRBlock, text string) bool {
	for _, line := range b.lines {
		if strings.Contains(line, text) {
			return true
		}
	}
	return false
}

func fixnumFastShape(t *testing.T, body, slowCall string) fixnumShape {
	t.Helper()
	blocks, _ := numericIRBlocks(t, body)
	byName := map[string]*numericIRBlock{}
	for _, b := range blocks {
		byName[b.name] = b
	}
	for _, b := range blocks {
		if len(b.lines) == 0 {
			continue
		}
		branch := numericIRBranch.FindStringSubmatch(b.lines[len(b.lines)-1])
		if branch == nil {
			continue
		}
		fast, slow := byName[branch[2]], byName[branch[3]]
		if fast == nil || slow == nil || len(fast.lines) == 0 || !fixnumBlockHas(slow, slowCall) || fixnumBlockHas(fast, "call ") {
			continue
		}
		last := fast.lines[len(fast.lines)-1]
		join := byName[strings.TrimPrefix(last, "br label %")]
		if !strings.HasPrefix(last, "br label %") || join == nil || len(join.preds) != 2 || !join.preds[fast.name] {
			continue
		}
		return fixnumShape{decision: b, fast: fast, slow: slow, join: join}
	}
	t.Fatalf("no inline fixnum arm before %s:\n%s", slowCall, body)
	return fixnumShape{}
}

func requireFixnumEntryAllocas(t *testing.T, body string) {
	t.Helper()
	blocks, _ := numericIRBlocks(t, body)
	for _, b := range blocks[1:] {
		if fixnumBlockHas(b, " = alloca ") {
			t.Fatalf("block %s reserves storage outside the entry block:\n%s", b.name, body)
		}
	}
}

func TestEmitFixnumCastFastPathKeepsHeapArm(t *testing.T) {
	bodyFor := fixnumFastBodies(t)
	for _, row := range []struct {
		name, slowCall, shift string
		signGate              bool
	}{
		{"cast_int_u64", "@rt_bigint_to_biguint(", "ashr", true},
		{"cast_int_i64", "@rt_bigint_to_i64(", "ashr", false},
		{"cast_uint_u64", "@rt_biguint_to_u64(", "lshr", false},
	} {
		t.Run(row.name, func(t *testing.T) {
			body := bodyFor(row.name)
			shape := fixnumFastShape(t, body, row.slowCall)
			decoded := ""
			for _, line := range shape.decision.lines {
				if m := fixnumDecodeRe.FindStringSubmatch(line); m != nil && m[2] == row.shift {
					decoded = m[1]
				}
			}
			if decoded == "" || !fixnumBlockHas(shape.fast, "store i64 "+decoded+",") {
				t.Fatalf("fast arm must store the %s-decoded payload:\n%s", row.shift, body)
			}
			if fixnumBlockHas(shape.decision, "icmp sge i64 "+decoded+", 0") != row.signGate {
				t.Fatalf("sign gate before the fast arm = %v, want %v:\n%s", !row.signGate, row.signGate, body)
			}
			if strings.Count(body, row.slowCall) != 1 {
				t.Fatalf("the runtime conversion must stay exactly once, on the slow arm:\n%s", body)
			}
			if !strings.Contains(shape.join.lines[0], " = load i64, ptr ") {
				t.Fatalf("join must load the slot both arms store:\n%s", body)
			}
			requireFixnumEntryAllocas(t, body)
		})
	}
}

func TestEmitFixnumArithFallsBackOnOverflowAndHeap(t *testing.T) {
	bodyFor := fixnumFastBodies(t)
	for _, row := range []struct{ name, opcode, slowCall string }{
		{"add_int", "add", "call ptr @rt_bigint_add("},
		{"sub_int", "sub", "call ptr @rt_bigint_sub("},
	} {
		t.Run(row.name, func(t *testing.T) {
			body := bodyFor(row.name)
			shape := fixnumFastShape(t, body, row.slowCall)
			for _, want := range []string{" = " + row.opcode + " i64 ", " = shl i64 ", " = ashr i64 "} {
				if !fixnumBlockHas(shape.decision, want) {
					t.Fatalf("decision lacks %q, the exact result and its inline round trip:\n%s", want, body)
				}
			}
			for _, want := range []string{", i64 0, i64 ", " = inttoptr i64 ", "store ptr "} {
				if !fixnumBlockHas(shape.fast, want) {
					t.Fatalf("fast arm lacks %q -- zero must box as NULL and the word must be stored:\n%s", want, body)
				}
			}
			var words []string
			for _, line := range shape.decision.lines {
				if m := fixnumWordRe.FindStringSubmatch(line); m != nil {
					words = append(words, m[1])
				}
			}
			call := fixnumAddCallRe.FindStringSubmatch(strings.Join(shape.slow.lines, "\n"))
			if len(words) != 2 || call == nil || call[1] != words[0] || call[2] != words[1] {
				t.Fatalf("slow arm must call the runtime with the original operands %v, got %v:\n%s", words, call, body)
			}
			requireFixnumEntryAllocas(t, body)
		})
	}
	for _, row := range []struct{ name, call string }{
		{"mul_int", "call ptr @rt_bigint_mul("},
		{"add_uint", "call ptr @rt_biguint_add("},
	} {
		t.Run(row.name+"_has_no_fast_path", func(t *testing.T) {
			body := bodyFor(row.name)
			if strings.Count(body, row.call) != 1 || strings.Contains(body, " = shl i64 ") || strings.Contains(body, " = inttoptr i64 ") {
				t.Fatalf("%s acquired an inline arm it has no proof for:\n%s", row.name, body)
			}
		})
	}
}

func TestEmitFixnumCompareFastPath(t *testing.T) {
	body := fixnumFastBodies(t)("less_int")
	shape := fixnumFastShape(t, body, "call i32 @rt_bigint_cmp(")
	if !fixnumBlockHas(shape.fast, " = icmp slt i64 ") || !fixnumBlockHas(shape.fast, "store i1 ") {
		t.Fatalf("fast arm must compare the decoded payloads:\n%s", body)
	}
	if !fixnumBlockHas(shape.slow, " = icmp slt i32 ") {
		t.Fatalf("slow arm must keep the runtime three-way predicate:\n%s", body)
	}
	if !strings.Contains(shape.join.lines[0], " = load i1, ptr ") {
		t.Fatalf("join must load the shared answer:\n%s", body)
	}
	requireFixnumEntryAllocas(t, body)
}
