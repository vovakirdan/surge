package mir_test

import (
	"strings"
	"testing"

	"surge/internal/mir"
	"surge/internal/types"
)

// A value built by a tag constructor is typed by that constructor's own union,
// whose only case is the tag. `nothing` is still an accepted arm against it, so
// the test for it has to be answered where the case list is known -- in MIR --
// and never reach a backend as a case the union does not have.
//
// A reference handed to a by-value parameter instantiated with a reference
// type is the value itself: the call must receive the address, not the bits
// behind it.
const tagCaseMembershipSource = `
tag Wrap<T>(T);
type Maybe<T> = Wrap(T) | nothing;
type Box = { v: int };

fn idg<T>(x: T) -> T { return x; }

fn bare_wrap() -> int {
    let e = Wrap(7);
    return compare e {
        Wrap(v) => v;
        nothing => 0;
    };
}

fn only_nothing() -> int {
    let e = Wrap(7);
    return compare e {
        nothing => 0;
        _ => 1;
    };
}

fn declared_maybe() -> int {
    let e: Maybe<int> = Wrap(7);
    return compare e {
        Wrap(v) => v;
        nothing => 0;
    };
}

fn wrap_ref(b: &mut Box) -> Maybe<&mut int> {
    return Wrap::<&mut int>(&mut b.v);
}

fn id_ref(b: &mut Box) -> int {
    let q = idg::<&mut int>(&mut b.v);
    *q = 9;
    return 0;
}

fn ternary_join(c: bool) -> Maybe<string> {
    let e: Maybe<string> = c ? Wrap("s") : nothing;
    return e;
}

fn main() -> int { return 0; }
`

func compileTagCaseMembership(t *testing.T) crossingMIRCompileResult {
	t.Helper()
	compiled := compileCrossingMIR(t, tagCaseMembershipSource, nil)
	for _, id := range compiled.mod.SortedFuncIDs() {
		if f := compiled.mod.Funcs[id]; f != nil {
			mir.SimplifyCFG(f)
			mir.RecognizeSwitchTag(f)
			mir.SimplifyCFG(f)
		}
	}
	return compiled
}

func dumpTagModule(compiled crossingMIRCompileResult) string {
	var b strings.Builder
	if err := mir.DumpModule(&b, compiled.mod, compiled.types, mir.DumpOptions{}); err != nil {
		return err.Error()
	}
	return b.String()
}

func funcNamed(t *testing.T, mod *mir.Module, name string) *mir.Func {
	t.Helper()
	for _, id := range mod.SortedFuncIDs() {
		if f := mod.Funcs[id]; f != nil && f.Name == name {
			return f
		}
	}
	t.Fatalf("no function named %q in module", name)
	return nil
}

// testedTags lists every case name a tag test or tag switch in fn asks about.
func testedTags(f *mir.Func) []string {
	var out []string
	for bi := range f.Blocks {
		bb := &f.Blocks[bi]
		for ii := range bb.Instrs {
			ins := &bb.Instrs[ii]
			if ins.Kind == mir.InstrAssign && ins.Assign.Src.Kind == mir.RValueTagTest {
				out = append(out, ins.Assign.Src.TagTest.TagName)
			}
		}
		if bb.Term.Kind == mir.TermSwitchTag {
			for _, c := range bb.Term.SwitchTag.Cases {
				out = append(out, c.TagName)
			}
		}
	}
	return out
}

func TestTagTestNamesOnlyCasesTheValueHas(t *testing.T) {
	compiled := compileTagCaseMembership(t)
	mod := compiled.mod
	validation := mir.ValidateStructure(mod, compiled.types)
	for _, row := range []struct {
		fn   string
		want string
	}{
		{"bare_wrap", "Wrap"},
		{"only_nothing", ""},
		{"declared_maybe", "Wrap,nothing"},
	} {
		t.Run(row.fn, func(t *testing.T) {
			if validation != nil {
				t.Fatalf("MIR validation: %v", validation)
			}
			got := strings.Join(testedTags(funcNamed(t, mod, row.fn)), ",")
			if got != row.want {
				t.Fatalf("%s tests cases [%s], want [%s]\n%s", row.fn, got, row.want, dumpTagModule(compiled))
			}
		})
	}
}

func TestValidationRefusesATagCaseTheUnionLacks(t *testing.T) {
	compiled := compileCrossingMIR(t, tagCaseMembershipSource, nil)
	f := funcNamed(t, compiled.mod, "bare_wrap")
	renamed := false
	for bi := range f.Blocks {
		for ii := range f.Blocks[bi].Instrs {
			ins := &f.Blocks[bi].Instrs[ii]
			if ins.Kind == mir.InstrAssign && ins.Assign.Src.Kind == mir.RValueTagTest {
				ins.Assign.Src.TagTest.TagName = "nothing"
				renamed = true
			}
		}
	}
	if !renamed {
		t.Fatalf("bare_wrap has no tag test to rename\n%s", dumpTagModule(compiled))
	}
	err := mir.ValidateStructure(compiled.mod, compiled.types)
	if err == nil {
		t.Fatal("validation accepted a tag test for `nothing` on a Wrap<int>")
	}
	for _, want := range []string{"function bare_wrap", `names case "nothing"`, "cases: Wrap", "__cmp"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("validation error lacks %q:\n%v", want, err)
		}
	}
}

