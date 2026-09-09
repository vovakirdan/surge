package buildpipeline

import (
	"testing"

	"surge/internal/diag"
)

// The `Pair` every row below sends: Copy, so no ownership rule notices it, and
// two `int` fields wide, so the receiver of a botched payload reads a plausible
// number rather than crashing.
const borrowedChannelPayloadPair = `
@copy @shard_movable
type Pair = { a: int, b: int };
`

// A referent that is NOT Copy and does have a `__clone`. It is here because the
// three states of a named borrow take three different ways out, and this is the
// middle one: `send(own *p)` is refused for it (SEM3143, "cannot take `p.*` out
// of `p`") and so is `send(clone(p))` at a select arm (SEM3140, which wants a
// whole owned binding), so the copy has to be bound to a name first.
const borrowedChannelPayloadBoxed = `
@shard_movable
type Boxed = { name: string };

extern<Boxed> {
    pub fn __clone(self: &Boxed) -> Boxed {
        return Boxed { name = clone(self.name) };
    }
}
`

// One question, asked underneath `own`, at every channel send this rule reaches.
//
// An index read is a REFERENCE and not a value -- `ps[0]` where `ps: Pair[]` has
// type `&Pair` -- and the bare spelling has been refused here for as long as the
// rule has existed. `own` in front of it changes nothing about the shape: it is a
// move annotation on a value, it says who releases the thing, and it never says
// the thing was copied out of where it lived. The rule asked the payload's
// SURFACE type, `own &Pair` is a `KindOwn` and not a `KindReference`, and so one
// keyword walked past the whole rule.
//
// RED BEFORE THIS, measured on this tree at SURGE_SHARDS/THREADS 2 and at 8, twenty
// runs at each width and identical every time. `let ps: Pair[] = [Pair{ a: 11, b: 22
// }, Pair{ a: 1, b: 2 }];` and `select { ch.send(own ps[0]) => 1; stop.recv() => 2; }`
// over a far `Channel<Pair>`, read back by an anchored `on ch` reader: the program
// built with NO diagnostic, and a reader of `p.b` answered 11 where the sender's
// second field is 22 -- the first field one slot late -- exit 0 on every one of
// those forty runs. What arrives is an ADDRESS, so the symptom follows the field
// the reader touches: a reader of `p.a + p.b` DEREFERENCES it and SIGSEGVs inside
// rt_bigint_add, sixty runs out of sixty on another x86-64 Linux host at this
// commit with this program. Both are the same corruption -- an address reads back
// as a plausible small number where its bits decode as a fixnum and as a heap
// pointer where they do not -- and the silent half is the dangerous one, because
// no exit code tells it from a correct answer. The control that isolates it
// -- `let p0: Pair = ps[0];` before the select, then `ch.send(own p0)` -- answered
// 33 at both widths.
//
// AND IT WAS NEVER THE SELECT ARM'S ALONE. `ch.send(own ps[0])` on a plain local
// `Channel<Pair>`, with no select anywhere, built just as silently, and so did the
// local select arm; a `&Pair` parameter given away with `ch.send(own p)` built as
// well while `ch.send(p)` beside it was refused. That is why the repair went into
// the rule instead of into the select arm: one question missed at one place had
// three sinks open, and a bespoke refusal at the arm would have left the other two.
func TestChannelSendOfABorrowedPayloadIsRefusedUnderOwn(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	for _, tc := range []struct{ name, src, want, help string }{
		{
			name: "an element of a dynamic array at a far select's send arm",
			src: borrowedChannelPayloadPair + `
async fn go(ch: far Channel<Pair>, stop: far Channel<int>) -> int {
    let ps: Pair[] = [Pair{ a: 11, b: 22 }, Pair{ a: 1, b: 2 }];
    return select { ch.send(own ps[0]) => 1; stop.recv() => 2; };
}
`,
			want: "cannot send a borrow (own &Pair) through a channel",
			help: "`let v: Pair = xs[0];`, then `send(own v)`",
		},
		{
			name: "an element of a fixed array at a far select's send arm",
			src: borrowedChannelPayloadPair + `
async fn go(ch: far Channel<Pair>, stop: far Channel<int>) -> int {
    let fa: Pair[2] = [Pair{ a: 11, b: 22 }, Pair{ a: 1, b: 2 }];
    return select { ch.send(own fa[0]) => 1; stop.recv() => 2; };
}
`,
			want: "cannot send a borrow (own &Pair) through a channel",
			help: "`let v: Pair = xs[0];`, then `send(own v)`",
		},
		{
			name: "a map lookup at a far select's send arm",
			src: borrowedChannelPayloadPair + `
async fn go(ch: far Channel<Pair>, stop: far Channel<int>) -> int {
    let mut m: Map<int, Pair> = Map::<int, Pair>::new();
    let k: int = 1;
    m.insert(1, Pair{ a: 11, b: 22 });
    return select { ch.send(own m[&k]) => 1; stop.recv() => 2; };
}
`,
			want: "cannot send a borrow (own &Pair) through a channel",
			help: "`let v: Pair = xs[0];`, then `send(own v)`",
		},
		{
			// The payload NAMES the borrow, and the name is read from underneath
			// the `own` so this row and the bare row below it get ONE answer.
			//
			// The sentence is not the give-away one the shared table hands a
			// payload that borrows something else. It cannot be: this rule
			// refuses `ch.send(p)` and `ch.send(own p)` alike, so "send `p`
			// itself to give it away" would name no program that builds. `p` IS
			// the reference; what builds is reading through it.
			name: "a borrow parameter given away at a far select's send arm",
			src: borrowedChannelPayloadPair + `
async fn go(ch: far Channel<Pair>, stop: far Channel<int>, p: &Pair) -> int {
    return select { ch.send(own p) => 1; stop.recv() => 2; };
}
`,
			want: "cannot send a borrow (own &Pair) through a channel",
			help: "read through it and send what it points at -- `send(own *p)`",
		},
		{
			// `&mut T` is a `KindReference` carrying a Mutable flag rather than
			// a kind of its own, so one question answers both. Measured, not
			// assumed: without this row the mutable spelling could have stayed
			// open behind a repair that reads as if it closed everything.
			name: "a mutable borrow parameter given away at a far select's send arm",
			src: borrowedChannelPayloadPair + `
async fn go(ch: far Channel<Pair>, stop: far Channel<int>, pm: &mut Pair) -> int {
    return select { ch.send(own pm) => 1; stop.recv() => 2; };
}
`,
			want: "cannot send a borrow (own &mut Pair) through a channel",
			help: "read through it and send what it points at -- `send(own *pm)`",
		},
		{
			// The referent does not copy out from behind the reference, so the
			// deref the row above offers is refused for it -- and `clone(p)` in
			// the send is refused too, by the arm's whole-binding rule. The copy
			// is bound to a name first, and that is a different sentence rather
			// than the same one with a word changed.
			name: "a borrow parameter whose referent has a clone, at a far select's send arm",
			src: borrowedChannelPayloadBoxed + `
async fn go(ch: far Channel<Boxed>, stop: far Channel<int>, p: &Boxed) -> int {
    return select { ch.send(own p) => 1; stop.recv() => 2; };
}
`,
			want: "cannot send a borrow (own &Boxed) through a channel",
			help: "bind a copy first -- `let v = clone(p);` -- then `send(own v)`",
		},
		{
			// Nothing copies this referent out, so the sentence names no
			// spelling at all rather than one that would be refused next.
			name: "a borrow parameter whose referent has no copy out, at a far select's send arm",
			src: `
@shard_movable
type Opaque = { s: string };

async fn go(ch: far Channel<Opaque>, stop: far Channel<int>, p: &Opaque) -> int {
    return select { ch.send(own p) => 1; stop.recv() => 2; };
}
`,
			want: "cannot send a borrow (own &Opaque) through a channel",
			help: "send the value from the binding that owns it",
		},
		{
			// A borrow TAKEN of a local, which names no borrow and reads no
			// element. It keeps the table's nameless give-away sentence, and it
			// is here because the element sentence was measured reaching it:
			// this payload was told to write `let v: Pair = xs[0];` out of a
			// container the program does not have.
			name: "a borrow taken of a local at a far select's send arm",
			src: borrowedChannelPayloadPair + `
async fn go(ch: far Channel<Pair>, stop: far Channel<int>) -> int {
    let v: Pair = Pair{ a: 11, b: 22 };
    return select { ch.send(&v) => 1; stop.recv() => 2; };
}
`,
			want: "cannot send a borrow (&Pair) through a channel",
			help: "send the value itself to give it away",
		},
		{
			name: "an element of a dynamic array at a LOCAL select's send arm",
			src: borrowedChannelPayloadPair + `
async fn go() -> int {
    let ch: own Channel<Pair> = Channel::<Pair>::new(4:uint);
    let stop: own Channel<int> = Channel::<int>::new(1:uint);
    let ps: Pair[] = [Pair{ a: 11, b: 22 }, Pair{ a: 1, b: 2 }];
    return select { ch.send(own ps[0]) => 1; stop.recv() => 2; };
}
`,
			want: "cannot send a borrow (own &Pair) through a channel",
			help: "`let v: Pair = xs[0];`, then `send(own v)`",
		},
		{
			// No select at all. This sink has no second opinion of its own --
			// the borrow rule is the only payload rule it asks -- so it was
			// silently corrupting on its own, and it is the row that says a
			// select-arm-local refusal would not have been enough.
			name: "an element of a dynamic array at a plain local send",
			src: borrowedChannelPayloadPair + `
async fn go() -> int {
    let ch: own Channel<Pair> = Channel::<Pair>::new(4:uint);
    let ps: Pair[] = [Pair{ a: 11, b: 22 }, Pair{ a: 1, b: 2 }];
    ch.send(own ps[0]);
    return 0;
}
`,
			want: "cannot send a borrow (own &Pair) through a channel",
			help: "`let v: Pair = xs[0];`, then `send(own v)`",
		},
		{
			// A SCALAR element, and this row costs something, so the reason
			// sits next to it rather than in a commit message. `own xs[0]` over
			// a far `Channel<int>` DELIVERS THE RIGHT VALUE today, at 2 shards
			// and at 8 -- a heap bignum too, and still right when the frame
			// that owned the array has returned, so the ring copies the value
			// eagerly rather than keeping the address. It is refused anyway:
			// the BARE spelling of this very program is already refused by this
			// same rule and this same code, the plain crossing refuses `ret own
			// xs[0]` for the same reference, and the anchored `send` refuses it
			// too -- so admitting it here would make `own` a one-token bypass
			// of a rule the language applies to the identical shape everywhere
			// else, and would leave four sinks giving three answers. Keeping a
			// struct row and a scalar row side by side is deliberate: a reader
			// who runs only the `int` program sees the arm "working".
			name: "an int element at a far select's send arm, which delivered correctly",
			src: `
async fn go(ch: far Channel<int>, stop: far Channel<int>) -> int {
    let xs: int[] = [11, 22];
    return select { ch.send(own xs[0]) => 1; stop.recv() => 2; };
}
`,
			want: "cannot send a borrow (own &int) through a channel",
			help: "`let v: int = xs[0];`, then `send(own v)`",
		},
		{
			name: "a float element at a far select's send arm, which delivered correctly",
			src: `
async fn go(ch: far Channel<float>, stop: far Channel<int>) -> int {
    let xs: float[] = [1.5, 2.5];
    return select { ch.send(own xs[0]) => 1; stop.recv() => 2; };
}
`,
			want: "cannot send a borrow (own &float) through a channel",
			help: "`let v: float = xs[0];`, then `send(own v)`",
		},
		{
			// A non-Copy element takes a different way out, because binding it
			// to a name does not work: `let v: string = xs[0];` is itself
			// refused "cannot assign &string to string", for this same
			// reference. Measured, and the help says so rather than sending its
			// reader to that second refusal.
			name: "a string element at a far select's send arm",
			src: `
async fn go(ch: far Channel<string>, stop: far Channel<int>) -> int {
    let xs: string[] = ["ab", "cde"];
    return select { ch.send(own xs[0]) => 1; stop.recv() => 2; };
}
`,
			want: "cannot send a borrow (own &string) through a channel",
			help: "does not copy out of the container under a name",
		},
		{
			// The spelling that was refused all along, here for its HELP: it
			// used to get the table's nameless sentence, "send the value itself
			// to give it away, or send a copy: clone(...)", which tells a
			// reader holding `ps[0]` nothing, because a value is exactly what an
			// index read does not produce. Bare and `own` now get one answer.
			name: "an element of a dynamic array, bare, at a far select's send arm",
			src: borrowedChannelPayloadPair + `
async fn go(ch: far Channel<Pair>, stop: far Channel<int>) -> int {
    let ps: Pair[] = [Pair{ a: 11, b: 22 }, Pair{ a: 1, b: 2 }];
    return select { ch.send(ps[0]) => 1; stop.recv() => 2; };
}
`,
			want: "cannot send a borrow (&Pair) through a channel",
			help: "`let v: Pair = xs[0];`, then `send(v)`",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requireSendPayloadDiagnosticWithHelp(t, tc.src, diag.SemaChannelNosendValue, tc.want, tc.help)
		})
	}
}

