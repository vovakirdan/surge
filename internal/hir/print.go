//nolint:errcheck,gocritic // Type assertions are checked by construction; ifElseChain is clearer than switch
package hir

import (
	"fmt"
	"io"
	"strings"

	"surge/internal/types"
)

// DumpOptions configures HIR dumping.
type DumpOptions struct {
	EmitBorrow bool
}

// Printer is used to dump HIR to text format.
type Printer struct {
	w        io.Writer
	interner *types.Interner
	indent   int
	opts     DumpOptions
}

// NewPrinter creates a new HIR printer.
func NewPrinter(w io.Writer, interner *types.Interner) *Printer {
	return NewPrinterWithOptions(w, interner, DumpOptions{})
}

// NewPrinterWithOptions creates a new HIR printer with the given options.
func NewPrinterWithOptions(w io.Writer, interner *types.Interner, opts DumpOptions) *Printer {
	return &Printer{w: w, interner: interner, opts: opts}
}

// Dump writes the HIR module to the writer.
func Dump(w io.Writer, m *Module, interner *types.Interner) error {
	return DumpWithOptions(w, m, interner, DumpOptions{})
}

// DumpWithOptions writes a formatted HIR module to the provided writer with options.
func DumpWithOptions(w io.Writer, m *Module, interner *types.Interner, opts DumpOptions) error {
	p := NewPrinterWithOptions(w, interner, opts)
	return p.PrintModule(m)
}

// PrintModule prints a complete module.
func (p *Printer) PrintModule(m *Module) error {
	p.printf("module %s\n", m.Name)
	if m.Path != "" {
		p.printf("  path: %s\n", m.Path)
	}
	p.printf("\n")

	// Print type declarations
	for _, td := range m.Types {
		p.printf("type %s <%s> (sym=%d, type=%d)\n", td.Name, td.Kind, td.SymbolID, td.TypeID)
	}
	if len(m.Types) > 0 {
		p.printf("\n")
	}

	// Print constants
	for _, c := range m.Consts {
		p.printf("const %s: %s = ", c.Name, p.typeStr(c.Type))
		if c.Value != nil {
			p.printExpr(c.Value)
		} else {
			p.printf("<nil>")
		}
		p.printf(" (sym=%d)\n", c.SymbolID)
	}
	if len(m.Consts) > 0 {
		p.printf("\n")
	}

	// Print globals
	for _, g := range m.Globals {
		mut := ""
		if g.IsMut {
			mut = "mut "
		}
		p.printf("let %s%s: %s", mut, g.Name, p.typeStr(g.Type))
		if g.Value != nil {
			p.printf(" = ")
			p.printExpr(g.Value)
		}
		p.printf(" (sym=%d)\n", g.SymbolID)
	}
	if len(m.Globals) > 0 {
		p.printf("\n")
	}

	// Print functions
	for _, f := range m.Funcs {
		if err := p.PrintFunc(f); err != nil {
			return err
		}
		p.printf("\n")
	}

	return nil
}

