package sema

import (
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
)

// The two refusals a VIEW earns at an `on` capture, and the question the second
// one asks. They live together because they say one thing in two places: the
// elements of a view travel exactly as its base's do, and the buffer they live
// in does not travel at all.
//
// Both are KINDNESS in front of a guarantee that lives in the runtime. Every
// crossing of a dynamic array hands its header to `rt_array_unshare_walk`,
// whatever the element type, and the walk asks the view registry and refuses a
// view -- or a base some view still reads -- by name. Nothing here is load
// bearing for safety: a view laundered through a call's return value, a struct
// field or a parameter, or written into a holder by `xs.push(v)`, `xs[0] = v` or
// `pair.0 = v`, carries no marker either of these can read, and every one of
// those programs dies with a VM1003 panic instead of aliasing.
//
// What these two buy is the moment and the words. A compile error names the
// binding the reader wrote and a way out they can take, at the point they can
// still change it, instead of a panic on a machine with a shard count. So they
// must never be read as the set of views that cannot cross -- they are the
// subset this checker can see, and they are also not a subset of what the
// runtime refuses, because they read a MAY fact over every path and the runtime
// reads the header in hand.

// holdsAnArrayView is ON-CAP-N008's question: does this binding hold a view
// somewhere inside the value it names?
func (tc *typeChecker) holdsAnArrayView(symID symbols.SymbolID) bool {
	_, held := tc.arrayViewHolderOf(symID)
	return held
}

// reportCrossingViewCapture words ON-CAP-N007.
//
// The way out is NAMED rather than quoted, and that is a correction. The quoted
// loop this replaced -- `let mut out: T[] = []; for i ... { out.push(v[i]); }`
// -- was checked on `int[]` and generalised without being checked again: an
// element read out of a view is a BORROW, so `out.push(v[i])` is refused with
// SEM3137 for `string[]` and for `int[][]`, which between them are most of what
// ON-CAP-V005 admits. `clone(v[i])` compiles for a `string` element and does not
// exist for an inner array. So what the message states is what holds for every
// element: the shape of the array the reader has to build.
func (tc *typeChecker) reportCrossingViewCapture(symID symbols.SymbolID, span source.Span) {
	tc.report(diag.SemaCrossNotShardMovable, span,
		"`%s` is a view of another array: its elements live in that array's buffer, which this "+
			"shard keeps, reads through and frees, so a view cannot cross a shard boundary. "+
			"Cross an array of your own instead -- build one holding a copy of every element "+
			"in the window, and cross that",
		tc.captureName(symID))
}

// reportCrossingHeldViewCapture words ON-CAP-N008, and names the element when
// the view had a name of its own.
func (tc *typeChecker) reportCrossingHeldViewCapture(symID symbols.SymbolID, span source.Span) {
	held, _ := tc.arrayViewHolderOf(symID)
	where := "one of its elements is a view of another array"
	if held.elem.IsValid() {
		where = "its element `" + tc.captureName(held.elem) + "` is a view of another array"
	}
	tc.report(diag.SemaCrossNotShardMovable, span,
		"`%s` cannot cross a shard boundary: %s, and the elements that view windows live in a "+
			"buffer this shard keeps, reads through and frees. Hold elements that own their "+
			"own storage -- replace the view with an array holding a copy of every element in "+
			"the window -- and cross that",
		tc.captureName(symID), where)
}

// captureName answers with the name a reader wrote, or a stand-in when the
// binding has none to quote.
func (tc *typeChecker) captureName(symID symbols.SymbolID) string {
	if sym := tc.symbolFromID(symID); sym != nil {
		return tc.lookupName(sym.Name)
	}
	return "this value"
}
