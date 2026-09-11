package llvm

import (
	"regexp"
	"strings"
	"testing"

	"surge/internal/source"
	"surge/internal/types"
)

// Float's instruction sequences are the pre-integer implementation verbatim.
// Compare the actual emitted bytes, rather than claiming equivalence because a
// new predicate still mentions float somewhere.
func TestEmitNumericLifecyclePreservesFloatIR(t *testing.T) {
	for _, operation := range []string{"retain", "drop", "unshare", "cross-clone"} {
		t.Run(operation, func(t *testing.T) {
			e := &Emitter{types: types.NewInterner()}
			id := e.types.Builtins().Float
			var want string
			switch operation {
			case "retain":
				fe := &funcEmitter{emitter: e}
				fe.emitRetainValue("%value", "ptr", id)
				want = "  %t1 = icmp eq ptr %value, null\n" +
					"  %t2 = select i1 %t1, ptr @__surge_rc_scratch, ptr %value\n" +
					"  %t3 = load i32, ptr %t2\n" +
					"  %t4 = add i32 %t3, 1\n" +
					"  store i32 %t4, ptr %t2\n"
				if got := e.buf.String(); got != want {
					t.Fatalf("float inline retain changed:\n%s\nwant:\n%s", got, want)
				}
				e.buf.Reset()
				e.emitLeafCloneAt(&glueTmp{}, id, 8, 0)
				want = "  %g1 = getelementptr inbounds i8, ptr %dst, i64 0\n" +
					"  %g2 = load ptr, ptr %g1, align 8\n" +
					"  %g3 = icmp eq ptr %g2, null\n" +
					"  %g4 = select i1 %g3, ptr @__surge_rc_scratch, ptr %g2\n" +
					"  %g5 = load i32, ptr %g4\n" +
					"  %g6 = add i32 %g5, 1\n" +
					"  store i32 %g6, ptr %g4\n"
			case "drop":
				e.emitDropHandle(&glueTmp{}, "%value", id)
				want = "  call void @rt_bigfloat_release(ptr %value)\n"
			case "unshare", "cross-clone":
				base, entry := "%val", "unshare"
				if operation == "unshare" {
					var err error
					if !(unshareWalk{e: e, err: &err}).leafAt(&glueTmp{}, id, 8, 0) || err != nil {
						t.Fatalf("float unshare was not handled: %v", err)
					}
				} else {
					base, entry = "%dst", "clone"
					if !(crossCloneWalk{e: e}).leafAt(&glueTmp{}, id, 8, 0) {
						t.Fatal("float cross clone was not handled")
					}
				}
				want = "  %g1 = getelementptr inbounds i8, ptr " + base + ", i64 0\n" +
					"  %g2 = load ptr, ptr %g1, align 8\n" +
					"  %g3 = call ptr @rt_bigfloat_" + entry + "(ptr %g2)\n" +
					"  store ptr %g3, ptr %g1, align 8\n"
			}
			if got := e.buf.String(); got != want {
				t.Fatalf("float %s changed:\n%s\nwant:\n%s", operation, got, want)
			}
		})
	}
}

// References are tested at the owning leaf boundary. Source aggregates cannot
// store references (SEM3138), so this does not claim a borrowed aggregate is a
// supported or reachable value walk.
func TestEmitNumericLifecycleAliasesAndNonOwningControls(t *testing.T) {
	for _, kind := range []string{"int", "uint"} {
		for _, form := range []string{"alias", "own", "ref", "fixed64"} {
			t.Run(kind+"/"+form, func(t *testing.T) {
				in := types.NewInterner()
				in.Strings = source.NewInterner()
				id, fixed := in.Builtins().Int, in.Builtins().Int64
				if kind == "uint" {
					id, fixed = in.Builtins().Uint, in.Builtins().Uint64
				}
				switch form {
				case "alias":
					alias := in.RegisterAlias(in.Strings.Intern("NumericAlias"), source.Span{})
					in.SetAliasTarget(alias, id)
					id = alias
				case "own":
					id = in.Intern(types.MakeOwn(id))
				case "ref":
					id = in.Intern(types.MakeReference(id, false))
				case "fixed64":
					id = fixed
				}
				e := &Emitter{types: in}
				fe := &funcEmitter{emitter: e}
				fe.emitRetainValue("%value", "ptr", id)
				e.emitDropHandle(&glueTmp{}, "%value", id)
				if form == "ref" || form == "fixed64" {
					if e.buf.Len() != 0 {
						t.Fatalf("nonowning %s emitted numeric lifecycle:\n%s", form, e.buf.String())
					}
					return
				}
				body := "define void @control(ptr %value) {\nentry:\n" + e.buf.String() + "  ret void\n}\n"
				counts := assertNumericHeapGuards(t, body, kind)
				if counts.retain != 1 || counts.release != 1 {
					t.Fatalf("%s does not own exactly one retain/release pair: %+v", form, counts)
				}
			})
		}
	}
}

func assertNumericOneRelease(t *testing.T, body, kind, constructor string) {
	t.Helper()
	makeOne := regexp.MustCompile(`(%[\w.]+) = call ptr @` + constructor + `\(i64 1\)`)
	ones := makeOne.FindAllStringSubmatch(body, -1)
	if len(ones) != 1 {
		t.Fatalf("%s bounds step creates one %d times, want once:\n%s", kind, len(ones), body)
	}
	want := 0
	if kind == "float" {
		want = 1
	}
	call := "call void @rt_big" + kind + "_release(ptr " + ones[0][1] + ")"
	if got := strings.Count(body, call); got != want {
		t.Fatalf("%s bounds step releases one %d times, want %d:\n%s", kind, got, want, body)
	}
}
