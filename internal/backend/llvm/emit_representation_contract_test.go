package llvm

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"surge/internal/mir"
	"surge/internal/types"
)

// The representation rules, asserted rather than trusted.
//
// Three defects in one change all had the same shape: a fact about how a value
// is stored was answered by whoever happened to be holding a spelling, instead
// of by the authority that owns the fact. Each was caught by something running
// the program — a benchmark, LLVM's verifier, a crash — rather than by a gate,
// and the two that reached emission produced IR that was wrong rather than IR
// that failed to build.
//
// So the rules are written here as negatives. Each names a shape that must not
// appear, and each one of them is a shape that DID appear:
//
//   - a composite moved by a plain `store`/`load`, when a composite is an
//     ADDRESS and the plain form moves a register;
//   - a size, stride or alignment read off an LLVM type spelling, when only the
//     layout registry knows it;
//   - a parameter or result classified by one authority and lowered by another,
//     when both halves have to describe one ABI;
//   - a suspension frame in a function-scoped alloca, when the runtime owns it
//     past the return.
//
// They are stated over emitted IR where the shape is visible in the output, and
// over the emitter SOURCE where it is not. The difference matters: emitted IR
// only shows the paths a fixture happened to take, so a site no fixture reaches
// reads as compliance. Where a source-shape assertion is used, its known gap is
// stated on the assertion itself.

// representationFixture exercises the shapes the rules are about: composites by
// value, by argument and by return, a zero-sized composite, a fixed array, a
// composite array element, and the two suspension frames — an async state and a
// blocking body's captures.
//
// It also builds each of those by DEFAULT as well as by literal. The two are
// separate emitters, and a corpus that only wrote literals is what let a
// composite-inside-a-composite keep moving through a register in the default
// path long after the literal path was fixed. A shape is only covered here once
// every emitter that can produce it does.
const representationFixture = `
@copy type Point = { x: int64, y: int64 }
type Payload = { a: int64, b: int64, c: int64, d: int64 }
type Empty = { }
type Nested = { inner: Payload, mark: int64 }

fn consume(p: Payload) -> int64 { return p.a + p.d; }

fn make(seed: int64) -> Payload {
	return Payload{ a: seed, b: seed, c: seed, d: seed };
}

fn passEmpty(e: Empty) -> int64 { return 1:int64; }

fn cells() -> int64 {
	let mut arr: Payload[3] = [make(1:int64), make(2:int64), make(3:int64)];
	arr[1] = make(9:int64);
	return arr[1].a + arr[2].d;
}

// Defaulted composites: a struct whose FIELD is a composite, and a fixed array
// whose ELEMENT is one. Both move a composite into storage it does not own, and
// neither goes through the literal emitter.
fn defaults() -> int64 {
	let plain: Payload = default::<Payload>();
	let nested: Nested = default::<Nested>();
	let cellArr: Payload[3] = default::<Payload[3]>();
	return plain.a + nested.inner.a + nested.mark + cellArr[2].d;
}

async fn work(seed: int64) -> int64 {
	let held: Payload = make(seed);
	let job: Task<int64> = blocking {
		ret held.a + held.b;
	};
	return compare job.await() {
		Success(v) => v;
		Cancelled() => 0:int64;
	};
}

@entrypoint
fn main() -> int {
	let p: Point = Point{ x: 1:int64, y: 2:int64 };
	let total: int64 = consume(make(p.x)) + cells() + passEmpty(Empty{}) + defaults();
	let async_total: int64 = compare work(3:int64).await() {
		Success(v) => v;
		Cancelled() => 0:int64;
	};
	if total + async_total > 0:int64 { return 0; }
	return 1;
}
`

// emitRepresentationFixture lowers the fixture and returns both halves the rules
// are stated over: the emitted module, and the MIR the classification is read
// from.
func emitRepresentationFixture(t *testing.T) (string, *mir.Module) {
	t.Helper()
	mirMod, result := lowerMIRFromSource(t, representationFixture)
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	return ir, mirMod
}

var (
	compositeRegisterStore = regexp.MustCompile(`^\s*store\s+\[\d+ x i8\]`)
	compositeRegisterLoad  = regexp.MustCompile(`^\s*%\S+ = load\s+\[\d+ x i8\]`)
)