// The whole-binding rule the select arm has of its own is NOT replaced by this
// one, and this row is what says so. `own xs[0]` on a `string[]` was refused
// SEM3141 before the borrow question was asked underneath `own`, and it still
// is: the two rules answer different questions about the same program -- this is
// a borrow, and this is not a whole binding the losing arms could reclaim -- and
// the second must not quietly disappear because the first started firing.
func TestChannelSendBorrowRefusalLeavesTheWholeBindingRuleStanding(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	requireSendPayloadDiagnostic(t, `
async fn go(ch: far Channel<string>, stop: far Channel<int>) -> int {
    let xs: string[] = ["ab", "cde"];
    return select { ch.send(own xs[0]) => 1; stop.recv() => 2; };
}
`, diag.SemaSelectSendPayloadNotBinding, "must be a whole owned binding")
}

// The help each refusal offers, compiled. A refusal whose advice does not build
// is worse than no advice, and it is what a reader meets first.
//
// Each row here is the sentence from the row above it, written out. The first
// also RUNS: bound out before the select, the same program whose reader answered
// 11 answers 33 at SURGE_SHARDS/THREADS 2 and at 8 (internal/vm carries that
// one end to end).
func TestChannelSendBorrowRefusalHelpCompiles(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	for _, tc := range []struct{ name, src string }{
		{
			name: "the struct element's help: bind it out, then give the name away",
			src: borrowedChannelPayloadPair + `
async fn go(ch: far Channel<Pair>, stop: far Channel<int>) -> int {
    let ps: Pair[] = [Pair{ a: 11, b: 22 }, Pair{ a: 1, b: 2 }];
    let v: Pair = ps[0];
    return select { ch.send(own v) => 1; stop.recv() => 2; };
}
`,
		},
		{
			name: "the same help at a plain local send",
			src: borrowedChannelPayloadPair + `
async fn go() -> int {
    let ch: own Channel<Pair> = Channel::<Pair>::new(4:uint);
    let ps: Pair[] = [Pair{ a: 11, b: 22 }, Pair{ a: 1, b: 2 }];
    let v: Pair = ps[0];
    ch.send(own v);
    return 0;
}
`,
		},
		{
			name: "the int element's help: bind it out and send the name",
			src: `
async fn go(ch: far Channel<int>, stop: far Channel<int>) -> int {
    let xs: int[] = [11, 22];
    let v: int = xs[0];
    return select { ch.send(v) => 1; stop.recv() => 2; };
}
`,
		},
		{
			name: "the float element's help: bind it out and send the name",
			src: `
async fn go(ch: far Channel<float>, stop: far Channel<int>) -> int {
    let xs: float[] = [1.5, 2.5];
    let v: float = xs[0];
    return select { ch.send(v) => 1; stop.recv() => 2; };
}
`,
		},
		{
			// The borrow parameter's help, in the spelling it prints. The
			// give-away clause it used to print is what this row replaced:
			// neither `ch.send(p)` nor `ch.send(own p)` builds at this commit,
			// so the sentence that offered them named nothing a reader could
			// write, and a pinned string is not a compiled one.
			name: "the borrow parameter's help: read through the borrow",
			src: borrowedChannelPayloadPair + `
async fn go(ch: far Channel<Pair>, stop: far Channel<int>, p: &Pair) -> int {
    return select { ch.send(own *p) => 1; stop.recv() => 2; };
}
`,
		},
		{
			name: "the borrow parameter's help at a plain local send",
			src: borrowedChannelPayloadPair + `
async fn go(p: &Pair) -> int {
    let ch: own Channel<Pair> = Channel::<Pair>::new(4:uint);
    ch.send(own *p);
    return 0;
}
`,
		},
		{
			name: "the mutable borrow parameter's help: read through the borrow",
			src: borrowedChannelPayloadPair + `
async fn go(ch: far Channel<Pair>, stop: far Channel<int>, pm: &mut Pair) -> int {
    return select { ch.send(own *pm) => 1; stop.recv() => 2; };
}
`,
		},
		{
			// The clonable referent's help, both clauses, in one program: the
			// copy is bound to a name and the name is given away. Sending
			// `clone(p)` straight into the arm is refused (SEM3140), which is
			// why the sentence does not offer it.
			name: "the clonable borrow parameter's help: bind a copy, then send the name",
			src: borrowedChannelPayloadBoxed + `
async fn go(ch: far Channel<Boxed>, stop: far Channel<int>, p: &Boxed) -> int {
    let v = clone(p);
    return select { ch.send(own v) => 1; stop.recv() => 2; };
}
`,
		},
		{
			// The nameless sentence's give-away clause, for the payload that
			// still gets it: `&v` borrows a value the author owns and names.
			name: "the borrowed local's help: send the value itself",
			src: borrowedChannelPayloadPair + `
async fn go(ch: far Channel<Pair>, stop: far Channel<int>) -> int {
    let v: Pair = Pair{ a: 11, b: 22 };
    return select { ch.send(own v) => 1; stop.recv() => 2; };
}
`,
		},
		{
			name: "the string element's help: send the container itself",
			src: `
async fn go(ch: far Channel<string[]>, stop: far Channel<int>) -> int {
    let xs: string[] = ["ab", "cde"];
    return select { ch.send(own xs) => 1; stop.recv() => 2; };
}
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compileCleanly(t, tc.src)
		})
	}
}

// The admission the refusals above are worth having, and the reason the repair
// went into the rule's QUESTION rather than into a new gate on `own`: `own` in
// front of a VALUE is ordinary and stays ordinary.
//
// A struct field read, a tuple position, a call result, a deref and a string
// index all yield a value, not a place, so `ownStripped` finds a value type and
// the question does not fire. Every row here compiles, and every row also runs
// and delivers what it was given at SURGE_SHARDS/THREADS 2 and at 8. The deref
// row is the nearest miss and the one that matters most: `own *p` is the way out
// a reader holding a borrow already has, and a repair that closed it would have
// shut the door it opens.
func TestChannelSendOfAValuePayloadIsNotRefused(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	for _, tc := range []struct{ name, body string }{
		{"an own local of a Copy type", `let v: Pair = mk(); return select { ch.send(own v) => 1; stop.recv() => 2; };`},
		{"a bare local of a Copy type", `let v: Pair = mk(); return select { ch.send(v) => 1; stop.recv() => 2; };`},
		{"an own Copy struct field", `let w: Wrap = Wrap{ p: mk(), n: 3 }; return select { ch.send(own w.p) => 1; stop.recv() => 2; };`},
		{"a bare Copy struct field", `let w: Wrap = Wrap{ p: mk(), n: 3 }; return select { ch.send(w.p) => 1; stop.recv() => 2; };`},
		{"an own tuple position", `let t: (Pair, int) = (mk(), 3); return select { ch.send(own t.0) => 1; stop.recv() => 2; };`},
		{"a bare tuple position", `let t: (Pair, int) = (mk(), 3); return select { ch.send(t.0) => 1; stop.recv() => 2; };`},
		{"an own call result", `return select { ch.send(own mk()) => 1; stop.recv() => 2; };`},
		{"a bare call result", `return select { ch.send(mk()) => 1; stop.recv() => 2; };`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compileCleanly(t, borrowedChannelPayloadPair+`
@copy @shard_movable
type Wrap = { p: Pair, n: int };

fn mk() -> Pair { return Pair{ a: 11, b: 22 }; }

async fn go(ch: far Channel<Pair>, stop: far Channel<int>) -> int {
    `+tc.body+`
}
`)
		})
	}
	t.Run("an own deref of a borrow parameter", func(t *testing.T) {
		compileCleanly(t, borrowedChannelPayloadPair+`
async fn go(ch: far Channel<Pair>, stop: far Channel<int>, p: &Pair) -> int {
    return select { ch.send(own *p) => 1; stop.recv() => 2; };
}
`)
	})
	t.Run("an own string index, which is a uint32 and not a place", func(t *testing.T) {
		compileCleanly(t, `
async fn go(ch: far Channel<uint32>, stop: far Channel<int>) -> int {
    let s: string = "abc";
    return select { ch.send(own s[0]) => 1; stop.recv() => 2; };
}
`)
	})
	t.Run("an own local of a non-Copy type", func(t *testing.T) {
		compileCleanly(t, `
async fn go(ch: far Channel<string>, stop: far Channel<int>) -> int {
    let mut s: string = "jo";
    s = s + "b";
    return select { ch.send(own s) => 1; stop.recv() => 2; };
}
`)
	})
}
