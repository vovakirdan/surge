package sema

import (
	"fmt"
	"strings"
	"testing"

	"surge/internal/source"
	"surge/internal/types"
)

type countedDiagnosticTypes struct {
	res   *Result
	in    *types.Interner
	names *source.Interner
}

func newCountedDiagnosticTypes(t *testing.T) countedDiagnosticTypes {
	t.Helper()
	in := types.NewInterner()
	in.Strings = source.NewInterner()
	if id, _ := in.EnsureMapNominal(in.Strings.Intern("Map"), in.Strings.Intern("K"), in.Strings.Intern("V"), source.Span{}, 0); id == types.NoTypeID {
		t.Fatal("missing authoritative Map identity")
	}
	for _, name := range []string{"Channel", "Task", "Range"} {
		in.MarkRuntimeHandleType(in.RegisterStruct(in.Strings.Intern(name), source.Span{}))
	}
	return countedDiagnosticTypes{res: &Result{TypeInterner: in}, in: in, names: in.Strings}
}

func (f countedDiagnosticTypes) handle(name string, payloads ...types.TypeID) types.TypeID {
	return f.in.RegisterStructInstance(f.names.Intern(name), source.Span{}, payloads)
}

func (f countedDiagnosticTypes) structure(name string, fields ...types.StructField) types.TypeID {
	id := f.in.RegisterStruct(f.names.Intern(name), source.Span{})
	f.in.SetStructFields(id, fields)
	return id
}

func (f countedDiagnosticTypes) field(name string, id types.TypeID) types.StructField {
	return types.StructField{Name: f.names.Intern(name), Type: id}
}

func TestCountedBlockRefusalPaths(t *testing.T) {
	f := newCountedDiagnosticTypes(t)
	b := f.in.Builtins()
	channelInt := f.handle("Channel", b.Int)
	channelUint := f.handle("Channel", b.Uint)
	channelFloat := f.handle("Channel", b.Float)
	mapBoth := f.handle("Map", b.Int, b.Float)
	meta := f.structure("Meta", f.field("counts", f.handle("Map", b.Uint, b.Int)))
	outer := f.structure("Outer", f.field("meta", meta))
	tagged := f.in.RegisterUnion(f.names.Intern("Tagged"), source.Span{})
	f.in.SetUnionMembers(tagged, []types.UnionMember{
		{Kind: types.UnionMemberTag, TagName: f.names.Intern("Held"), TagArgs: []types.TypeID{f.handle("Map", b.Uint, b.Float)}},
		{Kind: types.UnionMemberNothing},
	})
	union := f.in.RegisterUnion(f.names.Intern("Either"), source.Span{})
	f.in.SetUnionMembers(union, []types.UnionMember{{Kind: types.UnionMemberType, Type: b.Bool}, {Kind: types.UnionMemberType, Type: mapBoth}})
	alias := f.in.RegisterAlias(f.names.Intern("Aliased"), source.Span{})
	f.in.SetAliasTarget(alias, f.in.Intern(types.MakeOwn(channelFloat)))
	shared := f.structure("Shared", f.field("inline", b.Int), f.field("boxed", channelInt))
	rows := []struct {
		name   string
		id     types.TypeID
		path   string
		kind   types.Kind
		widths uint8
	}{
		{"channel_int", channelInt, "payload[0]", types.KindInt, 1},
		{"channel_uint", channelUint, "payload[0]", types.KindUint, 2},
		{"channel_float", channelFloat, "payload[0]", types.KindFloat, 4},
		{"map_key_before_value", mapBoth, "key", types.KindInt, 5},
		{"nested_struct", outer, "meta.counts.key", types.KindUint, 3},
		{"tuple", f.in.RegisterTuple([]types.TypeID{b.Bool, channelFloat}), "1.payload[0]", types.KindFloat, 4},
		{"fixed_array", f.in.Intern(types.MakeArray(channelUint, 2)), "element.payload[0]", types.KindUint, 2},
		{"dynamic_array", f.in.Intern(types.MakeArray(channelInt, types.ArrayDynamicLength)), "element.payload[0]", types.KindInt, 1},
		{"tag_payload", tagged, "Held[0].key", types.KindUint, 6},
		{"union_type_member", union, "member[1].key", types.KindInt, 5},
		{"alias_and_own", alias, "payload[0]", types.KindFloat, 4},
		{"same_type_inline_and_behind_handle", shared, "boxed.payload[0]", types.KindInt, 1},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			if !f.res.CountedBlockStaysShared(row.id) {
				t.Fatal("fixture never reaches the actual counted refusal")
			}
			got := f.res.countedBlockCulprit(f.names, row.id)
			want := countedBlockCulprit{path: row.path, kind: row.kind, widths: row.widths, complete: true}
			if got != want {
				t.Fatalf("culprit = %+v, want %+v", got, want)
			}
			message := f.res.CountedBlockRefusalMessage(f.names, row.id, "the value cannot cross", true)
			for _, want := range []string{fmt.Sprintf("`%s` at `%s`", row.kind, row.path), "cannot be made private", "If fixed precision is sufficient"} {
				if !strings.Contains(message, want) {
					t.Errorf("message %q lacks %q", message, want)
				}
			}
			for _, kind := range []types.Kind{types.KindInt, types.KindUint, types.KindFloat} {
				mapping := fmt.Sprintf("`%s` with `%s64`", kind, kind)
				if strings.Contains(message, mapping) != (row.widths&countedNumericWidth(kind) != 0) {
					t.Errorf("wrong repair set for %q: %s", mapping, message)
				}
			}
		})
	}
}

