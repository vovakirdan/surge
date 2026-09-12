package llvm

import (
	"regexp"
	"strings"
	"testing"
)

const numericCastIRSource = `
fn cast_f64_int(value: float64) -> int { return value to int; }
fn cast_f64_uint(value: float64) -> uint { return value to uint; }
fn cast_float_i64(value: float) -> int64 { return value to int64; }
fn cast_float_u64(value: float) -> uint64 { return value to uint64; }
fn cast_int_u64(value: int) -> uint64 { return value to uint64; }
fn cast_int_f64(value: int) -> float64 { return value to float64; }
fn cast_uint_f64(value: uint) -> float64 { return value to float64; }
fn borrow_int_i64(value: int) -> int64 { return value to int64; }
fn borrow_uint_u64(value: uint) -> uint64 { return value to uint64; }
fn borrow_float_f64(value: float) -> float64 { return value to float64; }
fn fixed_i64_u64(value: int64) -> uint64 { return value to uint64; }
fn fixed_f64_i64(value: float64) -> int64 { return value to int64; }
`

func numericCastBodies(t *testing.T) func(string) string {
	t.Helper()
	e, result := prepareEmitterAndResultForTest(t, numericCastIRSource)
	ir, err := EmitModule(e.mod, e.types, e.syms, result.FileSet)
	if err != nil {
		t.Fatal(err)
	}
	return func(name string) string {
		return findLLVMFuncBody(t, ir, "fn."+itoaMIRFuncID(findMIRFunc(t, e.mod, name).ID))
	}
}

func TestEmitNumericCastTemporaryCleanup(t *testing.T) {
	bodyFor := numericCastBodies(t)
	for _, row := range []struct{ name, producer, reader, kind string }{
		{"cast_f64_int", "rt_bigfloat_from_f64", "rt_bigfloat_to_bigint", "float"},
		{"cast_f64_uint", "rt_bigfloat_from_f64", "rt_bigfloat_to_biguint", "float"},
		{"cast_float_i64", "rt_bigfloat_to_bigint", "rt_bigint_to_i64", "int"},
		{"cast_float_u64", "rt_bigfloat_to_biguint", "rt_biguint_to_u64", "uint"},
		{"cast_int_u64", "rt_bigint_to_biguint", "rt_biguint_to_u64", "uint"},
		{"cast_int_f64", "rt_bigint_to_bigfloat", "rt_bigfloat_to_f64", "float"},
		{"cast_uint_f64", "rt_biguint_to_bigfloat", "rt_bigfloat_to_f64", "float"},
	} {
		t.Run(row.name, func(t *testing.T) {
			body := bodyFor(row.name)
			blocks, defs := numericIRBlocks(t, body)
			made := numericCastSingleMatch(t, body, `(%[\w.]+) = call ptr @`+row.producer+`\([^\n]+\)`)
			word := made[1]
			read := numericCastSingleMatch(t, body, `(%[\w.]+) = call (ptr|i1) @`+row.reader+`\(ptr `+word+`(?:, ptr (%[\w.]+))?\)`)
			release := "call void @rt_big" + row.kind + "_release(ptr " + word + ")"
			if strings.Count(body, release) != 1 {
				t.Fatalf("temporary %s must have exactly one release, body:\n%s", word, body)
			}
			if defs[word].block != defs[read[1]].block || strings.Index(body, made[0]) >= strings.Index(body, read[0]) ||
				strings.Index(body, read[0]) >= strings.Index(body, release) {
				t.Fatal("temporary must be constructed, read, then released in that order")
			}
			if read[2] == "ptr" {
				// The returned integer is a different owner; only the temporary
				// float disappears here. Later MIR return transfer uses the result.
				b := numericCastBlock(t, blocks, defs[read[1]].block)
				if !numericCastHasLine(b, release) {
					t.Fatal("float temporary cleanup left the conversion continuation")
				}
				return
			}
			numericCastCheckedCleanup(t, blocks, defs, word, read[1], read[3], release, row.kind)
		})
	}
}

