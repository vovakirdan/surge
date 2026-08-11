package valueops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/abimanifest"
)

// manifestValueFlags returns the frozen rt_value_flags enum from the generated
// manifest view.
func manifestValueFlags(t *testing.T) map[string]uint64 {
	t.Helper()
	for _, enum := range abimanifest.GeneratedSchema.Enums {
		if enum.Name != "rt_value_flags" {
			continue
		}
		out := make(map[string]uint64, len(enum.Values))
		for _, value := range enum.Values {
			out[value.Name] = value.Value
		}
		return out
	}
	t.Fatal("generated manifest has no rt_value_flags enum")
	return nil
}

// TestFlagsMirrorTheFrozenManifestValues pins every Flags constant to the frozen
// ABI by value, in both directions, so neither side can gain or move a bit
// unnoticed.
//
// It deliberately asserts constant VALUES only. Slot presence is a property of a
// materialized rt_value_ops struct, and this wave materializes none, so pinning
// slot presence here would assert something the registry does not yet claim.
func TestFlagsMirrorTheFrozenManifestValues(t *testing.T) {
	manifest := manifestValueFlags(t)
	if len(manifest) != len(slotRules) {
		t.Fatalf("manifest has %d rt_value_flags values, package mirrors %d", len(manifest), len(slotRules))
	}
	for _, rule := range slotRules {
		want, ok := manifest[rule.flag]
		if !ok {
			t.Errorf("package mirrors %s, which the manifest does not define", rule.flag)
			continue
		}
		if uint64(rule.bit) != want {
			t.Errorf("%s = %d, manifest says %d", rule.flag, rule.bit, want)
		}
	}
	mirrored := make(map[string]bool, len(slotRules))
	for _, rule := range slotRules {
		mirrored[rule.flag] = true
	}
	for name := range manifest {
		if !mirrored[name] {
			t.Errorf("manifest defines %s, which the package does not mirror", name)
		}
	}
}

// TestFlagConstantsMatchTheirRuleTable guards against the constants and the
// slot-rule table drifting apart, since the table is what the invariant reads.
func TestFlagConstantsMatchTheirRuleTable(t *testing.T) {
	want := []Flags{FlagCopy, FlagClonable, FlagDroppable, FlagTraceable, FlagShardMovable, FlagCrossClonable}
	if len(want) != len(slotRules) {
		t.Fatalf("%d constants, %d rules", len(want), len(slotRules))
	}
	for i, bit := range want {
		if slotRules[i].bit != bit {
			t.Errorf("rule %d covers %d, want %d", i, slotRules[i].bit, bit)
		}
	}
}

// TestEverySlotRuleStatesExactlyOneWayToFillItsSlot keeps the three dispositions
// disjoint. A rule that was both structural and staged, or neither and also not
// reachable through emittedSlot, would let checkSlots pass a bit whose slot
// nothing fills — which is precisely how copy_init came to be described as
// satisfied by a runtime symbol that did not exist.
func TestEverySlotRuleStatesExactlyOneWayToFillItsSlot(t *testing.T) {
	for _, rule := range slotRules {
		if rule.structural() && rule.staged {
			t.Errorf("%s is both filled by %s and staged", rule.flag, rule.runtimeSymbol)
		}
		if !rule.structural() && !rule.staged && rule.bit != FlagClonable {
			t.Errorf("%s is neither runtime-filled nor staged, and no emitted slot backs it", rule.flag)
		}
	}
}

// TestRuntimeFilledSlotNamesOnlyCopy pins which bits the registry expects the
// runtime to fill. Every other bit's slot is the compiler's to emit or is
// staged, and a writer that bound a runtime symbol for one of them would be
// shipping a descriptor nothing implements.
func TestRuntimeFilledSlotNamesOnlyCopy(t *testing.T) {
	symbol, ok := RuntimeFilledSlot(FlagCopy)
	if !ok {
		t.Fatal("RT_VALUE_FLAG_COPY has no runtime-filled slot; copy_init would ship null")
	}
	if symbol != CopyInitUnboundTrap {
		t.Fatalf("copy_init is filled by %q, want %q", symbol, CopyInitUnboundTrap)
	}
	for _, bit := range []Flags{FlagClonable, FlagDroppable, FlagTraceable, FlagShardMovable, FlagCrossClonable} {
		if name, filled := RuntimeFilledSlot(bit); filled {
			t.Errorf("%s claims the runtime fills its slot with %q", Flags(bit), name)
		}
	}
}