// PrintFunc prints a function.
func (p *Printer) PrintFunc(f *Func) error {
	// Print function flags/attributes
	if f.Flags.HasFlag(FuncIntrinsic) {
		p.printf("@intrinsic ")
	}
	if f.Flags.HasFlag(FuncEntrypoint) {
		p.printf("@entrypoint ")
	}
	if f.Flags.HasFlag(FuncOverload) {
		p.printf("@overload ")
	}
	if f.Flags.HasFlag(FuncOverride) {
		p.printf("@override ")
	}
	if f.Flags.HasFlag(FuncPublic) {
		p.printf("pub ")
	}
	if f.Flags.HasFlag(FuncAsync) {
		p.printf("async ")
	}

	p.printf("fn %s", f.Name)

	// Generic parameters
	if len(f.GenericParams) > 0 {
		p.printf("<")
		for i, gp := range f.GenericParams {
			if i > 0 {
				p.printf(", ")
			}
			p.printf("%s", gp.Name)
			if len(gp.Bounds) > 0 {
				p.printf(": ")
				for j, b := range gp.Bounds {
					if j > 0 {
						p.printf(" + ")
					}
					p.printf("%s", p.typeStr(b))
				}
			}
		}
		p.printf(">")
	}

	// Parameters
	p.printf("(")
	for i, param := range f.Params {
		if i > 0 {
			p.printf(", ")
		}
		p.printf("%s: %s", param.Name, p.typeStr(param.Type))
		if param.Ownership != OwnershipNone {
			p.printf(" [%s]", param.Ownership)
		}
	}
	p.printf(")")

	// Return type
	if f.Result != types.NoTypeID {
		p.printf(" -> %s", p.typeStr(f.Result))
	}

	p.printf(" (id=%d, sym=%d)", f.ID, f.SymbolID)

	// Body
	if f.Body != nil {
		p.printf(" {\n")
		p.indent++
		p.printBlock(f.Body)
		p.indent--
		p.printIndent()
		p.printf("}")
	}

	p.printf("\n")

	if p.opts.EmitBorrow {
		p.printBorrowAndMovePlan(f)
	}
	return nil
}

func (p *Printer) printBlock(b *Block) {
	for _, stmt := range b.Stmts {
		p.printStmt(&stmt)
	}
}

func (p *Printer) printStmt(s *Stmt) {
	p.printIndent()

	switch s.Kind {
	case StmtLet:
		data := s.Data.(LetData)
		if data.IsConst {
			p.printf("const ")
		} else if data.IsMut {
			p.printf("let mut ")
		} else {
			p.printf("let ")
		}
		if data.Pattern != nil {
			p.printExpr(data.Pattern)
		} else {
			p.printf("%s", data.Name)
		}
		p.printf(": %s", p.typeStr(data.Type))
		if data.Ownership != OwnershipNone {
			p.printf(" [%s]", data.Ownership)
		}
		if data.Value != nil {
			p.printf(" = ")
			p.printExpr(data.Value)
		}
		p.printf("\n")

	case StmtExpr:
		data := s.Data.(ExprStmtData)
		p.printExpr(data.Expr)
		p.printf("\n")

	case StmtAssign:
		data := s.Data.(AssignData)
		p.printExpr(data.Target)
		p.printf(" = ")
		p.printExpr(data.Value)
		p.printf("\n")

	case StmtReturn:
		data := s.Data.(ReturnData)
		p.printf("return")
		if data.Value != nil {
			p.printf(" ")
			p.printExpr(data.Value)
		}
		p.printf("\n")

	case StmtBreak:
		p.printf("break\n")

	case StmtContinue:
		p.printf("continue\n")

	case StmtIf:
		data := s.Data.(IfStmtData)
		p.printf("if ")
		p.printExpr(data.Cond)
		p.printf(" {\n")
		p.indent++
		p.printBlock(data.Then)
		p.indent--
		p.printIndent()
		p.printf("}")
		if data.Else != nil {
			p.printf(" else {\n")
			p.indent++
			p.printBlock(data.Else)
			p.indent--
			p.printIndent()
			p.printf("}")
		}
		p.printf("\n")

	case StmtWhile:
		data := s.Data.(WhileData)
		p.printf("while ")
		p.printExpr(data.Cond)
		p.printf(" {\n")
		p.indent++
		p.printBlock(data.Body)
		p.indent--
		p.printIndent()
		p.printf("}\n")

	case StmtFor:
		data := s.Data.(ForData)
		if data.Kind == ForClassic {
			p.printf("for ")
			if data.Init != nil {
				p.printStmtInline(data.Init)
			}
			p.printf("; ")
			if data.Cond != nil {
				p.printExpr(data.Cond)
			}
			p.printf("; ")
			if data.Post != nil {
				p.printExpr(data.Post)
			}
		} else {
			p.printf("for %s: %s in ", data.VarName, p.typeStr(data.VarType))
			p.printExpr(data.Iterable)
		}
		p.printf(" {\n")
		p.indent++
		p.printBlock(data.Body)
		p.indent--
		p.printIndent()
		p.printf("}\n")

	case StmtBlock:
		data := s.Data.(BlockStmtData)
		p.printf("{\n")
		p.indent++
		p.printBlock(data.Block)
		p.indent--
		p.printIndent()
		p.printf("}\n")

	case StmtDrop:
		data := s.Data.(DropData)
		p.printf("drop ")
		p.printExpr(data.Value)
		p.printf("\n")

	default:
		p.printf("<%s>\n", s.Kind)
	}
}

