package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/types"
)

// returnOriginSelectHeadRefusal names a `select` or `race` head that is not one of the awaitable operations the
// checker admits. isSelectAwaitableExpr (type_expr_select.go) refuses every other shape, so on a checked program
// this row never stands; it keeps the transfer fail-closed if that ever changes.
const returnOriginSelectHeadRefusal = "a `select` or `race` head that is not an awaitable operation needs its origin transfer"

// returnOriginSelectSendRefusal names a send head whose value, or whose channel's element, could carry something of
// this frame into the channel's ring, which outlives the frame.
const returnOriginSelectSendRefusal = "a `select` or `race` send whose payload can hold a reference, a storage loan or a task needs its payload origin"

// selectTransfer is the transfer of `select { … }` and `race { … }` (expression kinds 20 and 21).
//
// A head is not an expression the checker types: typeSelectAwaitExpr types only its operands -- the task, the
// channel, the sent value, a timeout's task and delay -- and MIR lowers only those (lowerSelectAwaitExpr), all of
// them before the one InstrSelect, whichever arm wins. So every head's operands are walked in arm order, each by its
// own transfer. A head's result is never delivered: the select writes only the winner's index, a received value is
// destroyed in a sink, and a joined task's result stays in its record, where a later `.await()` of that task meets
// the ordinary await transfer. A sent value goes into the channel's ring only if its type and the channel's element
// type are inert, as an anchored operation's (anchoredOperation). Exactly one arm's result then runs, from the state
// every head left: the value is the join of the arms' values and the flow the join of their flows, as a ternary's.
// What a task named in a head borrows while it runs is the task check's (owner ruling 2026-09-15): a head's join
// releases pins only in its own arm, and neither a race's cancel nor a timeout releases any (task_select_arm_joins.go).
func (b *returnOriginBody) selectTransfer(id ast.ExprID, kind ast.ExprKind, env returnOriginEnv, targets returnOriginTargets) (returnOriginExprResult, error) {
	u := b.function.unit
	span := u.Builder.Exprs.Get(id).Span
	var (
		data *ast.ExprSelectData
		ok   bool
	)
	if kind == ast.ExprRace {
		data, ok = u.Builder.Exprs.Race(id)
	} else {
		data, ok = u.Builder.Exprs.Select(id)
	}
	if !ok || data == nil {
		return returnOriginExprResult{}, fmt.Errorf("return origins: select at %v has no arms", span)
	}
	flow := returnOriginFlow{normal: env}
	for _, arm := range data.Arms {
		if arm.IsDefault {
			continue
		}
		head, known := b.selectHead(arm.Await)
		if !known {
			b.pending(arm.Span, returnOriginSelectHeadRefusal)
		}
		for _, operand := range head.operands {
			var err error
			flow, err = flow.then(func(next returnOriginEnv) (returnOriginFlow, error) {
				out, headErr := b.expr(operand, next, targets)
				return out.flow, headErr
			})
			if err != nil {
				return returnOriginExprResult{}, err
			}
		}
		if head.payload.IsValid() && flow.normal.reachable && !b.selectSendInert(head) {
			b.pending(u.Builder.Exprs.Get(head.payload).Span, returnOriginSelectSendRefusal)
		}
	}
	out := returnOriginExprResult{flow: flow.clone()}
	out.flow.normal = returnOriginEnv{}
	if !flow.normal.reachable {
		return out, nil
	}
	for _, arm := range data.Arms {
		result, err := b.expr(arm.Result, flow.normal.clone(), targets)
		if err != nil {
			return returnOriginExprResult{}, err
		}
		out.flow = out.flow.join(result.flow)
		out.value = out.value.join(result.value)
	}
	return out, nil
}

// returnOriginSelectHead is one head as the checker reads it: its operands in evaluation order, the channel of a
// send and the value it sends.
type returnOriginSelectHead struct {
	operands         []ast.ExprID
	channel, payload ast.ExprID
}

