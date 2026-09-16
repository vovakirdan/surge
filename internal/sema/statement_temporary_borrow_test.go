package sema

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"surge/internal/diag"
)

// P1z refuses a reference to a value the statement itself frees, at the three sinks
// that can outlive the evaluation region: `let`, `=` and `return`. The sources below
// are the packet's frozen leaves, each pinned by its sha256 so a row can never
// measure a source other than the one the packet reasoned about.
//
// The package harness cannot assert these bags. requireSemaCodeCount fatals on the
// first diagnostic of any OTHER code (drop_obligations_test.go:34-47) and S1 holds
// both SEM3023 and SEM3139; requireNoSemaErrors fatals on any error at all, and S1
// holds six by construction. So rows are asserted per (code, span) here, bag SIZE
// first: without the size assertion an extra unexpected diagnostic passes unseen and
// stop condition S-5 silently stops working.
//
// Two fixture corrections after the first measurement at 59663309:
//   - `tag` is the tag-declaration keyword, so S4 never parsed. The field is `mark`.
//   - `make() to string` is not spellable: `__to` is not defined string->string
//     (SEM3015 x2), and materialization into a `&` formal is string-only
//     (addressability.go:165), so no cast can reach the walk's anchor in a carrier
//     position. The refuse_cast_arg leaf is withdrawn and CF-11 is vacuous by
//     construction, the same standing as CF-13.

const s1Source = `fn id_ref(s: &string) -> &string {
    return s;
}
fn make() -> string {
    return "x";
}
fn shout(s: &string) -> nothing {
    return nothing;
}
fn dup(s: &string) -> string {
    return "y";
}
fn keep_arg() -> nothing {
    shout(id_ref(make()));
    return nothing;
}
fn keep_place(p: &string) -> nothing {
    let r: &string = id_ref(p);
    shout(r);
    return nothing;
}
fn keep_owned() -> nothing {
    let v: string = dup(make());
    return nothing;
}
fn refuse_let_call() -> nothing {
    let r: &string = id_ref(make());
    shout(r);
    return nothing;
}
fn refuse_let_literal() -> nothing {
    let r: &string = id_ref("lit");
    shout(r);
    return nothing;
}
fn refuse_assign(p: &string) -> nothing {
    let mut r: &string = id_ref(p);
    r = id_ref(make());
    shout(r);
    return nothing;
}
fn refuse_return() -> &string {
    return id_ref(make());
}
fn refuse_ternary(flag: bool, p: &string) -> nothing {
    let r: &string = flag ? id_ref(make()) : id_ref(p);
    shout(r);
    return nothing;
}
fn refuse_nested() -> nothing {
    let r: &string = id_ref(id_ref(make()));
    shout(r);
    return nothing;
}
`

const s1SHA = "9f543da8077a5987fcfeca2f3ad51de0810e34cffab25aba04ddce29ed00df41"

const s1Len = 1138

const s2Source = `extern<string> {
    fn me(self: &string) -> &string {
        return self;
    }
}
fn make() -> string {
    return "x";
}
fn shout(s: &string) -> nothing {
    return nothing;
}
fn keep_recv_place(p: &string) -> nothing {
    let r: &string = p.me();
    shout(r);
    return nothing;
}
fn refuse_recv_temp() -> nothing {
    let r: &string = make().me();
    shout(r);
    return nothing;
}
`

const s2SHA = "5d88fa652b612ab5be57ff9291a1ac89bcf1f73bf96a5cc0720f640c3f45e812"

const s2Len = 394

const s3Source = `fn id_ref(s: &string) -> &string {
    return s;
}
fn make() -> string {
    return "x";
}
fn shout(s: &string) -> nothing {
    return nothing;
}
fn take_mut(s: &mut string) -> nothing {
    return nothing;
}
fn mut_from_shared(m: &mut string, k: &string) -> &mut string {
    return m;
}
fn keep_materialized() -> nothing {
    shout(make());
    return nothing;
}
fn refuse_block_ret() -> nothing {
    let r: &string = { ret id_ref(make()); };
    shout(r);
    return nothing;
}
fn refuse_mut_carrier() -> nothing {
    let mut base: string = "b";
    let r: &mut string = mut_from_shared(base, make());
    take_mut(r);
    return nothing;
}
`

const s3SHA = "8851f231344ac3c2e4374e04a559c64e6acc048bff8adcd481b34721b7b4d5a8"

const s3Len = 648

