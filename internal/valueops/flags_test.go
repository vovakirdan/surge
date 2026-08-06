package valueops

import (
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