// printStmtInline prints a statement without leading indent or trailing newline
func (p *Printer) printStmtInline(s *Stmt) {
	switch s.Kind {
	case StmtLet:
		data := s.Data.(LetData)
		if data.IsMut {
			p.printf("let mut ")
		} else {
			p.printf("let ")
		}
		p.printf("%s: %s", data.Name, p.typeStr(data.Type))
		if data.Value != nil {
			p.printf(" = ")
			p.printExpr(data.Value)
		}
	case StmtExpr:
		data := s.Data.(ExprStmtData)
		p.printExpr(data.Expr)
	case StmtAssign:
		data := s.Data.(AssignData)
		p.printExpr(data.Target)
		p.printf(" = ")
		p.printExpr(data.Value)
	default:
		p.printf("<%s>", s.Kind)
	}
}

func (p *Printer) printExpr(e *Expr) {
	p.printExprWithType(e, true)
}

// printExprWithType prints an expression, optionally with type annotation.
func (p *Printer) printExprWithType(e *Expr, showType bool) {
	if e == nil {
		p.printf("<nil>")
		return
	}

	// Don't show type on simple literals to reduce noise
	skipType := false

	switch e.Kind {
	case ExprLiteral:
		data := e.Data.(LiteralData)
		switch data.Kind {
		case LiteralInt:
			p.printf("%d", data.IntValue)
		case LiteralFloat:
			p.printf("%g", data.FloatValue)
		case LiteralBool:
			p.printf("%t", data.BoolValue)
		case LiteralString:
			p.printf("%q", data.StringValue)
		case LiteralNothing:
			p.printf("nothing")
		default:
			p.printf("<literal>")
		}
		// Skip type for literals unless it's non-obvious
		skipType = true

	case ExprVarRef:
		data := e.Data.(VarRefData)
		p.printf("%s", data.Name)
		// Skip type on variable references - context shows type already
		skipType = true

	case ExprUnaryOp:
		data := e.Data.(UnaryOpData)
		p.printf("(%v ", data.Op)
		p.printExprWithType(data.Operand, false) // Skip type on operand
		p.printf(")")

	case ExprBinaryOp:
		data := e.Data.(BinaryOpData)
		p.printf("(")
		p.printExprWithType(data.Left, false) // Skip type on operands
		p.printf(" %v ", data.Op)
		p.printExprWithType(data.Right, false) // Skip type on operands
		p.printf(")")

	case ExprCall:
		data := e.Data.(CallData)
		// Print callee without type annotation for cleaner output
		p.printExprWithType(data.Callee, false)
		p.printf("(")
		for i, arg := range data.Args {
			if i > 0 {
				p.printf(", ")
			}
			p.printExprWithType(arg, false) // Skip types on args for cleaner output
		}
		p.printf(")")

	case ExprFieldAccess:
		data := e.Data.(FieldAccessData)
		p.printExpr(data.Object)
		if data.FieldName != "" {
			p.printf(".%s", data.FieldName)
		} else if data.FieldIdx >= 0 {
			p.printf(".%d", data.FieldIdx)
		} else {
			p.printf(".?")
		}

	case ExprIndex:
		data := e.Data.(IndexData)
		p.printExpr(data.Object)
		p.printf("[")
		p.printExpr(data.Index)
		p.printf("]")

	case ExprStructLit:
		data := e.Data.(StructLitData)
		p.printf("%s { ", data.TypeName)
		for i, f := range data.Fields {
			if i > 0 {
				p.printf(", ")
			}
			p.printf("%s = ", f.Name)
			p.printExpr(f.Value)
		}
		p.printf(" }")

	case ExprArrayLit:
		data := e.Data.(ArrayLitData)
		p.printf("[")
		for i, elem := range data.Elements {
			if i > 0 {
				p.printf(", ")
			}
			p.printExpr(elem)
		}
		p.printf("]")

	case ExprMapLit:
		data := e.Data.(MapLitData)
		p.printf("{")
		for i, entry := range data.Entries {
			if i > 0 {
				p.printf(", ")
			}
			p.printExpr(entry.Key)
			p.printf(" => ")
			p.printExpr(entry.Value)
		}
		p.printf("}")

	case ExprTupleLit:
		data := e.Data.(TupleLitData)
		p.printf("(")
		for i, elem := range data.Elements {
			if i > 0 {
				p.printf(", ")
			}
			p.printExpr(elem)
		}
		p.printf(")")

	case ExprCompare:
		data := e.Data.(CompareData)
		p.printf("compare ")
		p.printExpr(data.Value)
		p.printf(" {\n")
		p.indent++
		for _, arm := range data.Arms {
			p.printIndent()
			p.printExpr(arm.Pattern)
			if arm.Guard != nil {
				p.printf(" if ")
				p.printExpr(arm.Guard)
			}
			p.printf(" => ")
			p.printExpr(arm.Result)
			p.printf("\n")
		}
		p.indent--
		p.printIndent()
		p.printf("}")

	case ExprSelect, ExprRace:
		data := e.Data.(SelectData)
		kindLabel := "select"
		if e.Kind == ExprRace {
			kindLabel = "race"
		}
		p.printf("%s {\n", kindLabel)
		p.indent++
		for _, arm := range data.Arms {
			p.printIndent()
			if arm.IsDefault {
				p.printf("default")
			} else {
				p.printExpr(arm.Await)
			}
			p.printf(" => ")
			p.printExpr(arm.Result)
			p.printf("\n")
		}
		p.indent--
		p.printIndent()
		p.printf("}")

	case ExprTagTest:
		data := e.Data.(TagTestData)
		p.printf("tag_test(")
		p.printExprWithType(data.Value, false)
		p.printf(", %s)", data.TagName)

	case ExprTagPayload:
		data := e.Data.(TagPayloadData)
		p.printf("tag_payload(")
		p.printExprWithType(data.Value, false)
		p.printf(", %s, %d)", data.TagName, data.Index)

	case ExprIterInit:
		data := e.Data.(IterInitData)
		p.printf("iter_init(")
		p.printExprWithType(data.Iterable, false)
		p.printf(")")

	case ExprIterNext:
		data := e.Data.(IterNextData)
		p.printf("iter_next(")
		p.printExprWithType(data.Iter, false)
		p.printf(")")

	case ExprIf:
		data := e.Data.(IfData)
		p.printf("if ")
		p.printExpr(data.Cond)
		p.printf(" { ")
		p.printExpr(data.Then)
		p.printf(" }")
		if data.Else != nil {
			p.printf(" else { ")
			p.printExpr(data.Else)
			p.printf(" }")
		}

	case ExprAwait:
		data := e.Data.(AwaitData)
		p.printExpr(data.Value)
		p.printf(".await()")

	case ExprTask:
		data := e.Data.(TaskData)
		p.printf("task ")
		p.printExpr(data.Value)

	case ExprSpawn:
		data := e.Data.(SpawnData)
		p.printf("spawn ")
		p.printExpr(data.Value)

	case ExprAsync:
		data := e.Data.(AsyncData)
		if data.Failfast {
			p.printf("@failfast ")
		}
		p.printf("async {\n")
		p.indent++
		p.printBlock(data.Body)
		p.indent--
		p.printIndent()
		p.printf("}")

	case ExprCast:
		data := e.Data.(CastData)
		p.printExpr(data.Value)
		p.printf(" to %s", p.typeStr(data.TargetTy))

	case ExprBlock:
		data := e.Data.(BlockExprData)
		p.printf("{\n")
		p.indent++
		p.printBlock(data.Block)
		p.indent--
		p.printIndent()
		p.printf("}")

	default:
		p.printf("<%s>", e.Kind)
	}

	// Optionally add type annotation (skip for simple literals to reduce noise)
	if showType && !skipType && e.Type != types.NoTypeID {
		p.printf(": %s", p.typeStr(e.Type))
	}
}

