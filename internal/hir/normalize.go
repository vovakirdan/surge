//nolint:errcheck // HIR nodes are checked by construction; Kind implies the Data payload type.
package hir

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// NormalizeModule desugars high-level HIR constructs into a smaller, more uniform core.
//
// Current goals (v1):
//   - remove ExprCompare
//   - remove StmtFor (classic + for-in)
//
// This is a best-effort pass meant for analysis/debug and later MIR lowering; it must not
// change sema diagnostics and does not re-run type checking.
func NormalizeModule(m *Module) error {
	if m == nil {
		return nil
	}
	for _, fn := range m.Funcs {
		ctx := &normCtx{mod: m, fn: fn, nextTemp: 1}
		if err := normalizeFunc(ctx, fn); err != nil {
			return err
		}
	}
	return nil
}

// NormalizeFunc normalizes a single HIR function.
//
// Prefer NormalizeModule so normalization can consult module-level sema metadata when available.
func NormalizeFunc(fn *Func) error {
	if fn == nil {
		return nil
	}
	ctx := &normCtx{fn: fn, nextTemp: 1}
	return normalizeFunc(ctx, fn)
}

type normCtx struct {
	mod      *Module
	fn       *Func
	nextTemp uint32
}

func normalizeFunc(ctx *normCtx, fn *Func) error {
	if ctx == nil || fn == nil || fn.Body == nil {
		return nil
	}
	return normalizeBlock(ctx, fn.Body)
}

func normalizeBlock(ctx *normCtx, b *Block) error {
	if ctx == nil || b == nil {
		return nil
	}

	out := make([]Stmt, 0, len(b.Stmts))
	for i := range b.Stmts {
		s := b.Stmts[i]
		repl, err := normalizeStmt(ctx, &s)
		if err != nil {
			return err
		}
		out = append(out, repl...)
	}
	b.Stmts = out
	return nil
}

func normalizeStmt(ctx *normCtx, s *Stmt) ([]Stmt, error) {
	if ctx == nil || s == nil {
		return nil, nil
	}

	switch s.Kind {
	case StmtLet:
		data := s.Data.(LetData)
		if data.Value != nil {
			if err := normalizeExpr(ctx, data.Value); err != nil {
				return nil, err
			}
		}
		if data.Pattern != nil {
			if err := normalizeExpr(ctx, data.Pattern); err != nil {
				return nil, err
			}
		}
		s.Data = data
		return []Stmt{*s}, nil

	case StmtExpr:
		data := s.Data.(ExprStmtData)
		if data.Expr != nil {
			if err := normalizeExpr(ctx, data.Expr); err != nil {
				return nil, err
			}
		}
		s.Data = data
		return []Stmt{*s}, nil

	case StmtAssign:
		data := s.Data.(AssignData)
		if data.Target != nil {
			if err := normalizeExpr(ctx, data.Target); err != nil {
				return nil, err
			}
		}
		if data.Value != nil {
			if err := normalizeExpr(ctx, data.Value); err != nil {
				return nil, err
			}
		}
		s.Data = data
		return []Stmt{*s}, nil

	case StmtReturn:
		data := s.Data.(ReturnData)
		if data.Value != nil {
			if err := normalizeExpr(ctx, data.Value); err != nil {
				return nil, err
			}
		}
		s.Data = data
		return []Stmt{*s}, nil

	case StmtIf:
		data := s.Data.(IfStmtData)
		if data.Cond != nil {
			if err := normalizeExpr(ctx, data.Cond); err != nil {
				return nil, err
			}
		}
		if err := normalizeBlock(ctx, data.Then); err != nil {
			return nil, err
		}
		if err := normalizeBlock(ctx, data.Else); err != nil {
			return nil, err
		}
		s.Data = data
		return []Stmt{*s}, nil

	case StmtWhile:
		data := s.Data.(WhileData)
		if data.Cond != nil {
			if err := normalizeExpr(ctx, data.Cond); err != nil {
				return nil, err
			}
		}
		if err := normalizeBlock(ctx, data.Body); err != nil {
			return nil, err
		}
		s.Data = data
		return []Stmt{*s}, nil

	case StmtFor:
		return normalizeForStmt(ctx, s)

	case StmtBlock:
		data := s.Data.(BlockStmtData)
		if err := normalizeBlock(ctx, data.Block); err != nil {
			return nil, err
		}
		s.Data = data
		return []Stmt{*s}, nil

	case StmtDrop:
		data := s.Data.(DropData)
		if data.Value != nil {
			if err := normalizeExpr(ctx, data.Value); err != nil {
				return nil, err
			}
		}
		s.Data = data
		return []Stmt{*s}, nil

	default:
		return []Stmt{*s}, nil
	}
}

