package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// checkSpawnSendability verifies that a symbol's type can be safely sent to a spawn.
// Types with the @nosend attribute cannot cross spawn boundaries as they may contain
// thread-local state, non-atomic reference counts, or other non-thread-safe data.
//
// This check is performed when a variable is captured by a spawn expression
// to ensure structured concurrency safety.
func (tc *typeChecker) checkSpawnSendability(symID symbols.SymbolID, span source.Span) {
	if !symID.IsValid() {
		return
	}

	if tc.isLocalTaskBinding(symID) {
		label := tc.symbolLabel(symID)
		tc.report(diag.SemaNosendInSpawn, span,
			"cannot send local task handle %s to spawn; use @local spawn", label)
		return
	}

	valueType := tc.bindingType(symID)
	if valueType == types.NoTypeID {
		return
	}

	// Strip ownership/reference modifiers to get base type
	baseType := tc.valueType(valueType)

	// Check if the base type has @nosend
	if tc.typeHasAttr(baseType, "nosend") {
		label := tc.symbolLabel(symID)
		typeName := tc.typeLabel(baseType)
		tc.report(diag.SemaNosendInSpawn, span,
			"cannot send %s of @nosend type '%s' to spawn; use @local spawn", label, typeName)
	}

	// Recursively check struct fields for nested @nosend types
	tc.checkNestedNosendWith(baseType, span, diag.SemaNosendInSpawn)
}

// checkChannelSendValue checks if a value being sent through a channel has @nosend attribute.
// Channel sends transfer ownership to another task, so @nosend types are prohibited.
//
// This check is performed when evaluating channel send operations (ch.send(value)).
func (tc *typeChecker) checkChannelSendValue(valueExpr ast.ExprID, span source.Span) bool {
	if !valueExpr.IsValid() {
		return false
	}

	if tc.isLocalTaskExpr(valueExpr) {
		tc.report(diag.SemaChannelNosendValue, span,
			"cannot send local task handle through channel")
		return true
	}

	valueType := tc.result.ExprTypes[valueExpr]
	if valueType == types.NoTypeID {
		return false
	}

	// A borrow is never sendable: the receiving task can outlive the
	// borrowed value's scope, and the loan table cannot follow the
	// reference across the channel.
	//
	// THE QUESTION IS ASKED UNDERNEATH `own`. `own` is a move annotation on a
	// value -- it says who releases the thing and when the writer's binding
	// dies -- and it never says the thing was copied out of where it lived, so
	// `own &T` still names a place. Asking the payload's SURFACE type left
	// `own` a one-token bypass of this whole rule, and the bypass was silent:
	// `select { ch.send(own ps[0]) => 1; stop.recv() => 2; }` over a far
	// `Channel<Pair>`, `Pair` a `@copy @shard_movable` pair of `int`s, built
	// with no diagnostic and its anchored `p.a + p.b` reader answered 11 where
	// the two fields sum to 33, at SURGE_SHARDS/THREADS 2 and at 8.
	//
	// WHAT ARRIVES IS AN ADDRESS, so the symptom depends on which field the
	// reader touches and on what those bits decode as. Read field by field
	// instead of inferred: where the element sent is `{ a: 11, b: 22 }`, a
	// reader of `p.b` answers 11 -- the sender's FIRST field one slot late, and
	// nothing of the second -- exit 0 and no diagnostic, forty runs, twenty at
	// each width, every one answering 11; sharpening the sender to
	// `{ a: 0, b: 22 }` makes that reader answer 0 the same way. A reader that
	// touches the field the address landed on DEREFERENCES it: `p.a + p.b`
	// SIGSEGVs inside rt_bigint_add, sixty runs out of sixty on another
	// x86-64 Linux host at this commit with this program, the backtrace running
	// trim_len <- bu_cmp_limbs <- bi_add with a wild first operand and the
	// fixnum 11 as the second.
	//
	// Both measurements are one fact and neither is false: a word that is an
	// address reads back as a plausible small number where it decodes as a
	// fixnum and as a heap pointer where it does not. The silent half is the
	// dangerous one, because no exit code tells it from a correct answer; the
	// crash is the loud half of the same corruption.
	//
	// It was never the select arm's bypass alone: the bare `ps[0]` was refused
	// here all along, `ch.send(own ps[0])` on a plain local `Channel<Pair>`
	// was accepted the same way and delivered the same wrong 11, and the local
	// select arm with it. One question missed at one place, three sinks open.
	//
	// The anchored `send` asks this same question of its own payloads
	// (checkAnchoredSendPayloadIsAValue) and is NOT made redundant by this: it
	// is reached from typeAnchoredChannelOp, which never calls this rule, and
	// its message names the channel's element type while its help branches on
	// the element family for a give-away rule that is anchored-only. Two
	// functions asking one question -- do not unify them by deleting the
	// anchored one, whose message and help answer for a rule this one does not
	// have. It differs in a way this rule does NOT share, measured rather than
	// assumed: it names an element read for every payload it refuses, so
	// `ch.send(own p)` for a `p: &Pair` parameter is told there to bind
	// `let v: Pair = xs[0];` out of a container its program need not have, where
	// this rule answers the same payload by naming the borrow. That is a
	// wording difference and not a rule difference -- both refuse it -- and the
	// debt ledger carries it as a rough edge of that sink.
	if info, ok := tc.types.Lookup(tc.ownStripped(valueType)); ok && info.Kind == types.KindReference {
		// The way out leaves the headline and becomes help, because it names a
		// spelling: whether a copy can be sent at all depends on the payload,
		// and a headline cannot be conditional on it.
		message := fmt.Sprintf(
			"cannot send a borrow (%s) through a channel: the receiving task can outlive the borrowed value",
			tc.typeLabel(valueType))
		b := diag.ReportError(tc.reporter, diag.SemaChannelNosendValue, span, message)
		if b == nil {
			tc.report(diag.SemaChannelNosendValue, span, "%s", message)
			return true
		}
		if help := tc.channelBorrowPayloadHelp(info.Elem, valueExpr); help != "" {
			b.WithHelp(span, help)
		}
		b.Emit()
		return true
	}

	// Strip ownership/reference modifiers to get base type
	baseType := tc.valueType(valueType)

	// Check if the base type has @nosend
	if tc.typeHasAttr(baseType, "nosend") {
		typeName := tc.typeLabel(baseType)
		tc.report(diag.SemaChannelNosendValue, span,
			"cannot send @nosend type '%s' through channel", typeName)
		return true
	}

	// Recursively check struct fields for nested @nosend types
	tc.checkNestedNosendWith(baseType, span, diag.SemaChannelNosendValue)
	return false
}

