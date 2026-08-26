package sema

import (
	"fmt"
	"strings"

	"surge/internal/ast"
	"surge/internal/symbols"
	"surge/internal/types"
)

// cloneAdviceSite names an operation that an optional clone sentence hangs on.
//
// Every emitter of user-facing clone advice appears here. That is the point:
// the sentences are one table, so a capability cannot be phrased one way at one
// site and another way at the next, and a new emitter joins the table instead
// of inventing a twelfth phrasing.
type cloneAdviceSite uint8

const (
	adviceMovedTask cloneAdviceSite = iota + 1
	adviceMovedValue
	adviceOwnedParam
	adviceBorrowIntoOwned
	adviceReturnedBorrow
	adviceTaskBorrowsFrameLocal
	adviceReferenceInAggregate
	adviceChannelBorrow
	advicePartialMove
	adviceMoveOutOfSharedBorrow
	adviceCompareArmPayload
)

// cloneAdvice is what one site is allowed to say.
//
// Help is empty when no clause may be offered at all, which is the answer for
// every deferred generic: an undecided type gets silence, not a guess.
type cloneAdvice struct {
	Help  string
	State CloneState
}

// offersCloneCall reports whether the sentence named a clone, which is also the
// condition for attaching an edit that writes one.
func (a cloneAdvice) offersCloneCall() bool { return a.State == CloneValidMethod && a.Help != "" }

// cloneCall is how a clone is spelled in Surge source, everywhere, for every
// ordinary value: the free function, which dispatches to `__clone`.
//
// It is deliberately not `x.__clone()`. That spelling works and the standard
// library still uses it, but it names the magic method rather than the
// operation, and advice that teaches it teaches the wrong thing. `.clone()` is
// reserved for a local `Task<T>`, where it is a task operation and not this one.
func cloneCall(name string) string {
	if name == "" {
		return "clone(...)"
	}
	return fmt.Sprintf("clone(%s)", name)
}

// cloneDefinitionHelp is the one sentence that offers to declare the contract.
// It lives here so every emitter that offers it offers the same signature.
func cloneDefinitionHelp(label string) string {
	return fmt.Sprintf(
		"or give `%s` the contract that already exists: `extern<%s> { fn __clone(self: &%s) -> %s }`",
		label, label, label, label)
}

// adviceCloneState is the checker-time projection of the one clone authority.
//
// The whole-program classifier is not available while a file is being checked —
// a `__clone` may be declared in a module this file only imports, and the
// merged catalog does not exist yet. The checker's magic registry does carry
// local AND imported declarations, so it answers the same question from the
// same vocabulary, and the four states are the classifier's own.
//
// Where it cannot prove a clone it answers CloneNonClonable, and that is safe
// BY DIRECTION: this drives advice, never a refusal. Being wrong costs a
// sentence the author does not get. Answering the other way would cost them a
// sentence telling them to write a call that does not compile, which is the
// defect this table exists to remove.
func (tc *typeChecker) adviceCloneState(subject types.TypeID) CloneState {
	if tc == nil || subject == types.NoTypeID {
		return CloneDeferred
	}
	value := tc.valueType(subject)
	if value == types.NoTypeID {
		value = subject
	}
	if tc.isCopyType(value) {
		return CloneCopy
	}
	if tc.types != nil && types.ContainsGenericParam(tc.types, tc.resolveAlias(value)) {
		return CloneDeferred
	}
	if tc.hasCloneContract(value) {
		return CloneValidMethod
	}
	return CloneNonClonable
}

// hasCloneContract asks the checker's registry for a `__clone(self: &T) -> T`.
func (tc *typeChecker) hasCloneContract(value types.TypeID) bool {
	key := tc.typeKeyForType(value)
	if key == "" {
		return false
	}
	for _, sig := range tc.lookupMagicMethods(key, "__clone") {
		if validCloneSignatureShape(sig) {
			return true
		}
	}
	return false
}

// validCloneSignatureShape holds the declaration to the contract: one borrowed
// self, no other parameters, and the receiver's own type back.
//
// A declaration that fails this is exactly the "declared but wrong shape" case
// the canonical selector refuses, so advising a clone for it would send the
// author to a call that will not resolve.
func validCloneSignatureShape(sig *symbols.FunctionSignature) bool {
	if sig == nil || !sig.HasSelf || len(sig.Params) != 1 || sig.Result == "" {
		return false
	}
	if len(sig.Variadic) > 0 && sig.Variadic[0] {
		return false
	}
	return canonicalTypeKey(stripBorrowTypeKey(sig.Params[0])) == canonicalTypeKey(sig.Result)
}

// stripBorrowTypeKey drops the borrow marker from a self parameter spelling.
func stripBorrowTypeKey(key symbols.TypeKey) symbols.TypeKey {
	text := strings.TrimSpace(string(key))
	for {
		switch {
		case strings.HasPrefix(text, "&mut "):
			text = strings.TrimSpace(strings.TrimPrefix(text, "&mut "))
		case strings.HasPrefix(text, "&"):
			text = strings.TrimSpace(strings.TrimPrefix(text, "&"))
		default:
			return symbols.TypeKey(text)
		}
	}
}