// A composite is never moved by a plain store or load.
//
// It has no register to be moved through: the operand naming one names the
// address of its storage, so `store [64 x i8] %p, ptr %q` is a pointer being
// passed off as the bytes it points at. LLVM's own verifier rejects that exact
// pair, which is the lucky case — the same confusion at a site the verifier
// accepts writes eight bytes where sixty-four were meant.
//
// A composite is copied as bytes, at the alignment the layout registry
// published, by the memcpy `emitStorageCopy` writes.
func TestCompositesAreNeverMovedThroughARegister(t *testing.T) {
	ir, _ := emitRepresentationFixture(t)
	var offenders []string
	for i, line := range strings.Split(ir, "\n") {
		if compositeRegisterStore.MatchString(line) || compositeRegisterLoad.MatchString(line) {
			offenders = append(offenders, fmt.Sprintf("%d: %s", i+1, strings.TrimSpace(line)))
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("%d composite values move through a register instead of as bytes:\n%s",
			len(offenders), strings.Join(offenders, "\n"))
	}
}

// A composite parameter is passed as an address carrying `byval`, and both the
// attribute and the hidden result destination state an alignment.
//
// An unattributed `byval` or `sret` leaves LLVM to infer the alignment of memory
// whose alignment only the layout registry knows, which is the same guess the
// explicit operands exist to remove — and for a byte run the guess is align 1.
func TestByvalAndSretDestinationsStateTheirAlignment(t *testing.T) {
	ir, _ := emitRepresentationFixture(t)
	var offenders []string
	for i, line := range strings.Split(ir, "\n") {
		for _, attr := range []string{"byval(", "sret("} {
			idx := strings.Index(line, attr)
			if idx < 0 {
				continue
			}
			rest := line[idx:]
			if !strings.Contains(rest, " align ") {
				offenders = append(offenders, fmt.Sprintf("%d: %s", i+1, strings.TrimSpace(line)))
			}
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("%d by-address positions state no alignment:\n%s",
			len(offenders), strings.Join(offenders, "\n"))
	}
}

var defineLine = regexp.MustCompile(`^define\s+(\S+)\s+@(fn\.\d+)\(([^)]*)\)`)

// Every generated definition lowers the parameter and result classes the call
// contract published for it.
//
// The two halves are read from different places and compared: the classes come
// from `mir.CallLayoutTable` keyed on the function's declared types, and the
// spellings are parsed back out of the emitted text. A disagreement means the
// caller and the callee read different argument positions, or one of them
// allocated a result destination the other did not.
//
// SCOPE, stated because it overlaps something that already exists. The emitter
// REFUSES a signature whose spelling and classification disagree, at
// `loweredSignature` — and that refusal caught a real defect, so it stays. Every
// definition that goes through it therefore cannot fail this assertion. What
// this adds is the case the refusal structurally cannot see: a definition
// emitted by a path that does not consult the contract at all. The refusal is a
// check inside one helper; this is a check on the OUTPUT, so it holds for any
// way the output came to be. It also fires at test time rather than only when
// somebody compiles a program with that shape in it.
func TestEmittedSignaturesLowerTheClassesTheContractPublished(t *testing.T) {
	ir, mirMod := emitRepresentationFixture(t)
	if mirMod.Meta == nil || mirMod.Meta.CallLayouts == nil {
		t.Fatal("module carries no finalized call layout table")
	}

	spellings := make(map[string]string)
	for _, line := range strings.Split(ir, "\n") {
		if m := defineLine.FindStringSubmatch(line); m != nil {
			spellings[m[2]] = m[1] + " (" + m[3] + ")"
		}
	}

	checked := 0
	for _, id := range mirMod.SortedFuncIDs() {
		f := mirMod.Funcs[id]
		if f == nil {
			continue
		}
		name := fmt.Sprintf("fn.%d", f.ID)
		spelling, ok := spellings[name]
		if !ok {
			// Not every lowered function reaches a definition: an intrinsic and
			// a runtime declaration are spelled by an authority of their own.
			continue
		}
		checked++
		assertSignatureMatchesContract(t, mirMod, f, name, spelling)
	}
	if checked == 0 {
		t.Fatal("no generated definitions were checked; the fixture stopped covering them")
	}
}

// assertSignatureMatchesContract compares one definition's spelling against the
// classes the contract published for the same function.
func assertSignatureMatchesContract(t *testing.T, mod *mir.Module, f *mir.Func, name, spelling string) {
	t.Helper()

	paramTypes, result, ok := signatureTypesOf(f)
	if !ok {
		return
	}
	layoutOf, err := mod.Meta.CallLayouts.OfSignature(paramTypes, result, mir.ABIDomainSurge)
	if err != nil {
		// A signature the contract cannot classify is out of scope for this
		// assertion rather than a failure of it: the emitter refuses such a
		// function on its own path.
		return
	}
	abi, err := layoutOf.Surge()
	if err != nil {
		return
	}

	byvalCount := strings.Count(spelling, "byval(")
	wantByval := 0
	for _, p := range abi.Params {
		if p.Class == mir.ParamByval {
			wantByval++
		}
	}
	if byvalCount != wantByval {
		t.Errorf("%s: contract classifies %d by-address parameters, definition spells %d\n\t%s",
			name, wantByval, byvalCount, spelling)
	}

	hasSret := strings.Contains(spelling, "sret(")
	wantSret := abi.Ret.Class == mir.RetSret
	if hasSret != wantSret {
		t.Errorf("%s: contract classifies result %s, definition %s a hidden destination\n\t%s",
			name, abi.Ret.Class, map[bool]string{true: "spells", false: "omits"}[hasSret], spelling)
	}
}

// signatureTypesOf reads a lowered function's declared parameter and result
// types, which is what the contract classifies.
//
// Only the form the emitter reaches through `f.ParamCount` is read. A function
// whose parameters are recovered from the symbol table, and one whose result was
// inferred from the values it returns, are both skipped by the caller: this
// assertion is about the two halves agreeing, so it must not introduce a THIRD
// way of deciding what the signature is.
func signatureTypesOf(f *mir.Func) (params []types.TypeID, result types.TypeID, ok bool) {
	if f == nil || f.ParamCount > len(f.Locals) {
		return nil, types.NoTypeID, false
	}
	if f.Result == types.NoTypeID {
		return nil, types.NoTypeID, false
	}
	params = make([]types.TypeID, 0, f.ParamCount)
	for i := range f.ParamCount {
		ty := f.Locals[i].Type
		if ty == types.NoTypeID {
			return nil, types.NoTypeID, false
		}
		params = append(params, ty)
	}
	return params, f.Result, true
}

// storageEmitters are the files that move a Surge VALUE between storage and a
// register, and are therefore the files where a size or an alignment read off a
// spelling would be read about a composite.
//
// The list is a ratchet and no name may leave it.
var storageEmitters = []string{
	"emit_access.go",
	"emit_aggregate_ops.go",
	"emit_call_site.go",
	"emit_func.go",
	"emit_globals.go",
	"emit_helpers_place.go",
	"emit_instr.go",
	"emit_intrinsics_default.go",
	"emit_intrinsics_array.go",
	"emit_intrinsics_array_element.go",
	"emit_literals.go",
	"emit_storage_type.go",
	"emit_term.go",
}

// spellingSizers are the helpers that answer a physical fact from an LLVM type
// spelling. They are correct only for a machine word, whose size its own
// spelling carries; a composite is spelled as a byte run, and a byte run says
// how many bytes without saying how they must be placed.
var spellingSizers = regexp.MustCompile(`\b(llvmTypeSizeAlign|llvmTypeStrideAlign|naturalAlign)\s*\(`)

// spellingSizerCallers is every function permitted to ask a spelling for a
// physical fact, each because it has already established that the value is NOT
// a composite, or because it is the guard itself.
//
// KNOWN GAP, stated rather than papered over: this matches the name of the
// enclosing `func` and does not follow dataflow, so it proves that no NEW site
// asks the question — not that each listed site still guards correctly. The
// guards themselves are pinned by their own tests: `naturalAlign` refuses a byte
// run outright (TestNaturalAlignRefusesAnUnknownType). A reviewer adding a caller has
// to add it here, which is the point — the list is where the question gets
// asked, so it is where the answer gets checked.
var spellingSizerCallers = map[string]bool{
	// The guards themselves. `naturalAlign` refuses a byte run outright, so
	// every caller below that goes through it fails closed on a composite
	// rather than taking align 1.
	"naturalAlign":      true,
	"llvmTypeSizeAlign": true,

	// Ask only after establishing the value is NOT carried inline.
	"arrayElemStride": true, // returns the registry's stride for an inline element first
	"storageAlignOf":  true, // asks only when the spelling is not a storage run

	// Reach a spelling that cannot be a byte run, and fail closed if it ever is.
	"emitAlloca":  true, // a composite is reserved by emitAllocaAligned
	"emitLoad":    true, // a composite is moved by emitStorageCopy
	"emitStore":   true, // a composite is moved by emitStorageCopy
	"emitGlobals": true, // a global is a const scalar or string; a composite refuses
}

var enclosingFunc = regexp.MustCompile(`^func\s+(?:\([^)]*\)\s*)?(\w+)`)

// No physical fact about a composite is read off an LLVM type spelling.
//
// This is the rule the other assertions cannot see, and it is the one that would
// have caught both of the spelling defects before anything ran: a stride taken
// from a table of scalar spellings, and an element stored under the type its
// spelling named. Both were single call sites in files that emit ordinary
// storage, and both were invisible in emitted IR — one failed the build with
// "unsupported llvm type size for [64 x i8]", the other emitted a store LLVM
// then rejected.
//
// Checked against the SOURCE, because emitted IR shows only the paths a fixture
// took while the source shows every site there is.
func TestPhysicalFactsAreNotReadOffASpelling(t *testing.T) {
	for _, name := range storageEmitters {
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		enclosing := ""
		for i, line := range strings.Split(string(source), "\n") {
			if m := enclosingFunc.FindStringSubmatch(line); m != nil {
				enclosing = m[1]
			}
			if !spellingSizers.MatchString(line) {
				continue
			}
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			if spellingSizerCallers[enclosing] {
				continue
			}
			t.Errorf("%s:%d %s asks a spelling for a physical fact:\n\t%s",
				name, i+1, enclosing, strings.TrimSpace(line))
		}
	}
}

var (
	runtimeOwnedFrameSink = regexp.MustCompile(`call ptr @(__task_create|rt_blocking_submit)\(([^)]*)\)`)
	allocaTemp            = regexp.MustCompile(`^\s*(%\S+) = alloca `)
)

// A suspension frame is never handed to the runtime from a function-scoped
// alloca.
//
// The runtime takes the pointer, keeps it past the suspension, and releases it
// at the width the type it was named by states — `rt_blocking_submit` adopts it
// into the job and the task owns its resume state the same way. A frame in an alloca
// is therefore read after the function that built it returned, and then freed at
// a stack address. Both happened: every async program on this backend crashed
// with `free(): invalid pointer` and every blocking one segfaulted.
//
// The frame's storage comes from the runtime allocator, so the operand naming it
// is never one of this function's allocas.
func TestSuspensionFramesAreNotHandedOverFromTheStack(t *testing.T) {
	ir, _ := emitRepresentationFixture(t)

	// Temporary names restart at each definition, so the allocas in scope are
	// only the ones this definition made. Carrying them across would match a
	// name in one function against a handover in another.
	allocas := make(map[string]string)
	var offenders []string
	for i, line := range strings.Split(ir, "\n") {
		if strings.HasPrefix(line, "define ") {
			allocas = make(map[string]string)
		}
		if m := allocaTemp.FindStringSubmatch(line); m != nil {
			allocas[m[1]] = strings.TrimSpace(line)
		}
		m := runtimeOwnedFrameSink.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		for _, arg := range strings.Split(m[2], ",") {
			arg = strings.TrimSpace(arg)
			if !strings.HasPrefix(arg, "ptr ") {
				continue
			}
			operand := strings.TrimSpace(strings.TrimPrefix(arg, "ptr "))
			if decl, isAlloca := allocas[operand]; isAlloca {
				offenders = append(offenders, fmt.Sprintf(
					"%d: %s\n\t  hands over %s, which is %s", i+1, strings.TrimSpace(line), operand, decl))
			}
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("%d suspension frames are handed to the runtime from the stack:\n%s",
			len(offenders), strings.Join(offenders, "\n"))
	}
}
