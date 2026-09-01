package llvm

import (
	"regexp"
	"strings"
	"testing"
)

// A map entry is handed over BY ADDRESS, at its own type.
//
// Before the storage flip every one of these calls took a `uint64`: a key was
// squeezed through a machine word on the way in, a value was copied into a
// transport allocation whose address became the word, and the answer came back
// as a word that had to be turned into a pointer again. The whole family is
// pinned in one test because the word was one contract, and half a migration
// off it is worse than none.
func TestMapEntriesAreHandedOverByAddress(t *testing.T) {
	ir := emitMapProbeIR(t, `@entrypoint
fn main() -> int {
    let mut m = Map::<string, int>.new();
    let _ = m.insert("a", 1);
    {
        let hit = m.get_mut(&"a");
        compare hit {
            Some(p) => { *p = *p + 1; }
            _ => {}
        }
    }
    return compare m.remove(&"a") {
        Some(v) => v;
        nothing => 0;
    };
}
`)

	// The constructor hands the map the two descriptors that make its storage
	// typed: without them the runtime cannot say how wide an entry is.
	newRe := regexp.MustCompile(`call ptr @rt_map_new\(i64 1, ptr @__surge_value_ops_type\d+, ptr @__surge_value_ops_type\d+\)`)
	if !newRe.MatchString(ir) {
		t.Fatalf("rt_map_new was not given a key and a value descriptor:\n%s", ir)
	}

	for _, want := range []struct {
		intrinsic string
		arity     int
	}{
		{"rt_map_get_mut", 3},
		{"rt_map_insert", 4},
		{"rt_map_remove", 3},
	} {
		pattern := `call i1 @` + want.intrinsic + `\(` +
			strings.TrimSuffix(strings.Repeat(`ptr [^,)]+, `, want.arity), ", ") + `\)`
		if !regexp.MustCompile(pattern).MatchString(ir) {
			t.Fatalf("%s does not take %d pointers:\n%s", want.intrinsic, want.arity, ir)
		}
		assertMapOptionWrittenInPlace(t, ir, want.intrinsic)
	}
}

// A composite value goes into the map's own entry storage, so nothing allocates
// a box for it on the way in.
//
// This is the reading the old shape could not give: an entry was two words, a
// `Payload16` does not fit in one, so the insert allocated a transport block,
// copied the value into it and stored the block's address. The map's declared
// value type and the buffer's element type disagreed by construction.
func TestMapCompositeValueIsInsertedWithoutATransportAllocation(t *testing.T) {
	ir := emitMapProbeIR(t, `type Payload16 = { a: uint64, b: uint64 };

@entrypoint
fn main() -> int {
    let mut m = Map::<uint64, Payload16>.new();
    let key: uint64 = 1:uint64;
    let _ = m.insert(key, Payload16 { a = 7:uint64, b = 9:uint64 });
    return compare m.remove(&key) {
        Some(v) => v.a to int;
        nothing => 0;
    };
}
`)

	body := llvmFunctionContaining(t, ir, "call i1 @rt_map_insert(")
	for _, forbidden := range []string{"call ptr @rt_alloc(", "ptrtoint", "inttoptr"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("inserting a composite value still goes through %q:\n%s", forbidden, body)
		}
	}
	removed := llvmFunctionContaining(t, ir, "call i1 @rt_map_remove(")
	for _, forbidden := range []string{"call void @rt_free(", "ptrtoint", "inttoptr"} {
		if strings.Contains(removed, forbidden) {
			t.Fatalf("removing a composite value still goes through %q:\n%s", forbidden, removed)
		}
	}
}

// assertMapOptionWrittenInPlace checks the one shape every map entry point that
// answers an `Option` now has: the destination union is reserved first, the
// runtime is handed the address of its `Some` payload, and the tag is the only
// thing the emitted code decides afterwards.
//
// Naming the payload address is the point. An entry point handed some other
// slot would publish its answer where nothing reads it, which is exactly how
// three hand-written copies of this path used to be able to drift.
func assertMapOptionWrittenInPlace(t *testing.T, ir, intrinsic string) {
	t.Helper()

	callRe := regexp.MustCompile(`(%t\d+) = call i1 @` + intrinsic + `\([^)]*ptr (%t\d+)\)\n`)
	call := callRe.FindStringSubmatchIndex(ir)
	if call == nil {
		t.Fatalf("no %s call in emitted IR:\n%s", intrinsic, ir)
	}
	okVal := ir[call[2]:call[3]]
	payload := ir[call[4]:call[5]]

	before := ir[:call[0]]
	if start := strings.LastIndex(before, "\ndefine "); start >= 0 {
		before = before[start:]
	}
	payloadRe := regexp.MustCompile(
		regexp.QuoteMeta(payload) + ` = getelementptr inbounds i8, ptr (%t\d+), i64 \d+\n`)
	payloadMatch := payloadRe.FindStringSubmatch(before)
	if payloadMatch == nil {
		t.Fatalf("%s was not handed the destination Option's payload address:\n%s", intrinsic, before)
	}
	option := payloadMatch[1]
	allocRe := regexp.MustCompile(regexp.QuoteMeta(option) + ` = alloca \[(\d+) x i8\], align (\d+)\n`)
	alloc := allocRe.FindStringSubmatch(before)
	if alloc == nil {
		t.Fatalf("%s: the payload address %s is not inside a reserved Option:\n%s", intrinsic, payload, before)
	}
	zero := "call void @llvm.memset.p0.i64(ptr align " + alloc[2] + " " + option +
		", i8 0, i64 " + alloc[1] + ", i1 false)"
	zeroAt := strings.Index(before, zero)
	payloadAt := strings.Index(before, payloadMatch[0])
	if zeroAt < 0 || zeroAt > payloadAt {
		t.Fatalf("%s: the Option storage %s is not deterministically initialized before its payload is exposed:\n%s",
			intrinsic, option, before)
	}

	after := ir[call[1]:]
	if end := strings.Index(after, "\n}\n"); end >= 0 {
		after = after[:end]
	}
	tagRe := regexp.MustCompile(`(%t\d+) = select i1 ` + regexp.QuoteMeta(okVal) + `, i32 \d+, i32 \d+\n`)
	tag := tagRe.FindStringSubmatch(after)
	if tag == nil {
		t.Fatalf("%s: the hit flag %s does not choose the Option's tag:\n%s", intrinsic, okVal, after)
	}
	if !strings.Contains(after, "store i32 "+tag[1]+", ptr "+option+",") {
		t.Fatalf("%s: the chosen tag %s is never written into %s:\n%s", intrinsic, tag[1], option, after)
	}
}

func emitMapProbeIR(t *testing.T, sourceCode string) string {
	t.Helper()
	withRepoStdlib(t)
	return emitLLVMFromSource(t, sourceCode)
}