func TestReferenceTypeArgumentReachesTheCallAsAnAddress(t *testing.T) {
	compiled := compileTagCaseMembership(t)
	mod := compiled.mod
	for _, fn := range []string{"wrap_ref", "id_ref"} {
		t.Run(fn, func(t *testing.T) {
			f := funcNamed(t, mod, fn)
			for bi := range f.Blocks {
				for ii := range f.Blocks[bi].Instrs {
					ins := &f.Blocks[bi].Instrs[ii]
					if ins.Kind != mir.InstrAssign || ins.Assign.Src.Kind != mir.RValueUnaryOp {
						continue
					}
					t.Fatalf("%s reads through the reference it passes:\n%s", fn, dumpTagModule(compiled))
				}
			}
			calls := 0
			for bi := range f.Blocks {
				for ii := range f.Blocks[bi].Instrs {
					ins := &f.Blocks[bi].Instrs[ii]
					if ins.Kind != mir.InstrCall || len(ins.Call.Args) != 1 {
						continue
					}
					calls++
					if kind := ins.Call.Args[0].Kind; kind != mir.OperandAddrOfMut {
						t.Fatalf("%s passes operand kind %v, want the address\n%s", fn, kind, dumpTagModule(compiled))
					}
				}
			}
			if calls == 0 {
				t.Fatalf("%s makes no one-argument call\n%s", fn, dumpTagModule(compiled))
			}
		})
	}
}

func TestValidationRefusesNothingStoredInAUnionWithoutIt(t *testing.T) {
	compiled := compileCrossingMIR(t, tagCaseMembershipSource, nil)
	f := funcNamed(t, compiled.mod, "bare_wrap")
	target := mir.NoLocalID
	for i := range f.Locals {
		if f.Locals[i].Name == "e" {
			target = mir.LocalID(i)
		}
	}
	if target == mir.NoLocalID || len(f.Blocks) == 0 {
		t.Fatalf("bare_wrap has no local e\n%s", dumpTagModule(compiled))
	}
	f.Blocks[0].Instrs = append([]mir.Instr{{
		Kind: mir.InstrAssign,
		Assign: mir.AssignInstr{
			Dst: mir.Place{Local: target},
			Src: mir.RValue{Kind: mir.RValueUse, Use: mir.Operand{Kind: mir.OperandConst, Const: mir.Const{Kind: mir.ConstNothing}}},
		},
	}}, f.Blocks[0].Instrs...)
	err := mir.ValidateStructure(compiled.mod, compiled.types)
	if err == nil || !strings.Contains(err.Error(), `stores nothing: e at`) || !strings.Contains(err.Error(), "cases: Wrap") {
		t.Fatalf("validation did not refuse `nothing` stored in a Wrap<int>: %v", err)
	}
}

// The `nothing` a choice's branch yields is a value of the choice's union, as a
// compare arm's is: returned, it is owned like the other branch's value.
func TestNothingBranchOfAChoiceIsAnOwnedUnionValue(t *testing.T) {
	compiled := compileTagCaseMembership(t)
	f := funcNamed(t, compiled.mod, "ternary_join")
	for _, finding := range mir.VerifyOwnership(compiled.mod, compiled.types, compiled.sema) {
		if finding.Function == f.Name {
			t.Fatalf("ownership finding in %s: %+v\n%s", f.Name, finding, dumpTagModule(compiled))
		}
	}
}

// A union case and a tag layout that carry a reference record the reference,
// not the referent: the drop glue reads the membership and the constructor and
// payload paths read the layout, and a referent type there frees what the
// payload only points at, or stores the referent's bytes where its address
// belongs.
func TestReferencePayloadTypesStayReferences(t *testing.T) {
	compiled := compileTagCaseMembership(t)
	isRef := func(id types.TypeID) bool {
		tt, ok := compiled.types.Lookup(id)
		return ok && tt.Kind == types.KindReference
	}
	var unionRef, layoutRef bool
	for _, cases := range compiled.mod.Meta.UnionCases {
		for _, c := range cases {
			for _, pt := range c.PayloadTypes {
				unionRef = unionRef || (c.Name == "Wrap" && isRef(pt))
			}
		}
	}
	for _, cases := range compiled.mod.Meta.TagLayouts {
		for _, c := range cases {
			for _, pt := range c.PayloadTypes {
				layoutRef = layoutRef || (c.TagName == "Wrap" && isRef(pt))
			}
		}
	}
	t.Run("union_membership", func(t *testing.T) {
		if !unionRef {
			t.Fatalf("no Wrap union case records a reference payload\n%s", dumpTagModule(compiled))
		}
	})
	t.Run("tag_layout", func(t *testing.T) {
		if !layoutRef {
			t.Fatalf("no Wrap tag layout records a reference payload\n%s", dumpTagModule(compiled))
		}
	})
}