// selectHead unwraps a head exactly as the checker does (unwrapSelectAwaitExpr, type_expr_select.go): any number of
// groups, then at most one one-statement block whose `return` or `ret` value is the head -- that statement is never
// executed as one, and nothing after the block's value is unwrapped again. The operation is then `X.await()`,
// `await(X)`, an await expression, `X.recv()`, `X.send(V)` or `timeout(X, D)`.
func (b *returnOriginBody) selectHead(id ast.ExprID) (returnOriginSelectHead, bool) {
	u := b.function.unit
	exprs, stmts := u.Builder.Exprs, u.Builder.Stmts
	for id.IsValid() {
		node := exprs.Get(id)
		if node == nil {
			return returnOriginSelectHead{}, false
		}
		if node.Kind == ast.ExprGroup {
			group, ok := exprs.Group(id)
			if !ok || group == nil {
				return returnOriginSelectHead{}, false
			}
			id = group.Inner
			continue
		}
		if node.Kind == ast.ExprBlock {
			block, ok := exprs.Block(id)
			if !ok || block == nil || len(block.Stmts) != 1 {
				return returnOriginSelectHead{}, false
			}
			switch stmt := stmts.Get(block.Stmts[0]); {
			case stmt == nil:
				return returnOriginSelectHead{}, false
			case stmt.Kind == ast.StmtReturn:
				id = stmts.Return(block.Stmts[0]).Expr
			case stmt.Kind == ast.StmtRet:
				id = stmts.Ret(block.Stmts[0]).Expr
			default:
				return returnOriginSelectHead{}, false
			}
		}
		break
	}
	if !id.IsValid() {
		return returnOriginSelectHead{}, false
	}
	switch exprs.Get(id).Kind {
	case ast.ExprAwait:
		data, ok := exprs.Await(id)
		if !ok || data == nil {
			return returnOriginSelectHead{}, false
		}
		return returnOriginSelectHead{operands: []ast.ExprID{data.Value}}, true
	case ast.ExprCall:
		return b.selectCallHead(id)
	}
	return returnOriginSelectHead{}, false
}

func (b *returnOriginBody) selectCallHead(id ast.ExprID) (returnOriginSelectHead, bool) {
	u := b.function.unit
	exprs := u.Builder.Exprs
	call, ok := exprs.Call(id)
	if !ok || call == nil {
		return returnOriginSelectHead{}, false
	}
	if member, ok := exprs.Member(call.Target); ok && member != nil {
		name, _ := u.Builder.StringsInterner.Lookup(member.Field)
		switch {
		case (name == "await" || name == "recv") && len(call.Args) == 0:
			return returnOriginSelectHead{operands: []ast.ExprID{member.Target}}, true
		case name == "send" && len(call.Args) == 1:
			value := call.Args[0].Value
			return returnOriginSelectHead{operands: []ast.ExprID{member.Target, value}, channel: member.Target, payload: value}, true
		}
		return returnOriginSelectHead{}, false
	}
	if ident, ok := exprs.Ident(call.Target); ok && ident != nil {
		name, _ := u.Builder.StringsInterner.Lookup(ident.Name)
		switch {
		case name == "await" && len(call.Args) == 1:
			return returnOriginSelectHead{operands: []ast.ExprID{call.Args[0].Value}}, true
		case name == "timeout" && len(call.Args) == 2:
			return returnOriginSelectHead{operands: []ast.ExprID{call.Args[0].Value, call.Args[1].Value}}, true
		}
	}
	return returnOriginSelectHead{}, false
}

// selectSendInert says the sent value and the element of the channel it enters can carry nothing of this frame. The
// channel is read through a `far` or a reference wrapper; a channel whose element cannot be read is not inert.
func (b *returnOriginBody) selectSendInert(head returnOriginSelectHead) bool {
	u := b.function.unit
	in := u.Sema.TypeInterner
	if !b.crossingInert(u.Sema.ExprTypes[head.payload]) {
		return false
	}
	channel := u.Sema.ExprTypes[head.channel]
	for range 4 {
		t, ok := in.Lookup(channel)
		if !ok || (t.Kind != types.KindFar && t.Kind != types.KindReference) {
			break
		}
		channel = t.Elem
	}
	payloads, handle := in.RuntimeHandlePayloads(channel)
	return handle && in.IsRuntimeHandleType(channel) && len(payloads) == 1 && b.crossingInert(payloads[0])
}
