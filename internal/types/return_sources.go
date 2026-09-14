package types //nolint:revive

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// ReturnSources is a declared relation between a function result and its input
// slots. Its zero value means all reference-bearing inputs. An explicit empty
// set is a different promise: none of the inputs can back the returned borrow.
// Inferred body facts are kept separately and never change this value.
type ReturnSources struct {
	explicit bool
	slots    []uint32
}

// ExplicitReturnSources owns a normalized copy of the declaration-ordered slots.
// Calling it without slots constructs an explicit empty set, not AllInputs.
func ExplicitReturnSources(slots ...uint32) ReturnSources {
	owned := slices.Clone(slots)
	slices.Sort(owned)
	return ReturnSources{explicit: true, slots: slices.Compact(owned)}
}

// IsAllInputs reports whether the contract admits every reference-bearing input.
func (s ReturnSources) IsAllInputs() bool {
	return !s.explicit
}

// Slots returns a detached copy. IsAllInputs distinguishes AllInputs from an
// explicit empty set, both of which have no enumerated slots.
func (s ReturnSources) Slots() []uint32 {
	return slices.Clone(s.slots)
}

// Equal compares promises without exposing their immutable storage.
func (s ReturnSources) Equal(other ReturnSources) bool {
	return s.explicit == other.explicit && slices.Equal(s.slots, other.slots)
}

// CanonicalKey encodes only the declared relation. AllInputs has no suffix,
// preserving existing keys; an explicit empty promise encodes as empty braces.
func (s ReturnSources) CanonicalKey() string {
	var out strings.Builder
	s.appendCanonicalKey(&out)
	return out.String()
}

func (s ReturnSources) validateArity(arity int) {
	for _, slot := range s.slots {
		if int64(slot) >= int64(arity) {
			panic(fmt.Errorf("function return sources: slot %d is outside arity %d", slot, arity))
		}
	}
}

func (s ReturnSources) appendCanonicalKey(out *strings.Builder) {
	if !s.explicit {
		return
	}
	out.WriteString("@return_source{")
	for i, slot := range s.slots {
		if i > 0 {
			out.WriteByte(',')
		}
		out.WriteString(strconv.FormatUint(uint64(slot), 10))
	}
	out.WriteByte('}')
}

func (s ReturnSources) parameterLabel(index int, label string) string {
	for _, slot := range s.slots {
		if int64(slot) == int64(index) {
			return "@return_source " + label
		}
	}
	return label
}

func (s ReturnSources) resultLabel(label string) string {
	if s.explicit && len(s.slots) == 0 {
		// This internal promise has no parameter mark to print. The explanation
		// is a diagnostic label, not an additional source-language spelling.
		return label + " [no return sources]"
	}
	return label
}