func (p *Printer) printIndent() {
	for range p.indent {
		p.printf("  ")
	}
}

func (p *Printer) printf(format string, args ...interface{}) {
	fmt.Fprintf(p.w, format, args...)
}

func (p *Printer) typeStr(id types.TypeID) string {
	if id == types.NoTypeID {
		return "?"
	}
	if p.interner == nil {
		return fmt.Sprintf("type#%d", id)
	}
	t, ok := p.interner.Lookup(id)
	if !ok {
		return fmt.Sprintf("type#%d", id)
	}
	return p.formatType(t, id)
}

func (p *Printer) formatType(t types.Type, id types.TypeID) string {
	switch t.Kind {
	case types.KindUnit:
		return "()"
	case types.KindNothing:
		return "nothing"
	case types.KindBool:
		return "bool"
	case types.KindString:
		return "string"
	case types.KindInt:
		return p.formatIntType(t.Width, true)
	case types.KindUint:
		return p.formatIntType(t.Width, false)
	case types.KindFloat:
		return p.formatFloatType(t.Width)
	case types.KindPointer:
		return fmt.Sprintf("*%s", p.typeStr(t.Elem))
	case types.KindReference:
		if t.Mutable {
			return fmt.Sprintf("&mut %s", p.typeStr(t.Elem))
		}
		return fmt.Sprintf("&%s", p.typeStr(t.Elem))
	case types.KindOwn:
		return fmt.Sprintf("own %s", p.typeStr(t.Elem))
	case types.KindArray:
		if t.Count == types.ArrayDynamicLength {
			return fmt.Sprintf("[%s]", p.typeStr(t.Elem))
		}
		return fmt.Sprintf("[%s; %d]", p.typeStr(t.Elem), t.Count)
	case types.KindStruct, types.KindAlias, types.KindUnion, types.KindEnum, types.KindTuple, types.KindFn:
		// For nominal types, we would need more metadata to print the actual name
		return fmt.Sprintf("type#%d", id)
	default:
		return fmt.Sprintf("type#%d", id)
	}
}

func (p *Printer) formatIntType(width types.Width, signed bool) string {
	prefix := "int"
	if !signed {
		prefix = "uint"
	}
	switch width {
	case types.WidthAny:
		return prefix
	case types.Width8:
		return prefix + "8"
	case types.Width16:
		return prefix + "16"
	case types.Width32:
		return prefix + "32"
	case types.Width64:
		return prefix + "64"
	default:
		return prefix
	}
}

func (p *Printer) formatFloatType(width types.Width) string {
	switch width {
	case types.WidthAny:
		return "float"
	case types.Width16:
		return "float16"
	case types.Width32:
		return "float32"
	case types.Width64:
		return "float64"
	default:
		return "float"
	}
}

// ExprString returns a compact string representation of an expression.
func ExprString(e *Expr, interner *types.Interner) string {
	var sb strings.Builder
	p := NewPrinter(&sb, interner)
	p.printExpr(e)
	return sb.String()
}

// StmtString returns a compact string representation of a statement.
func StmtString(s *Stmt, interner *types.Interner) string {
	var sb strings.Builder
	p := NewPrinter(&sb, interner)
	p.printStmt(s)
	return sb.String()
}