// channelBorrowPayloadHelp names the way out of the refusal above, and the
// three shapes that reach it need three different sentences.
//
// A payload that NAMES the borrow -- `p` or `own p` for a `p: &Pair`, the name
// read from UNDERNEATH the `own` so one borrow gets one answer -- has nothing
// here to give away, because the name IS the reference. It does not keep the
// give-away sentence: measured at a far select's send arm, `ch.send(p)` and
// `ch.send(own p)` are both refused by this rule, so "send `p` itself to give
// it away" named no program that builds. It gets the sentence that spells what
// does (adviceChannelBorrowBinding).
//
// A payload that reads an ELEMENT out of a container -- `ps[0]`, `m[&k]` -- has
// no name to offer, and the table's nameless sentence ("send the value itself
// ... clone(...)") sends its reader nowhere: a value is exactly what an index
// read does not produce. Those get the way out the anchored sink already
// teaches for this same shape, with its block dropped.
//
// Anything else that made a reference without reading an element keeps the
// table's nameless sentence, and `&v` is the shape measured needing that: the
// element sentence would send its author to bind `let v: Pair = xs[0];` out of
// a container they do not have, while "send the value itself to give it away"
// is `ch.send(own v)`, which builds.
//
// TWO BRANCHES FOR THE ELEMENT, not the anchored sink's three. Its middle
// branch exists because a counted Copy element must then be GIVEN AWAY or
// SEM3212 refuses the next build, and that rule is anchored-only: `let v: float
// = xs[0];` followed by `send(v)` and by `send(own v)` both compile at a far
// select arm and at a plain local send, as do both spellings for a `@copy`
// struct. So the sentence keeps whichever spelling the author already wrote
// instead of teaching an ownership token this sink does not ask for.
func (tc *typeChecker) channelBorrowPayloadHelp(referent types.TypeID, valueExpr ast.ExprID) string {
	inner, wroteOwn := tc.payloadUnderOwn(valueExpr)
	if name := tc.identNameOf(inner); name != "" {
		return tc.cloneAdviceFor(adviceChannelBorrowBinding, referent, name).Help
	}
	if !tc.isElementRead(inner) {
		return tc.cloneAdviceFor(adviceChannelBorrow, referent, "").Help
	}
	label := tc.typeLabel(referent)
	switch tc.adviceCloneState(referent) {
	case CloneDeferred:
		// An undecided generic gets silence, the same contract the clone table
		// keeps one layer down: whether this element copies out under a name is
		// a fact about a type nobody has chosen yet.
		return ""
	case CloneCopy:
		spelling := "send(v)"
		if wroteOwn {
			spelling = "send(own v)"
		}
		return fmt.Sprintf("bind the element to a name first and send the name: "+
			"`let v: %s = xs[0];`, then `%s`", label, spelling)
	default:
		// Measured rather than reasoned: `let v: string = xs[0];` is itself
		// refused "cannot assign &string to string", for this same reference.
		return fmt.Sprintf("a `%s` does not copy out of the container under a name either -- "+
			"`let v: %s = xs[0];` is refused for this same reference -- so send a whole owned "+
			"binding instead: the container itself, over a channel whose element is the "+
			"container's own type", label, label)
	}
}

