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
	// Capability rows only: the two mandatory rows mirror no bit, and their
	// agreement with the manifest is pinned by TestEveryManifestValueOpsSlotHasARule
	// against the rt_value_ops RECORD instead.
	var capabilityRules []slotRule
	for _, rule := range slotRules {
		if rule.when == capabilitySlot {
			capabilityRules = append(capabilityRules, rule)
		}
	}
	if len(manifest) != len(capabilityRules) {
		t.Fatalf("manifest has %d rt_value_flags values, package mirrors %d", len(manifest), len(capabilityRules))
	}
	for _, rule := range capabilityRules {
		want, ok := manifest[rule.flag]
		if !ok {
			t.Errorf("package mirrors %s, which the manifest does not define", rule.flag)
			continue
		}
		if uint64(rule.bit) != want {
			t.Errorf("%s = %d, manifest says %d", rule.flag, rule.bit, want)
		}
	}
	mirrored := make(map[string]bool, len(capabilityRules))
	for _, rule := range capabilityRules {
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
	var got []Flags
	for _, rule := range slotRules {
		if rule.when == capabilitySlot {
			got = append(got, rule.bit)
		}
	}
	if len(want) != len(got) {
		t.Fatalf("%d constants, %d capability rules", len(want), len(got))
	}
	for i, bit := range want {
		if got[i] != bit {
			t.Errorf("capability rule %d covers %d, want %d", i, got[i], bit)
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
		if rule.fill == 0 {
			t.Errorf("rt_value_ops.%s states no fill policy", rule.slot)
			continue
		}
		// A shared filler must name its symbol, and only a shared filler may.
		for _, policy := range []fillPolicy{rule.fill, rule.otherwise} {
			symbol, shared := rule.sharedSymbol(policy)
			if shared && symbol == "" {
				t.Errorf("rt_value_ops.%s is filled by a shared symbol it does not name", rule.slot)
			}
		}
		if rule.runtimeSymbol != "" && rule.moduleSymbol != "" {
			t.Errorf("rt_value_ops.%s names both a runtime and a module symbol", rule.slot)
		}
		// A mandatory slot filled nowhere would ship the null the runtime
		// refuses, so that combination must stay unrepresentable.
		if rule.when == unconditionalSlot && rule.fillFor(0) == filledNowhere {
			t.Errorf("rt_value_ops.%s is mandatory and filled nowhere", rule.slot)
		}
		// A gate is meaningless without a second policy to choose, and vice versa.
		if (rule.gate == 0) != (rule.otherwise == 0) {
			t.Errorf("rt_value_ops.%s has a gate without an alternative, or the reverse", rule.slot)
		}
	}
}

// TestRuntimeFilledSlotNamesOnlyCopy pins which bits the registry expects the
// runtime to fill. Every other bit's slot is the compiler's to emit or is
// staged, and a writer that bound a runtime symbol for one of them would be
// shipping a descriptor nothing implements.
func TestRuntimeFilledSlotNamesOnlyCopy(t *testing.T) {
	filler, err := SlotFiller("copy_init", FlagCopy)
	if err != nil {
		t.Fatalf("copy_init has no filler: %v", err)
	}
	if filler.Kind != FillRuntimeSymbol || filler.Symbol != CopyInitUnboundTrap {
		t.Fatalf("copy_init = %+v, want the runtime trap %s", filler, CopyInitUnboundTrap)
	}
	// Exactly one slot may be filled by a RUNTIME symbol. plan_cross is filled
	// by a shared symbol too, but a module-local one: nothing links against it
	// and it is absent from the manifest, which is the whole point of keeping
	// the two kinds apart.
	for slot := range manifestValueOpsSlots(t) {
		if slot == "copy_init" {
			continue
		}
		other, err := SlotFiller(slot, ^Flags(0)&knownFlags())
		if err != nil {
			t.Errorf("SlotFiller(%q) failed: %v", slot, err)
			continue
		}
		if other.Kind == FillRuntimeSymbol {
			t.Errorf("%s claims the runtime fills its slot with %q", slot, other.Symbol)
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

// manifestValueOpsSlots returns the rt_value_ops callback fields from the frozen
// manifest view, mapped to their declared nullability.
//
// It reads Records, which nothing in this package read before. That absence is
// why the two mandatory slots could go missing: the only manifest agreement the
// package had was over rt_value_flags, an enum that names six capabilities and
// no slots at all, so a slot no bit named was invisible by construction.
func manifestValueOpsSlots(t *testing.T) map[string]string {
	t.Helper()
	for _, record := range abimanifest.GeneratedSchema.Records {
		if record.Name != "rt_value_ops" {
			continue
		}
		out := make(map[string]string, len(record.Fields))
		for _, field := range record.Fields {
			if !strings.HasPrefix(field.Type, "callback:") {
				continue // layout is not a callback slot
			}
			switch {
			case strings.HasSuffix(field.Type, ":nonnull"):
				out[field.Name] = "nonnull"
			case strings.HasSuffix(field.Type, ":nullable"):
				out[field.Name] = "nullable"
			default:
				t.Fatalf("rt_value_ops.%s declares neither nonnull nor nullable: %s", field.Name, field.Type)
			}
		}
		return out
	}
	t.Fatal("generated manifest has no rt_value_ops record")
	return nil
}

// TestEveryManifestValueOpsSlotHasARule pins the slot table against the frozen
// manifest RECORD, in both directions and on both axes.
//
// This is the assertion whose absence let move_init and plan_cross have no rows
// for the life of the epic. A table keyed by capability bit cannot represent a
// slot no bit names, and nothing compared the table to the record that does name
// them — so the gap read as completeness.
func TestEveryManifestValueOpsSlotHasARule(t *testing.T) {
	manifest := manifestValueOpsSlots(t)
	flagValues := manifestValueFlags(t)

	byName := make(map[string]slotRule, len(slotRules))
	for _, rule := range slotRules {
		if _, duplicate := byName[rule.slot]; duplicate {
			t.Errorf("two rules cover rt_value_ops.%s", rule.slot)
		}
		byName[rule.slot] = rule
	}

	for slot, nullability := range manifest {
		rule, ok := byName[slot]
		if !ok {
			t.Errorf("manifest rt_value_ops requires %s, which the package mirrors no rule for", slot)
			continue
		}
		switch nullability {
		case "nonnull":
			if rule.when != unconditionalSlot {
				t.Errorf("%s is non-null in the manifest but its rule is not unconditional", slot)
			}
		case "nullable":
			if rule.when != capabilitySlot {
				t.Errorf("%s is nullable in the manifest but its rule is unconditional", slot)
				continue
			}
			want, defined := flagValues[rule.flag]
			if !defined {
				t.Errorf("%s is gated on %s, which the manifest does not define", slot, rule.flag)
				continue
			}
			if uint64(rule.bit) != want {
				t.Errorf("%s is gated on %s = %d, manifest says %d", slot, rule.flag, rule.bit, want)
			}
		}
	}

	for _, rule := range slotRules {
		if _, ok := manifest[rule.slot]; !ok {
			t.Errorf("package mirrors a rule for %s, which rt_value_ops does not declare", rule.slot)
		}
	}
}

// TestSlotFillerAnswersForEveryManifestSlot stops a row from existing and being
// read by nothing, and pins the failure mode its bit-keyed predecessor had: a
// query it could not express returned a wrong answer instead of an error.
func TestSlotFillerAnswersForEveryManifestSlot(t *testing.T) {
	for slot := range manifestValueOpsSlots(t) {
		if _, err := SlotFiller(slot, 0); err != nil {
			t.Errorf("SlotFiller(%q) failed: %v", slot, err)
		}
	}
	if _, err := SlotFiller("no_such_slot", 0); err == nil {
		t.Error("SlotFiller accepted a slot name no rule covers")
	}
}

// TestPlanCrossFallsBackToTheModuleStubForEveryDescriptorThisWaveCanHold ties
// the plan_cross fallback to the staging of the cross bits mechanically, so the
// wave that un-stages either one cannot ship a stub-filled plan_cross for a type
// that really can cross.
func TestPlanCrossFallsBackToTheModuleStubForEveryDescriptorThisWaveCanHold(t *testing.T) {
	filler, err := SlotFiller("plan_cross", 0)
	if err != nil {
		t.Fatalf("plan_cross has no filler: %v", err)
	}
	if filler.Kind != FillModuleStub || filler.Symbol != PlanCrossUnavailableStub {
		t.Fatalf("plan_cross without cross flags = %+v, want the module stub %s", filler, PlanCrossUnavailableStub)
	}

	for _, bit := range []Flags{FlagShardMovable, FlagCrossClonable} {
		crossing, err := SlotFiller("plan_cross", bit)
		if err != nil {
			t.Fatalf("plan_cross with %v has no filler: %v", bit, err)
		}
		if crossing.Kind != FillBackendDerivedBody {
			t.Errorf("plan_cross with a cross bit = %+v, want a real per-type body", crossing)
		}
	}

	// The two cross bits are no longer in the same state, and the split is the
	// point. cross_move_init has a backend body (Epic 22's move half), so a
	// shard-movable entry is legal; cross_clone_init is still filled nowhere, so
	// an entry claiming it is refused -- the same refusal that kept both out
	// until the move half landed.
	movable := Entry{Type: 1, Flags: FlagShardMovable}
	if err := movable.checkSlots(); err != nil {
		t.Errorf("checkSlots refused FlagShardMovable although cross_move_init is backend-derived: %v", err)
	}
	clonable := Entry{Type: 1, Flags: FlagCrossClonable}
	if err := clonable.checkSlots(); err == nil {
		t.Errorf("checkSlots accepted FlagCrossClonable while cross_clone_init is filled nowhere")
	}
}

// TestMoveInitIsMandatoryAndBackendNamed states the difference the two mandatory
// slots must keep. move_init is semantically unconditional, so it always has a
// real body; plan_cross is only structurally unconditional. Collapsing them
// would put this registry back in the business of naming something it cannot
// see, which is copy_init's original failure in a new coat.
func TestMoveInitIsMandatoryAndBackendNamed(t *testing.T) {
	filler, err := SlotFiller("move_init", 0)
	if err != nil {
		t.Fatalf("move_init has no filler: %v", err)
	}
	if filler.Kind != FillBackendDerivedBody {
		t.Fatalf("move_init = %+v, want a per-type body the backend names", filler)
	}
	if filler.Symbol != "" {
		t.Errorf("move_init names symbol %q; a per-type body has no shared name", filler.Symbol)
	}
	if err := (&Entry{Type: 1}).checkSlots(); err != nil {
		t.Errorf("an entry with no flags was refused: %v", err)
	}
}

// TestSlotFillerRespectsPresenceBeforeFill pins the axis order.
//
// A capability slot whose bit is clear is ABSENT, and an absent slot ships null
// whatever filler its row names. Answering with the filler regardless — which
// the first version of SlotFiller did — hands a non-Copy descriptor copy_init's
// trap, and the runtime refuses that descriptor for breaking the biconditional
// "callback non-null exactly when the bit is set". The defect was invisible to
// the table tests, because the table was right and only the query was wrong.
func TestSlotFillerRespectsPresenceBeforeFill(t *testing.T) {
	cases := []struct {
		slot  string
		flags Flags
		want  FillKind
	}{
		{"copy_init", 0, FillNone},
		{"copy_init", FlagCopy, FillRuntimeSymbol},
		{"clone_init", 0, FillNone},
		{"clone_init", FlagClonable, FillRegistryNamedBody},
		{"drop_in_place", 0, FillNone},
		// Mandatory slots ignore the flags entirely.
		{"move_init", 0, FillBackendDerivedBody},
		{"plan_cross", 0, FillModuleStub},
	}
	for _, tc := range cases {
		got, err := SlotFiller(tc.slot, tc.flags)
		if err != nil {
			t.Errorf("SlotFiller(%q, %v): %v", tc.slot, tc.flags, err)
			continue
		}
		if got.Kind != tc.want {
			t.Errorf("SlotFiller(%q, %v).Kind = %d, want %d", tc.slot, tc.flags, got.Kind, tc.want)
		}
		if got.Kind == FillNone && got.Symbol != "" {
			t.Errorf("SlotFiller(%q, %v) is absent yet names %q", tc.slot, tc.flags, got.Symbol)
		}
	}
}
