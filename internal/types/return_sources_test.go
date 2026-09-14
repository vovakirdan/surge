package types

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestReturnSourcesCanonical(t *testing.T) {
	t.Run("all_zero", func(t *testing.T) {
		var sources ReturnSources
		if !sources.IsAllInputs() || len(sources.Slots()) != 0 {
			t.Fatal("zero contract must denote AllInputs")
		}
	})
	t.Run("explicit_empty", func(t *testing.T) {
		sources := ExplicitReturnSources()
		if sources.IsAllInputs() || len(sources.Slots()) != 0 || sources.Equal(ReturnSources{}) {
			t.Fatal("explicit empty collapsed into AllInputs")
		}
	})
	t.Run("sorted_unique_copy", func(t *testing.T) {
		input := []uint32{2, 0, 2, 1}
		sources := ExplicitReturnSources(input...)
		if !slices.Equal(input, []uint32{2, 0, 2, 1}) {
			t.Fatal("normalization changed the caller's slice")
		}
		input[0] = 9
		if !slices.Equal(sources.Slots(), []uint32{0, 1, 2}) || !sources.Equal(ExplicitReturnSources(1, 2, 0)) {
			t.Fatal("contract did not own a canonical copy")
		}
	})
	t.Run("getter_copy", func(t *testing.T) {
		in := NewInterner()
		ref := in.Intern(MakeReference(in.Builtins().String, false))
		fn := in.RegisterFnWithReturnSources([]TypeID{ref}, ref, ExplicitReturnSources(0))
		info, _ := in.FnInfo(fn)
		slots := info.ReturnSources().Slots()
		slots[0] = 17
		if !info.ReturnSources().Equal(ExplicitReturnSources(0)) {
			t.Fatal("getter exposed interned contract storage")
		}
	})
}

func TestFnReturnSourcesIdentity(t *testing.T) {
	in := NewInterner()
	ref := in.Intern(MakeReference(in.Builtins().String, false))
	params := []TypeID{ref, ref}
	t.Run("all_legacy", func(t *testing.T) {
		legacy := in.RegisterFn(params, ref)
		if got := in.RegisterFnWithReturnSources(params, ref, ReturnSources{}); got != legacy {
			t.Fatalf("default contract created a second type: %d != %d", got, legacy)
		}
	})
	t.Run("explicit0_vs1", func(t *testing.T) {
		first := in.RegisterFnWithReturnSources(params, ref, ExplicitReturnSources(0))
		second := in.RegisterFnWithReturnSources(params, ref, ExplicitReturnSources(1))
		if first == second || first == in.RegisterFn(params, ref) {
			t.Fatal("distinct declared sources shared a function type")
		}
		if got := in.RegisterFnWithReturnSources(params, ref, ExplicitReturnSources(0, 0)); got != first {
			t.Fatal("canonical equal sources did not reuse the type")
		}
	})
	t.Run("explicit_empty_vs_all", func(t *testing.T) {
		empty := in.RegisterFnWithReturnSources(params, ref, ExplicitReturnSources())
		if empty == in.RegisterFn(params, ref) {
			t.Fatal("explicit empty and AllInputs shared a function type")
		}
	})
	t.Run("zero_arity_modes", func(t *testing.T) {
		all := in.RegisterFn(nil, ref)
		empty := in.RegisterFnWithReturnSources(nil, ref, ExplicitReturnSources())
		if all == empty || in.RebuildFn(empty, nil, ref) != empty {
			t.Fatal("zero-arity contracts lost their distinct identity")
		}
	})
	t.Run("rebuilt_contract", func(t *testing.T) {
		original := in.RegisterFnWithReturnSources(params, ref, ExplicitReturnSources(1))
		replacement := in.Intern(MakeReference(in.Builtins().Bool, false))
		got := in.RebuildFn(original, []TypeID{replacement, replacement}, replacement)
		info, _ := in.FnInfo(got)
		before, _ := in.FnInfo(original)
		if got == original || !info.ReturnSources().Equal(ExplicitReturnSources(1)) ||
			info.Result != replacement || !slices.Equal(info.Params, []TypeID{replacement, replacement}) {
			t.Fatal("rebuild did not substitute the signature and preserve its contract")
		}
		if before.Result != ref || !slices.Equal(before.Params, params) || !before.ReturnSources().Equal(ExplicitReturnSources(1)) {
			t.Fatal("rebuild mutated the original interned type")
		}
	})
}

func TestFnReturnSourcesKeyAndLabel(t *testing.T) {
	in := NewInterner()
	ref := in.Intern(MakeReference(in.Builtins().String, false))
	ctx := CanonicalKeyContext{Types: in}
	refKey, err := ctx.TypeKey(ref)
	if err != nil {
		t.Fatal(err)
	}
	shapeKey := fmt.Sprintf("[%d:%s%d:%s]->%s", len(refKey), refKey, len(refKey), refKey, refKey)
	for _, tc := range []struct {
		name    string
		sources ReturnSources
		key     string
		label   string
	}{
		{"all_legacy", ReturnSources{}, "fn" + shapeKey, "fn(&string, &string) -> &string"},
		{"explicit_slots", ExplicitReturnSources(1, 0), "fn@return_source{0,1}" + shapeKey, "fn(@return_source &string, @return_source &string) -> &string"},
		{"explicit_empty", ExplicitReturnSources(), "fn@return_source{}" + shapeKey, "fn(&string, &string) -> &string [no return sources]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fn := in.RegisterFnWithReturnSources([]TypeID{ref, ref}, ref, tc.sources)
			key, err := ctx.TypeKey(fn)
			if err != nil || key != tc.key || Label(in, fn) != tc.label {
				t.Fatalf("key/label = %q / %q, error %v; want %q / %q", key, Label(in, fn), err, tc.key, tc.label)
			}
			legacy := in.RegisterFn([]TypeID{in.Builtins().Bool}, in.Builtins().Bool)
			legacyKey, err := ctx.TypeKey(legacy)
			if err != nil || legacyKey != "fn[4:bool]->bool" || Label(in, legacy) != "fn(bool) -> bool" {
				t.Fatalf("legacy representation changed: %q / %q, error %v", legacyKey, Label(in, legacy), err)
			}
		})
	}
}

func TestFnReturnSourcesInvalidInternalUse(t *testing.T) {
	in := NewInterner()
	ref := in.Intern(MakeReference(in.Builtins().String, false))
	t.Run("slot_out_of_range", func(t *testing.T) {
		for _, slot := range []uint32{1, ^uint32(0)} {
			assertReturnSourcesPanic(t, "outside arity 1", func() {
				in.RegisterFnWithReturnSources([]TypeID{ref}, ref, ExplicitReturnSources(slot))
			})
		}
	})
	t.Run("original_not_fn", func(t *testing.T) {
		assertReturnSourcesPanic(t, "is not a function", func() {
			in.RebuildFn(ref, []TypeID{ref}, ref)
		})
	})
	t.Run("rebuild_arity", func(t *testing.T) {
		original := in.RegisterFnWithReturnSources([]TypeID{ref}, ref, ExplicitReturnSources(0))
		assertReturnSourcesPanic(t, "arity changed from 1 to 0", func() {
			in.RebuildFn(original, nil, ref)
		})
	})
}

func assertReturnSourcesPanic(t *testing.T, want string, run func()) {
	t.Helper()
	defer func() {
		if recovered := recover(); recovered == nil || !strings.Contains(fmt.Sprint(recovered), want) {
			t.Fatalf("invariant panic = %v; want message containing %q", recovered, want)
		}
	}()
	run()
}