func normalizeExpr(ctx *normCtx, e *Expr) error {
	if ctx == nil || e == nil {
		return nil
	}

	switch e.Kind {
	case ExprUnaryOp:
		data := e.Data.(UnaryOpData)
		if data.Operand != nil {
			if err := normalizeExpr(ctx, data.Operand); err != nil {
				return err
			}
		}
		e.Data = data
		return nil

	case ExprBinaryOp:
		data := e.Data.(BinaryOpData)
		if data.Left != nil {
			if err := normalizeExpr(ctx, data.Left); err != nil {
				return err
			}
		}
		if data.Right != nil {
			if err := normalizeExpr(ctx, data.Right); err != nil {
				return err
			}
		}
		e.Data = data
		return nil

	case ExprCall:
		data := e.Data.(CallData)
		if data.Callee != nil {
			if err := normalizeExpr(ctx, data.Callee); err != nil {
				return err
			}
		}
		for _, arg := range data.Args {
			if err := normalizeExpr(ctx, arg); err != nil {
				return err
			}
		}
		e.Data = data
		return nil

	case ExprFieldAccess:
		data := e.Data.(FieldAccessData)
		if data.Object != nil {
			if err := normalizeExpr(ctx, data.Object); err != nil {
				return err
			}
		}
		e.Data = data
		return nil

	case ExprIndex:
		data := e.Data.(IndexData)
		if data.Object != nil {
			if err := normalizeExpr(ctx, data.Object); err != nil {
				return err
			}
		}
		if data.Index != nil {
			if err := normalizeExpr(ctx, data.Index); err != nil {
				return err
			}
		}
		e.Data = data
		return nil

	case ExprStructLit:
		data := e.Data.(StructLitData)
		for i := range data.Fields {
			if data.Fields[i].Value != nil {
				if err := normalizeExpr(ctx, data.Fields[i].Value); err != nil {
					return err
				}
			}
		}
		e.Data = data
		return nil

	case ExprArrayLit:
		data := e.Data.(ArrayLitData)
		for _, elem := range data.Elements {
			if err := normalizeExpr(ctx, elem); err != nil {
				return err
			}
		}
		e.Data = data
		return nil

	case ExprMapLit:
		data := e.Data.(MapLitData)
		for i := range data.Entries {
			if data.Entries[i].Key != nil {
				if err := normalizeExpr(ctx, data.Entries[i].Key); err != nil {
					return err
				}
			}
			if data.Entries[i].Value != nil {
				if err := normalizeExpr(ctx, data.Entries[i].Value); err != nil {
					return err
				}
			}
		}
		e.Data = data
		return nil

	case ExprTupleLit:
		data := e.Data.(TupleLitData)
		for _, elem := range data.Elements {
			if err := normalizeExpr(ctx, elem); err != nil {
				return err
			}
		}
		e.Data = data
		return nil

	case ExprCompare:
		return normalizeCompareExpr(ctx, e)

	case ExprSelect, ExprRace:
		data := e.Data.(SelectData)
		for i := range data.Arms {
			if data.Arms[i].Await != nil {
				if err := normalizeExpr(ctx, data.Arms[i].Await); err != nil {
					return err
				}
			}
			if data.Arms[i].Result != nil {
				if err := normalizeExpr(ctx, data.Arms[i].Result); err != nil {
					return err
				}
			}
		}
		e.Data = data
		return nil

	case ExprTagTest:
		data := e.Data.(TagTestData)
		if data.Value != nil {
			if err := normalizeExpr(ctx, data.Value); err != nil {
				return err
			}
		}
		e.Data = data
		return nil

	case ExprTagPayload:
		data := e.Data.(TagPayloadData)
		if data.Value != nil {
			if err := normalizeExpr(ctx, data.Value); err != nil {
				return err
			}
		}
		e.Data = data
		return nil

	case ExprIterInit:
		data := e.Data.(IterInitData)
		if data.Iterable != nil {
			if err := normalizeExpr(ctx, data.Iterable); err != nil {
				return err
			}
		}
		e.Data = data
		return nil

	case ExprIterNext:
		data := e.Data.(IterNextData)
		if data.Iter != nil {
			if err := normalizeExpr(ctx, data.Iter); err != nil {
				return err
			}
		}
		e.Data = data
		return nil

	case ExprIf:
		data := e.Data.(IfData)
		if data.Cond != nil {
			if err := normalizeExpr(ctx, data.Cond); err != nil {
				return err
			}
		}
		if data.Then != nil {
			if err := normalizeExpr(ctx, data.Then); err != nil {
				return err
			}
		}
		if data.Else != nil {
			if err := normalizeExpr(ctx, data.Else); err != nil {
				return err
			}
		}
		e.Data = data
		return nil

	case ExprAwait:
		data := e.Data.(AwaitData)
		if data.Value != nil {
			if err := normalizeExpr(ctx, data.Value); err != nil {
				return err
			}
		}
		e.Data = data
		return nil

	case ExprTask:
		data := e.Data.(TaskData)
		if data.Value != nil {
			if err := normalizeExpr(ctx, data.Value); err != nil {
				return err
			}
		}
		e.Data = data
		return nil

	case ExprSpawn:
		data := e.Data.(SpawnData)
		if data.Value != nil {
			if err := normalizeExpr(ctx, data.Value); err != nil {
				return err
			}
		}
		e.Data = data
		return nil

	case ExprAsync:
		data := e.Data.(AsyncData)
		if err := normalizeBlock(ctx, data.Body); err != nil {
			return err
		}
		e.Data = data
		return nil

	case ExprCast:
		data := e.Data.(CastData)
		if data.Value != nil {
			if err := normalizeExpr(ctx, data.Value); err != nil {
				return err
			}
		}
		e.Data = data
		return nil

	case ExprBlock:
		data := e.Data.(BlockExprData)
		if err := normalizeBlock(ctx, data.Block); err != nil {
			return err
		}
		e.Data = data
		return nil

	default:
		return nil
	}
}