const s4Source = `fn make() -> string {
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

const s4SHA = "d3c27e6e41319b1a8f6e83c0ff819d86f1090146ac3a24a7f389530d5b9b263d"

const s4Len = 309

// checkOriginSource pins a measured source to the bytes the packet froze. A row that
// measures a source nobody can identify measures nothing.
func checkOriginSource(t *testing.T, name, src, wantSHA string, wantLen int) {
	t.Helper()
	sum := sha256.Sum256([]byte(src))
	got := hex.EncodeToString(sum[:])
	if len(src) != wantLen || got != wantSHA {
		t.Fatalf("%s is not the frozen source: %d bytes sha256 %s, want %d bytes sha256 %s",
			name, len(src), got, wantLen, wantSHA)
	}
}

// temporaryRow is one expected diagnostic: its code, the span of the message, and
// the span of the note, which must name the evaluation MIR frees.
type temporaryRow struct {
	code      diag.Code
	start     uint32
	end       uint32
	noteStart uint32
	noteEnd   uint32
}

func requireTemporaryRows(t *testing.T, bag *diag.Bag, want []temporaryRow) {
	t.Helper()
	items := bag.Items()
	if len(items) != len(want) {
		t.Fatalf("diagnostic count = %d, want %d: %s", len(items), len(want), diagnosticsSummary(bag))
	}
	used := make([]bool, len(want))
	for _, d := range items {
		matched := -1
		for i, w := range want {
			if used[i] || d.Code != w.code || d.Primary.Start != w.start || d.Primary.End != w.end {
				continue
			}
			matched = i
			break
		}
		if matched < 0 {
			t.Fatalf("unexpected %s at [%d,%d): %s", d.Code.ID(), d.Primary.Start, d.Primary.End, diagnosticsSummary(bag))
		}
		used[matched] = true
		w := want[matched]
		if len(d.Notes) == 0 {
			t.Fatalf("%s at [%d,%d) carries no note; the temporary must be named", d.Code.ID(), w.start, w.end)
		}
		if d.Notes[0].Span.Start != w.noteStart || d.Notes[0].Span.End != w.noteEnd {
			t.Fatalf("%s at [%d,%d): note names [%d,%d), want [%d,%d)", d.Code.ID(), w.start, w.end,
				d.Notes[0].Span.Start, d.Notes[0].Span.End, w.noteStart, w.noteEnd)
		}
	}
}

func semaRowsFor(t *testing.T, name, src, sha string, length int) *diag.Bag {
	t.Helper()
	checkOriginSource(t, name, src, sha, length)
	parseBag, semaBag := runSemaOnSnippet(t, src)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	return semaBag
}

// Rows in leaf order: refuse_let_call, refuse_let_literal, refuse_assign,
// refuse_return, refuse_ternary, refuse_nested.
func TestStatementTemporaryS1Rows(t *testing.T) {
	bag := semaRowsFor(t, "S1", s1Source, s1SHA, s1Len)
	requireTemporaryRows(t, bag, []temporaryRow{
		{diag.SemaBorrowNonAddressable, 518, 532, 525, 531},
		{diag.SemaBorrowNonAddressable, 628, 641, 635, 640},
		{diag.SemaBorrowNonAddressable, 765, 779, 772, 778},
		{diag.SemaBorrowEscapesReturn, 860, 874, 867, 873},
		{diag.SemaBorrowNonAddressable, 954, 987, 968, 974},
		{diag.SemaBorrowNonAddressable, 1078, 1100, 1092, 1098},
	})
}

// The method-receiver form: refuse_recv_temp.
func TestStatementTemporaryS2ReceiverRow(t *testing.T) {
	bag := semaRowsFor(t, "S2", s2Source, s2SHA, s2Len)
	requireTemporaryRows(t, bag, []temporaryRow{
		{diag.SemaBorrowNonAddressable, 345, 356, 345, 351},
	})
}

// Rows in leaf order: refuse_block_ret (the TempDrops disjunct), refuse_mut_carrier
// (P1z's own formal gate). keep_materialized is the admitted control.
func TestStatementTemporaryS3Rows(t *testing.T) {
	bag := semaRowsFor(t, "S3", s3Source, s3SHA, s3Len)
	requireTemporaryRows(t, bag, []temporaryRow{
		{diag.SemaBorrowNonAddressable, 423, 446, 436, 442},
		{diag.SemaBorrowNonAddressable, 578, 607, 600, 606},
	})
}

// S4 is the control A-2 asked for: a by-value receiver whose result borrows a
// DIFFERENT formal is a valid program, and it must stay admitted under either
// version of the receiver gate.
func TestStatementTemporaryS4ByValueReceiverClean(t *testing.T) {
	bag := semaRowsFor(t, "S4", s4Source, s4SHA, s4Len)
	requireTemporaryRows(t, bag, nil)
}
