package mir

import (
	"fmt"

	"surge/internal/hir"
	"surge/internal/symbols"
	"surge/internal/types"
)

// lowerExpr lowers a HIR expression to an MIR operand.
func (l *funcLowerer) lowerExpr(e *hir.Expr, consume bool) (Operand, error) {
	if l == nil {
		return Operand{}, nil
	}
	if e == nil {
		return l.constNothing(types.NoTypeID), nil
	}

	switch e.Kind {
	case hir.ExprLiteral:
		data, ok := e.Data.(hir.LiteralData)
		if !ok {
			return Operand{}, fmt.Errorf("mir: literal: unexpected payload %T", e.Data)
		}
		return l.lowerLiteral(e.Type, data), nil

	case hir.ExprVarRef:
		data, ok := e.Data.(hir.VarRefData)
		if !ok {
			return Operand{}, fmt.Errorf("mir: var ref: unexpected payload %T", e.Data)
		}
		if !data.SymbolID.IsValid() {
			return Operand{}, fmt.Errorf("mir: unsupported global value reference %q", data.Name)
		}
		ty := e.Type
		local, ok := l.symToLocal[data.SymbolID]
		if ok {
			if ty == types.NoTypeID && l.f != nil {
				idx := int(local)
				if idx >= 0 && idx < len(l.f.Locals) {
					if lty := l.f.Locals[idx].Type; lty != types.NoTypeID {
						ty = lty
					}
				}
			}
			return l.placeOperand(Place{Local: local}, ty, consume), nil
		}
		if l.symToGlobal != nil {
			if global, ok := l.symToGlobal[data.SymbolID]; ok {
				if ty == types.NoTypeID && l.out != nil {
					idx := int(global)
					if idx >= 0 && idx < len(l.out.Globals) {
						if gty := l.out.Globals[idx].Type; gty != types.NoTypeID {
							ty = gty
						}
					}
				}
				return l.placeOperand(Place{Kind: PlaceGlobal, Global: global}, ty, consume), nil
			}
		}
		if op, handled, err := l.lowerConstValue(data.SymbolID, consume); handled {
			return op, err
		}
		if ty == types.NoTypeID && l.symbols != nil && l.symbols.Table != nil && l.symbols.Table.Symbols != nil {
			if sym := l.symbols.Table.Symbols.Get(data.SymbolID); sym != nil && sym.Type != types.NoTypeID {
				ty = sym.Type
			}
		}
		if ty == types.NoTypeID && l.mono != nil && l.mono.FuncBySym != nil && l.types != nil {
			if mf := l.mono.FuncBySym[data.SymbolID]; mf != nil {
				if mf.Func != nil {
					paramTypes := make([]types.TypeID, 0, len(mf.Func.Params))
					for _, p := range mf.Func.Params {
						paramTypes = append(paramTypes, p.Type)
					}
					ty = l.types.RegisterFn(paramTypes, mf.Func.Result)
				} else if ty == types.NoTypeID && l.symbols != nil && l.symbols.Table != nil && l.symbols.Table.Symbols != nil {
					if sym := l.symbols.Table.Symbols.Get(mf.OrigSym); sym != nil && sym.Type != types.NoTypeID {
						ty = sym.Type
					}
				}
			}
		}
		if l.types != nil && ty != types.NoTypeID {
			if tt, ok := l.types.Lookup(resolveAlias(l.types, ty)); ok && tt.Kind == types.KindFn {
				return Operand{
					Kind: OperandConst,
					Type: ty,
					Const: Const{
						Kind: ConstFn,
						Type: ty,
						Sym:  data.SymbolID,
					},
				}, nil
			}
		}
		if l.symbols != nil && l.symbols.Table != nil && l.symbols.Table.Symbols != nil {
			if sym := l.symbols.Table.Symbols.Get(data.SymbolID); sym != nil {
				if sym.Kind == symbols.SymbolFunction || sym.Kind == symbols.SymbolTag {
					return Operand{
						Kind: OperandConst,
						Type: ty,
						Const: Const{
							Kind: ConstFn,
							Type: ty,
							Sym:  data.SymbolID,
						},
					}, nil
				}
			}
		}
		if l.mono != nil && l.mono.FuncBySym != nil {
			if _, ok := l.mono.FuncBySym[data.SymbolID]; ok {
				return Operand{
					Kind: OperandConst,
					Type: ty,
					Const: Const{
						Kind: ConstFn,
						Type: ty,
						Sym:  data.SymbolID,
					},
				}, nil
			}
		}
		return Operand{}, fmt.Errorf("mir: unknown local symbol %d (%s)", data.SymbolID, data.Name)

	case hir.ExprUnaryOp:
		return l.lowerUnaryOpExpr(e, consume)

	case hir.ExprBinaryOp:
		return l.lowerBinaryOpExpr(e, consume)

	case hir.ExprCast:
		return l.lowerCastExpr(e, consume)

	case hir.ExprFieldAccess:
		return l.lowerFieldAccessExpr(e, consume)

	case hir.ExprIndex:
		return l.lowerIndexExpr(e, consume)

	case hir.ExprStructLit:
		return l.lowerStructLitExpr(e, consume)

	case hir.ExprArrayLit:
		return l.lowerArrayLitExpr(e, consume)

	case hir.ExprMapLit:
		return l.lowerMapLitExpr(e, consume)

	case hir.ExprTupleLit:
		return l.lowerTupleLitExpr(e, consume)

	case hir.ExprTagTest:
		return l.lowerTagTestExpr(e, consume)

	case hir.ExprTagPayload:
		return l.lowerTagPayloadExpr(e, consume)

	case hir.ExprIterInit:
		return l.lowerIterInitExpr(e, consume)

	case hir.ExprIterNext:
		return l.lowerIterNextExpr(e, consume)

	case hir.ExprCall:
		return l.lowerCallExpr(e, consume)

	case hir.ExprIf:
		data, ok := e.Data.(hir.IfData)
		if !ok {
			return Operand{}, fmt.Errorf("mir: if-expr: unexpected payload %T", e.Data)
		}
		return l.lowerIfExpr(e, data, consume)

	case hir.ExprSelect:
		data, ok := e.Data.(hir.SelectData)
		if !ok {
			return Operand{}, fmt.Errorf("mir: select-expr: unexpected payload %T", e.Data)
		}
		return l.lowerSelectExpr(e, data, false, consume)

	case hir.ExprRace:
		data, ok := e.Data.(hir.SelectData)
		if !ok {
			return Operand{}, fmt.Errorf("mir: race-expr: unexpected payload %T", e.Data)
		}
		return l.lowerSelectExpr(e, data, true, consume)

	case hir.ExprBlock:
		data, ok := e.Data.(hir.BlockExprData)
		if !ok {
			return Operand{}, fmt.Errorf("mir: block-expr: unexpected payload %T", e.Data)
		}
		return l.lowerBlockExpr(e, data, consume)

	case hir.ExprAwait:
		return l.lowerAwaitExpr(e, consume)

	case hir.ExprTask:
		return l.lowerTaskExpr(e, consume)

	case hir.ExprSpawn:
		return l.lowerSpawnExpr(e, consume)

	case hir.ExprAsync:
		return l.lowerAsyncExpr(e, consume)

	case hir.ExprBlocking:
		return l.lowerBlockingExpr(e, consume)

	case hir.ExprCompare:
		return Operand{}, fmt.Errorf("mir: compare must be normalized before MIR lowering")

	default:
		return Operand{}, fmt.Errorf("mir: unsupported expr kind %s", e.Kind)
	}
}

// lowerUnaryOpExpr lowers a unary operation expression.
