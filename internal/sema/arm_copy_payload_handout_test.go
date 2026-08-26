package sema

import (
	"fmt"
	"testing"

	"surge/internal/diag"
	"surge/internal/types"
)

// The code number is kicked LITERALLY, not through the constant: a test that
// reads the constant agrees with whatever the constant says.
const moveOutOfSharedBorrowCode = 3197

func TestMoveOutOfSharedBorrowCodeNumber(t *testing.T) {
	if got := int(diag.SemaMoveOutOfSharedBorrow); got != moveOutOfSharedBorrowCode {
		t.Fatalf("SemaMoveOutOfSharedBorrow is %d, want %d", got, moveOutOfSharedBorrowCode)
	}
}

// An arm that hands its payload binding out of a compare over a BORROWED union
// is refused (SEM3197) exactly when the payload owns heap — the union and the
// caller would both free it. The gate is the OwnsHeap axis, and this pins the
// gate's answer for the composite payloads RV2-DEBT-256 was filed on: the rule
// used to refuse a `@copy` pair of ints because every value composite counted
// as a heap box, and a pair of ints has nothing to double-free.
//
// One program shape, one condition varied per row. The refused rows are what
// make the accepted rows more than a vacuous pass: the same template, through
// the same harness, reaches the rule.
//
// The float row is the one the axis DECIDES rather than merely reports: a
// `float` is a reference-counted scalar, so a `@copy` pair of floats owns two
// counted blocks and the arm is refused. Whether that refusal is NECESSARY —
// the extraction of a `@copy` composite through a borrow duplicates it, and the
// duplicate retains its floats on the VM lane — is a question for the rule's
// escape hatch (`payloadTakesItsOwnReference`, bare scalars only today), not
// for the axis; the axis says what the value owns, and this row holds it to
// that.
func TestArmHandingOutPayloadFollowsTheOwnsHeapAxis(t *testing.T) {
	const shape = `
%s

tag Hold(%s);
tag Nothing_();
type Held = Hold(%s) | Nothing_;

fn payload_of(h: &Held) -> %s {
    return compare *h {
        Hold(p) => p;
        _ => %s;
    };
}
`
	rows := []struct {
		name     string
		decl     string
		typeName string
		fallback string
		refused  bool
	}{
		{
			name:     "copy pair of ints is handed out",
			decl:     "@copy type Pair = { a: int, b: int };",
			typeName: "Pair",
			fallback: "Pair { a = 0, b = 0 }",
			refused:  false,
		},
		{
			name:     "copy pair of bools is handed out",
			decl:     "@copy type Pair = { a: bool, b: bool };",
			typeName: "Pair",
			fallback: "Pair { a = false, b = true }",
			refused:  false,
		},
		{
			name:     "nested copy composite of ints is handed out",
			decl:     "@copy type Inner = { x: int };\n@copy type Pair = { inner: Inner, label: int };",
			typeName: "Pair",
			fallback: "Pair { inner = Inner { x = 0 }, label = 0 }",
			refused:  false,
		},
		{
			name:     "copy pair of floats owns two counted blocks",
			decl:     "@copy type Pair = { a: float, b: float };",
			typeName: "Pair",
			fallback: "Pair { a = 0.0, b = 0.0 }",
			refused:  true,
		},
		{
			// Spelled without `@copy` on purpose: `@copy type Tagged = { s:
			// string, n: int }` is refused at the declaration (a @copy field
			// must be Copy), and a row carrying two refusals pins neither.
			name:     "pair holding a string is refused",
			decl:     "type Tagged = { s: string, n: int };",
			typeName: "Tagged",
			fallback: `Tagged { s = "", n = 0 }`,
			refused:  true,
		},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			src := fmt.Sprintf(shape, row.decl, row.typeName, row.typeName, row.typeName, row.fallback)
			parseBag, semaBag, res := runSemaOnSnippetResult(t, src)
			if parseBag.HasErrors() {
				t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
			}
			if row.refused {
				requireSemaCodeCount(t, semaBag, diag.SemaMoveOutOfSharedBorrow, 1)
			} else {
				requireNoSemaErrors(t, parseBag, semaBag)
			}
			if res == nil || res.TypeInterner == nil {
				t.Fatalf("expected a sema result")
			}
			// The refusal follows the axis, and the axis is asked of the
			// program's own type — so an accepted row also proves the program
			// was typed far enough to have one.
			payload := typeNamed(t, res.TypeInterner, row.typeName)
			if got := res.OwnsHeap(payload); got != row.refused {
				t.Fatalf("%s: OwnsHeap=%v, but SEM3197 refused=%v; the rule and the axis parted",
					row.typeName, got, row.refused)
			}
		})
	}
}

// typeNamed finds the one type the interner labels with this name.
func typeNamed(t *testing.T, in *types.Interner, name string) types.TypeID {
	t.Helper()
	found := types.NoTypeID
	for id := types.TypeID(1); ; id++ {
		if _, ok := in.Lookup(id); !ok {
			break
		}
		if types.Label(in, id) != name {
			continue
		}
		if found != types.NoTypeID {
			t.Fatalf("the interner labels two types %q (%d and %d)", name, found, id)
		}
		found = id
	}
	if found == types.NoTypeID {
		t.Fatalf("the interner holds no type labelled %q", name)
	}
	return found
}
