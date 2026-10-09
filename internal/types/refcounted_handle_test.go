package types //nolint:revive

import (
	"testing"

	"surge/internal/source"
)

func TestRefCountedHandleFollowsDeclarationIdentity(t *testing.T) {
	in := NewInterner()
	in.Strings = source.NewInterner()
	name := in.Strings.Intern("Channel")
	core := source.Span{File: 1, Start: 10, End: 20}
	other := source.Span{File: 2, Start: 10, End: 20}
	family := in.RegisterStruct(name, core)
	before := in.RegisterStructInstance(name, core, []TypeID{in.Builtins().Int64})
	lookalike := in.RegisterStructInstance(name, other, []TypeID{in.Builtins().Int64})
	in.MarkRefCountedHandleType(family)
	after := in.RegisterStructInstance(name, core, []TypeID{in.Builtins().String})
	alias := in.RegisterAlias(in.Strings.Intern("Pipe"), source.Span{})
	in.SetAliasTarget(alias, after)
	for _, row := range []struct {
		name string
		id   TypeID
		want bool
	}{
		{"family", family, true}, {"before_mark", before, true}, {"after_mark", after, true},
		{"alias", alias, true}, {"own", in.Intern(MakeOwn(after)), true},
		{"same_name_other_declaration", lookalike, false},
		{"shared_reference", in.Intern(MakeReference(after, false)), false},
		{"mutable_reference", in.Intern(MakeReference(after, true)), false},
	} {
		t.Run(row.name, func(t *testing.T) {
			if got := in.IsRefCountedHandle(row.id); got != row.want {
				t.Fatalf("counted handle=%v, want %v", got, row.want)
			}
		})
	}
}
