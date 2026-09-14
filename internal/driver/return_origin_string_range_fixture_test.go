package driver

import (
	"crypto/sha256"
	"fmt"
	"maps"
	"slices"
	"testing"

	"surge/internal/ast"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/types"
)

func checkReturnOriginRangeSites(t *testing.T, res *DiagnoseResult, units []sema.ReturnOriginUnit, name string) returnOriginRangeFacts {
	t.Helper()
	facts := returnOriginRangeFacts{root: -1, sites: make(map[source.Span]string)}
	spans := map[string][][2]uint32{"core/format.sg": {{2466, 2485}, {2741, 2760}, {3353, 3372}, {3669, 3694}},
		"core/string.sg": {{4202, 4218}, {4555, 4573}, {4686, 4708}, {5858, 5880}, {6193, 6214}, {6719, 6737}, {7312, 7330}, {7480, 7502}, {7812, 7828}}}
	hashes := map[string]string{"core/format.sg": "d36017c22af545db77f76d7672b42a8deaf10bffa7031b97b5ffbebd2f87e1f6",
		"core/intrinsics.sg": "ef9d4c657599262b678e9f59640e545936bb3d87ea70e173d145f3abccdfe060",
		"core/string.sg":     "c4beccf20672687ebb3be2da59c0a180435271e350b830f36516990523e7c020", "core/array.sg": "532a6cd1bc46d2d665d71f13f988358e42fe30dd39810117278afd46b967ffbc"}
	var intrinsicFile source.FileID
	for _, u := range units {
		if u.SourceKey == "core/intrinsics.sg" {
			intrinsicFile = u.Builder.Files.Get(u.FileID).Span.File
		}
	}
	native, children, arrays := 0, 0, 0
	constructors := make(map[string]int)
	in := res.Sema.TypeInterner
	for ui, u := range units {
		file := res.FileSet.Get(u.Builder.Files.Get(u.FileID).Span.File)
		isRoot := file.ID == res.File.ID
		if isRoot {
			facts.root = ui
		}
		if want := hashes[u.SourceKey]; want != "" && fmt.Sprintf("%x", sha256.Sum256(file.Content)) != want {
			t.Fatal("PRECONDITION: frozen core source changed")
		}
		for raw := uint32(1); raw <= u.Builder.Exprs.Arena.Len(); raw++ {
			id := ast.ExprID(raw)
			node := u.Builder.Exprs.Get(id)
			if node == nil || node.Span.File != file.ID {
				continue
			}
			if node.Kind == ast.ExprIndex {
				idx, _ := u.Builder.Exprs.Index(id)
				selected, present := u.Sema.IndexSymbols[id]
				array := u.SourceKey == "core/array.sg" && node.Span.Start == 2922 && node.Span.End == 2929
				reviewed := isRoot || array || slices.Contains(spans[u.SourceKey], [2]uint32{node.Span.Start, node.Span.End})
				if !reviewed {
					continue
				}
				if idx == nil || u.Sema.ExprTypes[id] == types.NoTypeID || u.Sema.ExprTypes[idx.Index] == types.NoTypeID {
					t.Fatal("PRECONDITION: range index is untyped")
				}
				targetID := u.Sema.ExprTypes[idx.Target]
				target, targetOK := in.Lookup(targetID)
				if targetOK && target.Kind == types.KindReference {
					targetID = target.Elem
				}
				rangeInfo, rangeOK := in.StructInfo(u.Sema.ExprTypes[idx.Index])
				if !targetOK || !rangeOK || rangeInfo == nil || rangeInfo.Decl.Start != 6599 || rangeInfo.Decl.End != 6654 ||
					!slices.Equal(rangeInfo.TypeArgs, []types.TypeID{in.Builtins().Int}) || rangeInfo.Decl.File != intrinsicFile {
					t.Fatal("PRECONDITION: index lost concrete target or original core Range<int> type")
				}
				if !array && (!isRoot || name != "foreign_selected_range") && targetID != in.Builtins().String {
					t.Fatal("PRECONDITION: range subject is not string or &string")
				}
				if !isRoot && present {
					t.Fatal("PRECONDITION: native core index acquired selected authority")
				}
				if array {
					payload, typed := in.StructInfo(targetID)
					registered, _ := in.StructInfo(in.ArrayNominalType())
					if !typed || payload == nil || registered == nil || payload.Name != registered.Name || payload.Decl != registered.Decl ||
						len(payload.TypeArgs) != 1 || target.Kind != types.KindReference || target.Mutable || u.Sema.ExprTypes[id] != targetID || u.Builder.Exprs.Get(idx.Index).Kind != ast.ExprIdent {
						t.Fatal("PRECONDITION: separate Array<T> range lost exact payload-preserving result")
					}
					arrays++
					facts.array = sema.ReturnOriginPending{SourceKey: u.SourceKey, Span: node.Span, Reason: "index requires a non-scalar index transfer"}
				} else {
					if !isRoot && u.Builder.Exprs.Get(idx.Index).Kind != ast.ExprRangeLit {
						t.Fatal("PRECONDITION: native string index lost its actual range-literal child")
					}
					facts.sites[node.Span] = u.SourceKey
					if u.Sema.ExprTypes[id] != in.Builtins().String {
						t.Fatal("PRECONDITION: range index result is not owning string")
					}
					if isRoot {
						facts.indexes = append(facts.indexes, id)
					} else {
						native++
					}
				}
				if isRoot {
					c := checkReturnOriginRangeCallable(t, res, units, u, selected)
					foreign := name == "foreign_selected_range"
					if !present || c.Name != "__index" || !c.HasSelf || c.ResultType != in.Builtins().String || c.Builtin == foreign || c.HasBody != foreign || c.Intrinsic == foreign || len(c.ParamTypes) != 2 || c.ParamTypes[1] != u.Sema.ExprTypes[idx.Index] || len(c.TemplateParams) != 0 {
						t.Fatal("PRECONDITION: root lost exact selected range index")
					}
					self, typed := in.Lookup(c.ParamTypes[0])
					if !typed || self.Kind != types.KindReference || self.Mutable || self.Elem != targetID {
						t.Fatal("PRECONDITION: selected range receiver differs from actual source subject")
					}
				}
				logReturnOriginCallEvidence(t, map[string]any{"range_index": id, "source_key": u.SourceKey, "span": node.Span, "ast": idx, "target_type": u.Sema.ExprTypes[idx.Target], "index_type": u.Sema.ExprTypes[idx.Index], "result_type": u.Sema.ExprTypes[id], "selected": selected, "selected_present": present, "separate_array": array})
			}
			if node.Kind != ast.ExprRangeLit {
				continue
			}
			rng, _ := u.Builder.Exprs.RangeLit(id)
			if !isRoot && !slices.ContainsFunc(spans[u.SourceKey], func(s [2]uint32) bool { return node.Span.Start > s[0] && node.Span.End < s[1] }) {
				continue
			}
			if rng == nil {
				t.Fatal("PRECONDITION: missing literal payload")
			}
			facts.sites[node.Span] = u.SourceKey
			if isRoot {
				facts.literals = append(facts.literals, id)
			} else {
				children++
			}
			want, params := "rt_range_int_full", []types.TypeID{}
			for _, bound := range []ast.ExprID{rng.Start, rng.End} {
				if bound.IsValid() {
					if u.Sema.ExprTypes[bound] != in.Builtins().Int {
						t.Fatal("PRECONDITION: range bound is not int")
					}
					params = append(params, in.Builtins().Int)
				}
			}
			if rng.Start.IsValid() && rng.End.IsValid() {
				want = "rt_range_int_new"
			} else if rng.Start.IsValid() {
				want = "rt_range_int_from_start"
			} else if rng.End.IsValid() {
				want = "rt_range_int_to_end"
			}
			params = append(params, in.Builtins().Bool)
			c := checkReturnOriginRangeCallable(t, res, units, u, u.Symbols.ExprSymbols[id])
			if c.Name != want || c.HasSelf || c.HasBody || !c.Builtin || !c.Intrinsic || !slices.Equal(c.ParamTypes, params) || c.ResultType != u.Sema.ExprTypes[id] || len(c.TemplateParams) != 0 {
				t.Fatal("PRECONDITION: literal lost exact original constructor signature")
			}
			if isRoot {
				constructors[want]++
			}
			logReturnOriginCallEvidence(t, map[string]any{"range_literal": id, "source_key": u.SourceKey, "span": node.Span, "ast": rng, "constructor": c})
		}
	}
	if facts.root < 0 || native != 13 || children != 13 || arrays != 1 {
		t.Fatalf("PRECONDITION: native range census root%d/string%d/children%d/array%d", facts.root, native, children, arrays)
	}
	wantIndexes, wantLiterals := 1, 1
	if name == "string_range_forms" {
		wantIndexes, wantLiterals = 8, 7
		if !maps.Equal(constructors, map[string]int{"rt_range_int_new": 2, "rt_range_int_from_start": 2, "rt_range_int_to_end": 2, "rt_range_int_full": 1}) {
			t.Fatal("PRECONDITION: missing-bound constructor census changed")
		}
	}
	if name == "foreign_selected_range" {
		wantLiterals = 0
	}
	if len(facts.indexes) != wantIndexes || len(facts.literals) != wantLiterals {
		t.Fatal("PRECONDITION: root index/literal census changed")
	}
	return facts
}