func (ctx *normCtx) boolType() types.TypeID {
	if ctx != nil && ctx.mod != nil && ctx.mod.TypeInterner != nil {
		return ctx.mod.TypeInterner.Builtins().Bool
	}
	return types.NoTypeID
}

func (ctx *normCtx) inferOwnership(ty types.TypeID) Ownership {
	if ctx == nil || ctx.mod == nil || ctx.mod.TypeInterner == nil || ty == types.NoTypeID {
		return OwnershipNone
	}
	t, ok := ctx.mod.TypeInterner.Lookup(ty)
	if !ok {
		return OwnershipNone
	}
	switch t.Kind {
	case types.KindReference:
		if t.Mutable {
			return OwnershipRefMut
		}
		return OwnershipRef
	case types.KindPointer:
		return OwnershipPtr
	case types.KindOwn:
		return OwnershipOwn
	case types.KindInt, types.KindUint, types.KindFloat, types.KindBool:
		return OwnershipCopy
	default:
		return OwnershipNone
	}
}

func (ctx *normCtx) bindingType(sym symbols.SymbolID) types.TypeID {
	if ctx == nil || ctx.mod == nil || ctx.mod.BindingTypes == nil || !sym.IsValid() {
		return types.NoTypeID
	}
	return ctx.mod.BindingTypes[sym]
}

func (ctx *normCtx) isTagSymbol(sym symbols.SymbolID) bool {
	if ctx == nil || ctx.mod == nil || ctx.mod.Symbols == nil || ctx.mod.Symbols.Table == nil || !sym.IsValid() {
		return false
	}
	s := ctx.mod.Symbols.Table.Symbols.Get(sym)
	return s != nil && s.Kind == symbols.SymbolTag
}

func (ctx *normCtx) newTemp(nameHint string) (sym symbols.SymbolID, name string) {
	if ctx == nil {
		return symbols.NoSymbolID, ""
	}
	n := ctx.nextTemp
	ctx.nextTemp++

	// Use a high-bit namespace to avoid colliding with real SymbolIDs allocated by the resolver.
	sym = symbols.SymbolID(0x8000_0000 + n)
	if nameHint == "" {
		nameHint = "tmp"
	}
	name = fmt.Sprintf("__%s%d", nameHint, n)
	return sym, name
}

func (ctx *normCtx) varRef(name string, sym symbols.SymbolID, ty types.TypeID, span source.Span) *Expr {
	return &Expr{
		Kind: ExprVarRef,
		Type: ty,
		Span: span,
		Data: VarRefData{
			Name:     name,
			SymbolID: sym,
		},
	}
}

func (ctx *normCtx) boolLit(v bool, span source.Span) *Expr {
	return &Expr{
		Kind: ExprLiteral,
		Type: ctx.boolType(),
		Span: span,
		Data: LiteralData{
			Kind:      LiteralBool,
			BoolValue: v,
		},
	}
}

func (ctx *normCtx) intLit(v int64, ty types.TypeID, span source.Span) *Expr {
	return &Expr{
		Kind: ExprLiteral,
		Type: ty,
		Span: span,
		Data: LiteralData{
			Kind:     LiteralInt,
			IntValue: v,
		},
	}
}

func (ctx *normCtx) binary(op ast.ExprBinaryOp, left, right *Expr, ty types.TypeID, span source.Span) *Expr {
	return &Expr{
		Kind: ExprBinaryOp,
		Type: ty,
		Span: span,
		Data: BinaryOpData{
			Op:    op,
			Left:  left,
			Right: right,
		},
	}
}
