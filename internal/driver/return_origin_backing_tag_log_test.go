package driver

import (
	"slices"
	"testing"

	"surge/internal/ast"
	"surge/internal/sema"
	"surge/internal/symbols"
)

type storageTagOwner struct {
	unit  sema.ReturnOriginUnit
	local symbols.SymbolID
	cand  *symbols.Symbol
}

// logStorageTagOwnerSearch recomputes, from exported fields only, the predicates
// behind "tag constructor lacks its exact owning source declaration" (packet R4
// §3.10, §7.4). It is evidence for choosing the permitted edit set: it asserts
// only its own preconditions, never a verdict, and adds no RUN. Names are
// compared by StringID; both IDs and both texts are logged, never text-matched.
func logStorageTagOwnerSearch(t *testing.T, res *DiagnoseResult, units []sema.ReturnOriginUnit, root sema.ReturnOriginUnit, call ast.ExprID) {
	t.Helper()
	data, ok := root.Builder.Exprs.Call(call)
	selected := root.Symbols.ExprSymbols[call]
	sym := root.Symbols.Table.Symbols.Get(selected)
	if !ok || data == nil || sym == nil || sym.Kind != symbols.SymbolTag {
		t.Fatal("PRECONDITION: the tag owner log needs a selected tag constructor call")
	}
	site := root.Builder.Exprs.Get(call).Span
	record := func(stage string, fields map[string]any) {
		fields["tag_log"], fields["site"], fields["root_file"] = stage, site, res.File.ID
		logReturnOriginCallEvidence(t, fields)
	}
	target := root.Builder.Exprs.Get(data.Target)
	p0 := target != nil && target.Kind == ast.ExprIdent && root.Symbols.ExprSymbols[data.Target] == selected
	canonical, p1 := storageTagCanonical(root, selected)
	record("selected", map[string]any{"selected": selected, "symbol": sym, "p0": p0, "canonical": canonical, "p1": p1})
	var omega []storageTagOwner
	for _, unit := range units {
		file := unit.Builder.Files.Get(unit.FileID)
		gA, gB := unit.FileID == sym.Decl.ASTFile, file != nil && file.Span.File == sym.Decl.SourceFile
		record("unit", map[string]any{"source_key": unit.SourceKey, "g_a": gA, "g_b": gB})
		locals := unit.Publication.RootToLocalSymbols[canonical]
		if len(unit.Publication.RootToLocalSymbols) == 0 {
			locals = nil
			if unit.SourceKey == root.SourceKey {
				locals = []symbols.SymbolID{canonical}
			}
		}
		for _, local := range locals {
			cand := unit.Symbols.Table.Symbols.Get(local)
			if cand == nil {
				record("mapped_local", map[string]any{"source_key": unit.SourceKey, "local": local, "missing": true})
				continue
			}
			item, found := unit.Builder.Items.Tag(cand.Decl.Item)
			c1, lA := cand.Kind == symbols.SymbolTag, cand.Decl.ASTFile == unit.FileID
			lB := file != nil && file.Span.File == cand.Decl.SourceFile
			lI := slices.Contains(unit.Symbols.ItemSymbols[cand.Decl.Item], local)
			c4 := found && item != nil && item.Name == cand.Name && item.NameSpan == cand.Span
			c5 := unit.Symbols.Table.Scopes.Get(cand.Scope) != nil
			record("mapped_local", map[string]any{"source_key": unit.SourceKey, "local": local, "candidate": cand, "decl": cand.Decl,
				"c1": c1, "l_a": lA, "l_b": lB, "l_i": lI, "c4": c4, "item_found": found, "item": item, "c5": c5,
				"c2": cand.Decl == sym.Decl, "c3": cand.Span == sym.Span})
			if c1 && lA && lB && lI && c4 && c5 {
				omega = append(omega, storageTagOwner{unit: unit, local: local, cand: cand})
			}
		}
		if gA || gB {
			item, found := unit.Builder.Items.Tag(sym.Decl.Item)
			for _, local := range unit.Symbols.ItemSymbols[sym.Decl.Item] {
				record("head_loop", map[string]any{"source_key": unit.SourceKey, "local": local,
					"candidate": unit.Symbols.Table.Symbols.Get(local), "item_found": found, "item": item})
			}
		}
	}
	var truths []storageTagOwner
	for _, owner := range omega {
		if owner.unit.SourceKey == "core/option.sg" {
			truths = append(truths, owner)
		}
	}
	record("owners", map[string]any{"omega": len(omega), "true_owners": len(truths)})
	if len(truths) != 1 {
		return
	}
	truth := truths[0]
	original, file := truth.cand, truth.unit.Builder.Files.Get(truth.unit.FileID)
	rootName, rootKnown := root.Symbols.Table.Strings.Lookup(sym.Name)
	ownerName, ownerKnown := truth.unit.Symbols.Table.Strings.Lookup(original.Name)
	o3 := original.Signature != nil && sym.Signature != nil
	identity := make(map[string]bool)
	if o3 {
		a, b := original.Signature, sym.Signature
		identity["params_len"] = len(a.Params) == len(b.Params)
		identity["params"] = slices.Equal(a.Params, b.Params)
		identity["variadic"] = slices.Equal(a.Variadic, b.Variadic)
		identity["result"] = a.Result == b.Result
		identity["has_self"] = a.HasSelf == b.HasSelf
		identity["sources"] = a.ReturnSourceSyntax.Sources().Equal(b.ReturnSourceSyntax.Sources())
	}
	o4 := o3
	for _, same := range identity {
		o4 = o4 && same
	}
	predicates := map[string]bool{"G-a": truth.unit.FileID == sym.Decl.ASTFile,
		"G-b": file != nil && file.Span.File == sym.Decl.SourceFile,
		"C2":  original.Decl == sym.Decl, "C3": original.Span == sym.Span, "O4": o4}
	var falseSet, edits []string
	for _, name := range []string{"G-a", "G-b", "C2", "C3", "O4"} {
		if !predicates[name] {
			falseSet = append(falseSet, name)
		}
	}
	switch {
	case !predicates["G-a"] || !predicates["G-b"]:
		edits = append(edits, "E-A")
	case !predicates["C2"] || !predicates["C3"]:
		edits = append(edits, "E-B")
	}
	if !o4 {
		edits = append(edits, "E-C")
	}
	record("true_owner", map[string]any{"source_key": truth.unit.SourceKey, "local": truth.local, "original": original,
		"o2": original.Name == sym.Name, "o2_ids": []any{original.Name, sym.Name}, "o2_texts": []string{ownerName, rootName},
		"o2_texts_known": []bool{ownerKnown, rootKnown}, "o3": o3, "o4_fields": identity, "o4": o4,
		"o4t": original.Type == sym.Type, "false_set": falseSet, "edit_set": edits})
}

func storageTagCanonical(root sema.ReturnOriginUnit, selected symbols.SymbolID) (symbols.SymbolID, bool) {
	if len(root.Publication.RootToLocalSymbols) == 0 {
		return selected, selected.IsValid()
	}
	canonical, count := symbols.NoSymbolID, 0
	for key, locals := range root.Publication.RootToLocalSymbols {
		if slices.Contains(locals, selected) {
			canonical = key
			count++
		}
	}
	return canonical, count == 1 && canonical.IsValid()
}