// Read the actual CFG: the owned heap arm and the inline bypass must rejoin
// BEFORE testing the runtime reader's result. A release in its success block
// would still pass a successful execution while abandoning overflow temporaries.
func numericCastCheckedCleanup(t *testing.T, blocks []*numericIRBlock, defs map[string]numericIRDef, word, ok, out, release, kind string) {
	t.Helper()
	reader := numericCastBlock(t, blocks, defs[ok].block)
	decision := reader
	if kind == "float" {
		if !numericCastHasLine(reader, release) {
			t.Fatal("float cleanup must precede the checked branch in the reader block")
		}
	} else {
		guards := numericHeapGuards(t, blocks, defs)
		branch := numericIRBranch.FindStringSubmatch(reader.lines[len(reader.lines)-1])
		if branch == nil {
			t.Fatal("integer temporary reader has no heap/inline branch")
		}
		heap := numericCastBlock(t, blocks, branch[2])
		guard := guards[heap.name]
		if guard.word != word || guard.from != reader.name || len(heap.preds) != 1 || !heap.preds[reader.name] ||
			!numericCastHasLine(heap, release) {
			t.Fatal("temporary release lacks its own word's heap guard")
		}
		decision = numericCastBlock(t, blocks, branch[3])
		if heap.lines[len(heap.lines)-1] != "br label %"+decision.name ||
			len(decision.preds) != 2 || !decision.preds[reader.name] || !decision.preds[heap.name] {
			t.Fatal("heap cleanup and inline bypass do not rejoin before checked conversion branches")
		}
	}
	branch := numericIRBranch.FindStringSubmatch(decision.lines[len(decision.lines)-1])
	if branch == nil || branch[1] != ok {
		t.Fatal("released temporary does not reach a branch on its reader's exact result")
	}
	good, bad := numericCastBlock(t, blocks, branch[2]), numericCastBlock(t, blocks, branch[3])
	if !strings.Contains(strings.Join(good.lines, "\n"), ", ptr "+out) ||
		!strings.Contains(strings.Join(bad.lines, "\n"), "call void @rt_panic_numeric(") || bad.lines[len(bad.lines)-1] != "unreachable" {
		t.Fatal("checked success must read the output slot; failure must retain its numeric panic")
	}
}

func TestEmitNumericCastBorrowedAndFixedControls(t *testing.T) {
	bodyFor := numericCastBodies(t)
	for _, row := range []struct{ name, reader, kind string }{
		{"borrow_int_i64", "rt_bigint_to_i64", "int"},
		{"borrow_uint_u64", "rt_biguint_to_u64", "uint"},
		{"borrow_float_f64", "rt_bigfloat_to_f64", "float"},
		{"fixed_i64_u64", "", ""},
		{"fixed_f64_i64", "", ""},
	} {
		t.Run(row.name, func(t *testing.T) {
			body := bodyFor(row.name)
			blocks, defs := numericIRBlocks(t, body)
			if row.reader == "" {
				if strings.Contains(body, "@rt_big") || strings.Contains(body, "ptrtoint") {
					t.Fatalf("fixed conversion acquired a numeric owner:\n%s", body)
				}
				return
			}
			read := numericCastSingleMatch(t, body, `(%[\w.]+) = call i1 @`+row.reader+`\(ptr (%[\w.]+), ptr (%[\w.]+)\)`)
			if strings.Contains(body, "call void @rt_big"+row.kind+"_release(ptr "+read[2]+")") {
				t.Fatal("checked conversion consumed its borrowed input")
			}
			b := numericCastBlock(t, blocks, defs[read[1]].block)
			branch := numericIRBranch.FindStringSubmatch(b.lines[len(b.lines)-1])
			if branch == nil || branch[1] != read[1] {
				t.Fatal("borrowing conversion acquired a cleanup branch")
			}
		})
	}
}

func numericCastSingleMatch(t *testing.T, body, pattern string) []string {
	t.Helper()
	matches := regexp.MustCompile(pattern).FindAllStringSubmatch(body, -1)
	if len(matches) != 1 {
		t.Fatalf("expected exactly one %s, got %d:\n%s", pattern, len(matches), body)
	}
	return matches[0]
}

func numericCastBlock(t *testing.T, blocks []*numericIRBlock, name string) *numericIRBlock {
	t.Helper()
	for _, b := range blocks {
		if b.name == name {
			return b
		}
	}
	t.Fatalf("missing conversion block %s", name)
	return nil
}

func numericCastHasLine(b *numericIRBlock, want string) bool {
	for _, line := range b.lines {
		if line == want {
			return true
		}
	}
	return false
}
