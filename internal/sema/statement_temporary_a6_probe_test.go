package sema

import (
	"context"
	"testing"

	"surge/internal/diag"
	"surge/internal/symbols"
)

// Probe A6 decides the question R1.2.9 item 7 could not settle by reading: whether a
// by-value receiver's move consumes the temporary's candidacy before any sink asks.
//
// What it really tests is the shared-key invariant of R1.3.1 -- gate-false implies
// moved, because formalMayBeBorrowSource and applyParamOwnership read the SAME
// Params[0] key (magic_ownership.go:21-37, :48-57). A candidacy that survives its
// statement is exactly what popTempFrame publishes (temp_drops.go:38-60), so
// Result.TempDrops answers without any frame instrumentation.
//
// GREEN (by-value absent, control present): CF-13 is vacuous by construction.
// RED (by-value present): the invariant broke, the withdrawn A-2 row stands after
// all, and CF-13 is a real counterfactual -- record it under RR-4, relax nothing.
//
// The receiver method is `mark`, not `tag`: `tag` is the tag-declaration keyword and
// the first version of this probe never parsed.

const a6ByValueSource = `fn make() -> string {
    return "x";
}
fn shout(s: &string) -> nothing {
    return nothing;
}
extern<string> {
    fn mark(self: string, k: &string) -> &string {
        return k;
    }
}
fn keep_byvalue_recv(p: &string) -> nothing {
    let r: &string = make().mark(p);
    shout(r);
    return nothing;
}
`

const a6ByValueSHA = "d3c27e6e41319b1a8f6e83c0ff819d86f1090146ac3a24a7f389530d5b9b263d"

const a6ByValueLen = 309

const a6ByValueMake = 257

const a6ControlSource = `fn id_ref(s: &string) -> &string {
    return s;
}
fn make() -> string {
    return "x";
}
fn shout(s: &string) -> nothing {
    return nothing;
}
fn control() -> nothing {
    let r: &string = id_ref(make());
    shout(r);
    return nothing;
}
`

const a6ControlSHA = "62f4b892861192d71075a682e544ce8baeff2c305c61eaf29ed59db49e7c93f0"

const a6ControlLen = 246

const a6ControlMake = 201

func TestStatementTemporaryA6ReceiverMoveConsumesCandidacy(t *testing.T) {
	checkOriginSource(t, "A6 by-value receiver", a6ByValueSource, a6ByValueSHA, a6ByValueLen)
	checkOriginSource(t, "A6 shared-formal control", a6ControlSource, a6ControlSHA, a6ControlLen)

	// Positive control: a shared `&string` formal records no move, so make() is
	// still flagged when the statement's frame pops. Without this half a green
	// measurement would be indistinguishable from a probe that measures nothing.
	if !tempDropAtOffset(t, a6ControlSource, a6ControlMake) {
		t.Fatalf("A6 control is dead: no TempDrops entry for make() at %d", a6ControlMake)
	}

	// Measurement: a by-value `self` moves the receiver through
	// applyParamOwnership's default arm, which consumes the candidacy.
	if tempDropAtOffset(t, a6ByValueSource, a6ByValueMake) {
		t.Fatalf("A6 RED: make() at %d survived as a statement temporary, so the receiver gate is load-bearing (RR-4)", a6ByValueMake)
	}
}

func tempDropAtOffset(t *testing.T, src string, start uint32) bool {
	t.Helper()
	builder, fileID, parseBag := parseSnippet(t, src)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	semaBag := diag.NewBag(32)
	symRes := symbols.ResolveFile(builder, fileID, &symbols.ResolveOptions{
		Reporter: &diag.BagReporter{Bag: semaBag},
	})
	res := Check(context.Background(), builder, fileID, Options{
		Reporter: &diag.BagReporter{Bag: semaBag},
		Symbols:  &symRes,
	})
	for id := range res.TempDrops {
		if node := builder.Exprs.Get(id); node != nil && node.Span.Start == start {
			return true
		}
	}
	return false
}
