package sema

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) populateUnionType(itemID ast.ItemID, typeItem *ast.TypeItem, typeID types.TypeID) {
	unionDecl := tc.builder.Items.TypeUnion(typeItem)
	if unionDecl == nil {
		return
	}
	symID := tc.typeSymbolForItem(itemID)
	pushed := tc.pushTypeParams(symID, typeItem.Generics, nil)
	defer func() {
		if pushed {
			tc.popTypeParams()
		}
	}()
	scope := tc.fileScope()
	members, hasTag, hasNothing := tc.collectUnionMembers(unionDecl, scope)
	tc.validateUnionMembers(hasTag, hasNothing, typeItem, unionDecl)
	tc.types.SetUnionMembers(typeID, members)
}

func (tc *typeChecker) instantiateUnion(typeItem *ast.TypeItem, symID symbols.SymbolID, args []types.TypeID) types.TypeID {
	unionDecl := tc.builder.Items.TypeUnion(typeItem)
	if unionDecl == nil {
		return types.NoTypeID
	}
	pushed := tc.pushTypeParams(symID, typeItem.Generics, args)
	defer func() {
		if pushed {
			tc.popTypeParams()
		}
	}()
	scope := tc.fileScope()
	members, hasTag, hasNothing := tc.collectUnionMembers(unionDecl, scope)
	tc.validateUnionMembers(hasTag, hasNothing, typeItem, unionDecl)
	typeID := tc.types.RegisterUnionInstance(typeItem.Name, typeItem.Span, args)
	tc.types.SetUnionMembers(typeID, members)
	return typeID
}

func (tc *typeChecker) collectUnionMembers(unionDecl *ast.TypeUnionDecl, scope symbols.ScopeID) ([]types.UnionMember, bool, bool) {
	members := make([]types.UnionMember, 0, unionDecl.MembersCount)
	hasTag := false
	hasNothing := false
	if unionDecl.MembersCount == 0 {
		return members, hasTag, hasNothing
	}
	start := uint32(unionDecl.MembersStart)
	count := int(unionDecl.MembersCount)
	for offset := range count {
		uoff, err := safecast.Conv[uint32](offset)
		if err != nil {
			panic(fmt.Errorf("union member offset overflow: %w", err))
		}
		memberID := ast.TypeUnionMemberID(start + uoff)
		member := tc.builder.Items.UnionMember(memberID)
		if member == nil {
			continue
		}
		switch member.Kind {
		case ast.TypeUnionMemberType:
			typ := tc.resolveTypeExprWithScope(member.Type, scope)
			members = append(members, types.UnionMember{
				Kind: types.UnionMemberType,
				Type: typ,
			})
		case ast.TypeUnionMemberNothing:
			hasNothing = true
			members = append(members, types.UnionMember{
				Kind: types.UnionMemberNothing,
				Type: tc.types.Builtins().Nothing,
			})
		case ast.TypeUnionMemberTag:
			hasTag = true
			if !tc.tagSymbolExists(member.TagName, member.Span) {
				continue
			}
			tagArgs := make([]types.TypeID, 0, len(member.TagArgs))
			for _, arg := range member.TagArgs {
				tagArgs = append(tagArgs, tc.resolveTypeExprWithScope(arg, scope))
			}
			members = append(members, types.UnionMember{
				Kind:    types.UnionMemberTag,
				TagName: member.TagName,
				TagArgs: tagArgs,
			})
		}
	}
	return members, hasTag, hasNothing
}

func (tc *typeChecker) validateUnionMembers(hasTag, hasNothing bool, typeItem *ast.TypeItem, unionDecl *ast.TypeUnionDecl) {
	if hasTag || hasNothing || typeItem == nil {
		return
	}
	typeName := tc.lookupName(typeItem.Name)
	span := typeItem.Span
	if unionDecl != nil && unionDecl.BodySpan != (source.Span{}) {
		span = unionDecl.BodySpan
	}
	if typeName == "" {
		typeName = "_"
	}
	tc.report(diag.SemaTypeMismatch, span, "%s: pure union of value types is not allowed; use tagged variants instead", typeName)
}

func (tc *typeChecker) tagSymbolExists(name source.StringID, span source.Span) bool {
	if name == source.NoStringID || tc.symbols == nil || tc.symbols.Table == nil || tc.symbols.Table.Scopes == nil || tc.symbols.Table.Symbols == nil {
		return false
	}
	scope := tc.fileScope()
	for scope.IsValid() {
		data := tc.symbols.Table.Scopes.Get(scope)
		if data == nil {
			break
		}
		if ids := data.NameIndex[name]; len(ids) > 0 {
			for i := len(ids) - 1; i >= 0; i-- {
				id := ids[i]
				sym := tc.symbols.Table.Symbols.Get(id)
				if sym == nil {
					continue
				}
				if sym.Kind == symbols.SymbolTag {
					return true
				}
			}
		}
		scope = data.Parent
	}
	tc.report(diag.SemaUnresolvedSymbol, span, "unknown tag %s in union", tc.lookupName(name))
	return false
}