// identNameOf names the expression a sentence is about, when it is a plain
// identifier. Anything else — a field, a call result — has no name the author
// wrote, and the sentence is phrased without one rather than inventing it.
func (tc *typeChecker) identNameOf(expr ast.ExprID) string {
	if tc == nil || tc.builder == nil {
		return ""
	}
	inner := tc.unwrapGroupExpr(expr)
	if ident, ok := tc.builder.Exprs.Ident(inner); ok && ident != nil {
		return tc.lookupName(ident.Name)
	}
	return ""
}

// cloneAdviceFor renders one site's optional clone sentence.
//
// `name` is the binding the sentence may spell. An empty name means the value
// has no name at this position, and the sentence is written without one rather
// than inventing an identifier the author never wrote.
//
// A deferred subject returns nothing at all. That silence is the contract from
// Global Rule 11: an undecided generic must not acquire a clonability
// constraint through a hint, and a hint that guesses is worse than none.
func (tc *typeChecker) cloneAdviceFor(site cloneAdviceSite, subject types.TypeID, name string) cloneAdvice {
	state := tc.adviceCloneState(subject)
	return cloneAdvice{State: state, Help: cloneAdviceSentence(site, state, name)}
}

//nolint:gocyclo // one table, read as a table: eleven sites by four capabilities.
func cloneAdviceSentence(site cloneAdviceSite, state CloneState, name string) string {
	if state == CloneDeferred {
		// The gate lives HERE rather than in the caller so the property holds
		// wherever the table is read from: an undecided generic gets silence,
		// and a future emitter cannot opt out of that by calling one layer down.
		return ""
	}
	subject := "the value"
	if name != "" {
		subject = "`" + name + "`"
	}
	call := cloneCall(name)
	switch site {
	case adviceMovedTask:
		// `.clone()` here is the TASK operation, not the ordinary clone route.
		// It is offered only where the payload can actually be duplicated,
		// because that is exactly when the call is legal.
		if state == CloneNonClonable {
			return "a task handle is affine: consume this one by awaiting it, or move it to whoever awaits it"
		}
		if name == "" {
			return "call `.clone()` on the handle before moving it to keep one for yourself"
		}
		return fmt.Sprintf("call `%s.clone()` before moving it to keep a handle for yourself", name)
	case adviceMovedValue:
		if state == CloneNonClonable {
			return fmt.Sprintf("let the receiver borrow %s instead, or change where ownership lives", subject)
		}
		return fmt.Sprintf("let the receiver borrow %s, or pass a copy: %s", subject, call)
	case adviceOwnedParam:
		if state == CloneNonClonable {
			return fmt.Sprintf("write `own` to give %s away, or change the parameter to take a borrow", subject)
		}
		return fmt.Sprintf("write `own` to give %s away, or keep yours and pass a copy: %s", subject, call)
	case adviceBorrowIntoOwned:
		if state == CloneNonClonable {
			return fmt.Sprintf("move %s itself to give it away, or change the destination's lifetime", subject)
		}
		return fmt.Sprintf("move %s itself to give it away, or keep yours and pass a copy: %s", subject, call)
	case adviceReturnedBorrow:
		if state == CloneNonClonable {
			return fmt.Sprintf("return %s itself to move it out, or change who owns the result", subject)
		}
		return fmt.Sprintf("return %s itself to move it out, or return a copy: %s", subject, call)
	case adviceTaskBorrowsFrameLocal:
		if state == CloneNonClonable {
			return fmt.Sprintf("await the task inside this function, or move %s into it", subject)
		}
		return fmt.Sprintf("await the task inside this function, or spawn it with an owned value: %s itself or %s", subject, call)
	case adviceReferenceInAggregate:
		if state == CloneNonClonable {
			return fmt.Sprintf("store an owned value instead: move %s in, or redesign where it lives", subject)
		}
		return fmt.Sprintf("store an owned value instead: move %s in, or copy it with %s", subject, call)
	case adviceChannelBorrow:
		if state == CloneNonClonable {
			return fmt.Sprintf("send %s itself to give it away, or restructure who owns it", subject)
		}
		return fmt.Sprintf("send %s itself to give it away, or send a copy: %s", subject, call)
	case advicePartialMove:
		if state == CloneNonClonable {
			return fmt.Sprintf("if you did not mean to empty it, borrow the field instead (`&%s`)", name)
		}
		return fmt.Sprintf("if you did not mean to empty it, borrow the field instead (`&%s`), or take a copy: %s", name, call)
	case adviceMoveOutOfSharedBorrow:
		if state == CloneNonClonable {
			return "keep working through the borrow: this reference only lends what it points at"
		}
		return fmt.Sprintf("pay for a copy with %s, or keep working through the borrow", call)
	case adviceCompareArmPayload:
		if state == CloneNonClonable {
			return fmt.Sprintf("build the answer from %s without giving it away", subject)
		}
		return fmt.Sprintf("pay for a copy with %s, or build the answer from %s without giving it away", call, subject)
	default:
		return ""
	}
}
