package mir

import (
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// These nominal instances have no source declaration or visible symbol. The
// empty declaration span distinguishes the compiler-owned family from every
// parsed declaration, including a user type spelled Task under no_std.
func rootTaskTypes(in *types.Interner, payload types.TypeID) (task, result types.TypeID) {
	task = in.RegisterStructInstance(in.Strings.Intern("Task"), source.Span{}, []types.TypeID{payload})
	in.SetStructFields(task, []types.StructField{{Name: in.Strings.Intern("__opaque"), Type: in.Builtins().Int64}})
	in.MarkRuntimeHandleType(task)
	result = in.RegisterUnionInstance(in.Strings.Intern("TaskResult"), source.Span{}, []types.TypeID{payload})
	in.SetUnionMembers(result, []types.UnionMember{
		{Kind: types.UnionMemberTag, TagName: in.Strings.Intern("Success"), TagArgs: []types.TypeID{payload}},
		{Kind: types.UnionMemberTag, TagName: in.Strings.Intern("Cancelled")},
	})
	return task, result
}

func registerRootResultLayouts(m *Module, in *types.Interner, table *symbols.Table) error {
	seen := make(map[types.TypeID]bool)
	for _, id := range m.SortedFuncIDs() {
		fn := m.Funcs[id]
		if fn == nil || fn.AsyncTaskType == types.NoTypeID {
			continue
		}
		_, result := rootTaskTypes(in, fn.Result)
		if seen[result] {
			continue
		}
		seen[result] = true
		if m.Meta.TagNames == nil {
			m.Meta.TagNames = make(map[symbols.SymbolID]string)
		}
		tags := make(map[string]symbols.SymbolID, 2)
		for _, name := range []string{"Success", "Cancelled"} {
			sym := nextSyntheticTagSym(m, table)
			m.Meta.TagNames[sym] = name
			tags[name] = sym
		}
		delete(m.Meta.TagLayouts, result)
		if err := ensureTagLayout(m, in, tags, result); err != nil {
			return err
		}
	}
	return nil
}