func TestCountedBlockRefusalPlainAndReachable(t *testing.T) {
	f := newCountedDiagnosticTypes(t)
	b := f.in.Builtins()
	inline := f.structure("Inline", f.field("i", b.Int), f.field("u", b.Uint), f.field("f", b.Float))
	placement := f.in.RegisterStruct(f.names.Intern("Placement"), source.Span{})
	f.in.SetStructFields(placement, []types.StructField{f.field("__opaque", b.Int)})
	f.in.MarkRuntimePlacementType(placement)
	channel := f.handle("Channel", b.Int)
	rows := []struct {
		name string
		ids  []types.TypeID
	}{
		{"numeric_scalars", []types.TypeID{b.Int, b.Uint, b.Float}},
		{"inline_composites", []types.TypeID{inline, f.in.RegisterTuple([]types.TypeID{b.Int, b.Float}), f.in.Intern(types.MakeArray(b.Uint, 2))}},
		{"dynamic_arrays", []types.TypeID{f.in.Intern(types.MakeArray(inline, types.ArrayDynamicLength))}},
		{"range_bounds", []types.TypeID{f.handle("Range", b.Int), f.handle("Range", b.Uint), f.handle("Range", b.Float)}},
		{"non_owning_edges", []types.TypeID{f.in.Intern(types.MakeReference(channel, false)), f.in.Intern(types.MakePointer(channel)), f.in.Intern(types.MakeFar(channel))}},
		{"fixed_width_handles_and_placement", []types.TypeID{f.handle("Channel", b.Int64), f.handle("Channel", b.Uint64), f.handle("Channel", b.Float64), f.handle("Map", b.Int64, b.Float64), placement}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			for _, id := range row.ids {
				if got := f.res.countedBlockCulprit(f.names, id); got.widths != 0 || !got.complete {
					t.Errorf("%s: unexpected culprit %+v", types.Label(f.in, id), got)
				}
				if f.res.CountedBlockStaysShared(id) {
					t.Errorf("%s unexpectedly refused by the admission predicate", types.Label(f.in, id))
				}
			}
		})
	}
	// Being behind a channel remains decisive even when its payload is a range
	// whose own bounds would otherwise be reachable.
	got := f.res.countedBlockCulprit(f.names, f.handle("Channel", f.handle("Range", b.Int)))
	if got.path != "payload[0].bound" || got.widths != 1 || !got.complete {
		t.Fatalf("channel-held range lost its inaccessible boundary: %+v", got)
	}
}

func TestCountedBlockRefusalIncomplete(t *testing.T) {
	f := newCountedDiagnosticTypes(t)
	emptyUnion := f.in.RegisterUnion(f.names.Intern("Unknown"), source.Span{})
	cycle := f.structure("Cycle")
	f.in.SetStructFields(cycle, []types.StructField{f.field("next", cycle), f.field("counted", f.handle("Channel", f.in.Builtins().Int))})
	aliasCycle := f.in.RegisterAlias(f.names.Intern("AliasCycle"), source.Span{})
	f.in.SetAliasTarget(aliasCycle, aliasCycle)
	rows := []struct {
		name string
		ids  []types.TypeID
	}{
		{"no_type", []types.TypeID{types.NoTypeID}},
		{"invalid_type", []types.TypeID{types.TypeID(^uint32(0)), f.in.Intern(types.Type{Kind: types.KindGenericParam})}},
		{"missing_union_members", []types.TypeID{emptyUnion, f.in.Intern(types.Type{Kind: types.KindUnion, Payload: ^uint32(0)})}},
		{"recursive_type", []types.TypeID{cycle, aliasCycle}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			for _, id := range row.ids {
				if got := f.res.countedBlockCulprit(f.names, id); got.complete {
					t.Errorf("incomplete type %d reported complete: %+v", id, got)
				}
				message := f.res.CountedBlockRefusalMessage(f.names, id, "the value cannot cross", true)
				if strings.Contains(message, "replace ") || strings.Contains(message, "values themselves") {
					t.Errorf("incomplete type %d received an unproved repair: %s", id, message)
				}
			}
		})
	}
	if got := (*Result)(nil).countedBlockCulprit(nil, types.NoTypeID); got.complete {
		t.Fatal("nil result reported a complete type")
	}
}
