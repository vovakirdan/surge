package types //nolint:revive

import (
	"testing"

	"surge/internal/source"
)

func TestRefCountedNumericScalarKindsAndQualifiers(t *testing.T) {
	in := NewInterner()
	in.Strings = source.NewInterner()
	b := in.Builtins()
	type numericRow struct {
		name    string
		id      TypeID
		scalar  bool
		counted bool
	}
	rows := []numericRow{
		{"int", b.Int, true, true}, {"uint", b.Uint, true, true}, {"float", b.Float, true, true},
		{"int64", b.Int64, false, false}, {"uint64", b.Uint64, false, false},
		{"float64", b.Float64, false, false}, {"bool", b.Bool, false, false},
	}
	for _, kind := range []struct {
		name string
		id   TypeID
	}{{"int", b.Int}, {"uint", b.Uint}, {"float", b.Float}} {
		alias := in.RegisterAlias(in.Strings.Intern("Alias_"+kind.name), source.Span{})
		in.SetAliasTarget(alias, kind.id)
		rows = append(rows,
			numericRow{"alias_" + kind.name, alias, false, true},
			numericRow{"own_" + kind.name, in.Intern(MakeOwn(kind.id)), false, true},
			numericRow{"ref_" + kind.name, in.Intern(MakeReference(kind.id, false)), false, false},
		)
	}
	for _, kind := range []struct {
		name string
		id   TypeID
	}{{"int", b.Int}, {"uint", b.Uint}} {
		rows = append(rows, numericRow{
			"pointer_" + kind.name, in.Intern(MakePointer(kind.id)), false, false,
		})
	}
	if len(rows) != 18 {
		t.Fatalf("numeric type census: got %d rows, want 18", len(rows))
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			if got := in.IsRefCountedScalar(row.id); got != row.scalar {
				t.Errorf("raw scalar predicate = %v, want %v", got, row.scalar)
			}
			if got := in.IsRefCounted(row.id); got != row.counted {
				t.Errorf("ownership predicate = %v, want %v", got, row.counted)
			}
			if !in.IsCopy(row.id) {
				t.Error("numeric ownership must preserve Copy")
			}
		})
	}
	if in.IsRefCountedScalar(NoTypeID) || (*Interner)(nil).IsRefCountedScalar(b.Int) {
		t.Fatal("an absent type/interner cannot own a count")
	}
}