// isElementRead reports whether a payload took an element out of a container --
// `xs[0]`, `m[&k]` -- which is the only shape the element sentence can name.
// `&v` reaches the same refusal and is not one of them.
func (tc *typeChecker) isElementRead(expr ast.ExprID) bool {
	if tc == nil || tc.builder == nil || !expr.IsValid() {
		return false
	}
	index, ok := tc.builder.Exprs.Index(expr)
	return ok && index != nil
}

// payloadUnderOwn returns what an `own` in front of a payload wraps, and whether
// there was one. A sentence about the payload's SHAPE has to look through the
// `own` for both halves of its job: to find the binding the author named, and to
// answer in the spelling they wrote.
func (tc *typeChecker) payloadUnderOwn(expr ast.ExprID) (ast.ExprID, bool) {
	if tc == nil || tc.builder == nil || !expr.IsValid() {
		return expr, false
	}
	inner := tc.unwrapGroupExpr(expr)
	unary, ok := tc.builder.Exprs.Unary(inner)
	if !ok || unary == nil || unary.Op != ast.ExprUnaryOwn {
		return inner, false
	}
	return tc.unwrapGroupExpr(unary.Operand), true
}

// checkNestedNosendWith recursively checks struct fields for @nosend attribute.
// This is a unified implementation used by both task and channel send checking.
//
// The function traverses struct fields recursively to find any @nosend types
// that might be embedded within composite types. A visited set prevents
// infinite recursion with recursive struct definitions.
//
// Parameters:
//   - typeID: The type to check for nested @nosend fields
//   - span: Source location for error reporting
//   - diagCode: The diagnostic code to use (SemaNosendInSpawn or SemaChannelNosendValue)
func (tc *typeChecker) checkNestedNosendWith(typeID types.TypeID, span source.Span, diagCode diag.Code) {
	tc.checkNestedNosendWithVisited(typeID, span, diagCode, make(map[types.TypeID]struct{}))
}

// checkNestedNosendWithVisited is the internal implementation with cycle detection.
func (tc *typeChecker) checkNestedNosendWithVisited(typeID types.TypeID, span source.Span, diagCode diag.Code, visited map[types.TypeID]struct{}) {
	// Prevent infinite recursion with recursive types
	if _, seen := visited[typeID]; seen {
		return
	}
	visited[typeID] = struct{}{}

	// Only structs can have nested fields to check
	structInfo, ok := tc.types.StructInfo(typeID)
	if !ok || structInfo == nil {
		return
	}

	// Check each field for @nosend attribute
	for _, field := range structInfo.Fields {
		fieldType := tc.valueType(field.Type)
		if tc.typeHasAttr(fieldType, "nosend") {
			typeName := tc.typeLabel(typeID)
			fieldTypeName := tc.typeLabel(fieldType)
			if diagCode == diag.SemaNosendInSpawn {
				tc.report(diagCode, span,
					"type '%s' contains @nosend field of type '%s'; use @local spawn", typeName, fieldTypeName)
			} else {
				tc.report(diagCode, span,
					"type '%s' contains @nosend field of type '%s'", typeName, fieldTypeName)
			}
		}
		// Recurse into nested structs
		tc.checkNestedNosendWithVisited(fieldType, span, diagCode, visited)
	}
}