// TestTheCopyInitTrapExistsInTheRuntimeAndDoesNotCopy is the check that turns
// the structural exemption from a claim into a fact, in both halves.
//
// The registry says a Copy descriptor binds CopyInitUnboundTrap, so the runtime
// has to declare that symbol under the frozen ABI and define it, or every Copy
// descriptor is refused by rt_slot_control the first time one reaches it. And
// the registry says the symbol is a TRAP and not the thing that copies, so the
// definition has to abort and the bytes have to be moved somewhere else. A test
// that only grepped for the definition line would pass identically for a body
// that copies and a body that aborts, which is how the false description this
// test replaces survived: it reads the two bodies instead.
func TestTheCopyInitTrapExistsInTheRuntimeAndDoesNotCopy(t *testing.T) {
	native := filepath.Join("..", "..", "runtime", "native")
	for _, tc := range []struct {
		file string
		want string
		why  string
	}{
		{
			file: "rt_typed_carrier_abi.generated.h",
			want: "void " + CopyInitUnboundTrap + "(void* dst, const void* src);",
			why:  "the frozen ABI must declare the trap with rt_value_copy_init_fn's signature",
		},
		{
			file: "rt_typed_carrier_abi_checks.generated.h",
			want: "\"" + CopyInitUnboundTrap + " signature drift\"",
			why:  "the generated checks must pin the trap's signature",
		},
	} {
		body, err := os.ReadFile(filepath.Join(native, tc.file))
		if err != nil {
			t.Fatalf("read %s: %v", tc.file, err)
		}
		if !strings.Contains(string(body), tc.want) {
			t.Errorf("%s does not contain %q: %s", tc.file, tc.want, tc.why)
		}
	}

	source := readRuntimeSource(t, filepath.Join(native, "rt_value_ops.c"))

	trap := functionBody(t, source, "void "+CopyInitUnboundTrap+"(void* dst, const void* src) {")
	if !strings.Contains(trap, "abort();") {
		t.Errorf("%s does not abort; the registry describes it as a trap, so a body that "+
			"returns would let a caller publish uninitialized storage:\n%s", CopyInitUnboundTrap, trap)
	}
	if !strings.Contains(trap, CopyInitUnboundTrap) {
		t.Errorf("%s does not name itself in its diagnosis:\n%s", CopyInitUnboundTrap, trap)
	}
	for _, copying := range []string{"memcpy", "memmove", "*dst", "dst[", "dst->"} {
		if strings.Contains(trap, copying) {
			t.Errorf("%s contains %q: the frozen callback signature carries no width, so this "+
				"symbol cannot be the thing that copies", CopyInitUnboundTrap, copying)
		}
	}

	// The other half of the same sentence: something does copy, it takes the
	// width from the descriptor, and it is not reachable through the slot.
	const helper = "void rt_value_copy_init(const rt_value_ops* operations, void* dst, const void* src) {"
	if !strings.Contains(source, helper) {
		t.Fatalf("rt_value_ops.c no longer defines the helper that holds the descriptor while it copies")
	}
	copier := functionBody(t, source, helper)
	if !strings.Contains(copier, "memcpy(dst, src, operations->layout.size)") {
		t.Errorf("rt_value_copy_init does not copy rt_value_layout.size bytes, so nothing in the "+
			"runtime performs the copy the registry's structural exemption assumes:\n%s", copier)
	}
}

// functionBody returns the brace-balanced body that follows one exact C
// definition line, so a test can assert what a function DOES rather than that it
// was mentioned.
func functionBody(t *testing.T, source, signature string) string {
	t.Helper()
	start := strings.Index(source, signature)
	if start < 0 {
		t.Fatalf("no definition of %q", signature)
	}
	open := start + len(signature) - 1
	depth := 0
	for index := open; index < len(source); index++ {
		switch source[index] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return source[open : index+1]
			}
		}
	}
	t.Fatalf("definition of %q has no balanced body", signature)
	return ""
}

func readRuntimeSource(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(body)
}

func TestFlagsStringIsStableAndNamesUnknownBits(t *testing.T) {
	for _, tc := range []struct {
		name  string
		flags Flags
		want  string
	}{
		{name: "empty", flags: 0, want: "none"},
		{name: "copy", flags: FlagCopy, want: "copy"},
		{name: "abi true bits", flags: FlagCopy | FlagClonable, want: "copy|clonable"},
		{name: "bit order is fixed", flags: FlagClonable | FlagCopy, want: "copy|clonable"},
		{name: "every mirrored bit", flags: knownFlags(), want: "copy|clonable|droppable|traceable|shard_movable|cross_clonable"},
		{name: "unknown bit is named", flags: FlagCopy | 1<<8, want: "copy|unknown:0x100"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.flags.String(); got != tc.want {
				t.Fatalf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}
