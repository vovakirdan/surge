package sema

import (
	"fmt"
	"strings"

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
	scope := tc.fileScope()
	paramSpecs := tc.specsFromTypeParams(tc.builder.Items.GetTypeParamIDs(typeItem.TypeParamsStart, typeItem.TypeParamsCount), scope)
	if len(paramSpecs) == 0 && len(typeItem.Generics) > 0 {
		paramSpecs = specsFromNames(typeItem.Generics)
	}
	pushed := tc.pushTypeParams(symID, paramSpecs, nil)
	defer func() {
		if pushed {
			tc.popTypeParams()
		}
	}()
	if paramIDs := tc.builder.Items.GetTypeParamIDs(typeItem.TypeParamsStart, typeItem.TypeParamsCount); len(paramIDs) > 0 {
		bounds := tc.resolveTypeParamBounds(paramIDs, scope, nil)
		tc.attachTypeParamSymbols(symID, bounds)
		tc.applyTypeParamBounds(symID)
	} else if len(paramSpecs) > 0 && len(typeItem.Generics) > 0 {
		// Attach type param symbols for generics syntax (<T>)
		typeParamSyms := make([]symbols.TypeParamSymbol, 0, len(paramSpecs))
		for _, spec := range paramSpecs {
			typeParamSyms = append(typeParamSyms, symbols.TypeParamSymbol{
				Name:      spec.name,
				IsConst:   spec.kind == paramKindConst,
				ConstType: spec.constType,
			})
		}
		tc.attachTypeParamSymbols(symID, typeParamSyms)
	}
	members, hasTag, hasNothing := tc.collectUnionMembers(unionDecl, scope)
	tc.validateUnionMembers(hasTag, hasNothing, typeItem, unionDecl)
	tc.types.SetUnionMembers(typeID, members)
	tc.registerTagConstructors(typeItem, typeID, members)
}

func (tc *typeChecker) instantiateUnion(typeItem *ast.TypeItem, symID symbols.SymbolID, args []types.TypeID) types.TypeID {
	unionDecl := tc.builder.Items.TypeUnion(typeItem)
	if unionDecl == nil {
		return types.NoTypeID
	}
	scope := tc.fileScope()
	paramSpecs := tc.specsFromTypeParams(tc.builder.Items.GetTypeParamIDs(typeItem.TypeParamsStart, typeItem.TypeParamsCount), scope)
	if len(paramSpecs) == 0 && len(typeItem.Generics) > 0 {
		paramSpecs = specsFromNames(typeItem.Generics)
	}
	pushed := tc.pushTypeParams(symID, paramSpecs, args)
	defer func() {
		if pushed {
			tc.popTypeParams()
		}
	}()
	members, hasTag, hasNothing := tc.collectUnionMembers(unionDecl, scope)
	tc.validateUnionMembers(hasTag, hasNothing, typeItem, unionDecl)
	typeID := tc.types.RegisterUnionInstance(typeItem.Name, typeItem.Span, args)
	tc.types.SetUnionMembers(typeID, members)
	tc.registerTagConstructors(typeItem, typeID, members)
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
			if path, ok := tc.builder.Types.Path(member.Type); ok && path != nil && len(path.Segments) == 1 {
				seg := path.Segments[0]
				if tc.lookupTagSymbol(seg.Name, scope).IsValid() {
					tagArgs := make([]types.TypeID, 0, len(seg.Generics))
					for _, arg := range seg.Generics {
						tagArgs = append(tagArgs, tc.resolveTypeExprWithScope(arg, scope))
					}
					members = append(members, types.UnionMember{
						Kind:    types.UnionMemberTag,
						TagName: seg.Name,
						TagArgs: tagArgs,
					})
					hasTag = true
					continue
				}
			}
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
	if tc.lookupTagSymbol(name, tc.fileScope()).IsValid() {
		return true
	}
	tc.report(diag.SemaUnresolvedSymbol, span, "unknown tag %s in union", tc.lookupName(name))
	return false
}

func (tc *typeChecker) registerTagConstructors(typeItem *ast.TypeItem, unionType types.TypeID, members []types.UnionMember) {
	if typeItem == nil || unionType == types.NoTypeID || tc.builder == nil {
		return
	}
	unionName := tc.lookupName(typeItem.Name)
	if unionName == "" {
		return
	}

	scope := tc.fileScope()
	for _, m := range members {
		if m.Kind != types.UnionMemberTag {
			continue
		}
		symID := tc.lookupTagSymbol(m.TagName, scope)
		if !symID.IsValid() {
			continue
		}
		sym := tc.symbolFromID(symID)
		if sym == nil {
			continue
		}
		tagItem, ok := tc.builder.Items.Tag(sym.Decl.Item)
		if !ok || tagItem == nil {
			continue
		}

		params := make([]symbols.TypeKey, 0, len(tagItem.Payload))
		for _, payload := range tagItem.Payload {
			params = append(params, typeKeyForTypeExpr(tc.builder, payload))
		}
		variadic := make([]bool, len(params))

		sym.Type = unionType

		// Determine which type params the tag uses
		// If tag has its own generics (e.g., `tag Success<T>(T)`), use those
		// Otherwise, use the union's generics
		var tagTypeParams []source.StringID
		if len(tagItem.Generics) > 0 {
			tagTypeParams = tagItem.Generics
		} else if len(typeItem.Generics) > 0 {
			tagTypeParams = typeItem.Generics
		}

		if len(sym.TypeParams) == 0 {
			sym.TypeParams = append([]source.StringID(nil), tagTypeParams...)
		}

		// Build result key using TAG name (not union name).
		// Tag constructors return their own tag type, e.g., Success(1) returns Success<int>,
		// and non-generic tags include payload types to form a concrete tag type.
		tagName := tc.lookupName(m.TagName)
		resultKey := tagName
		if len(tagTypeParams) > 0 {
			genNames := make([]string, 0, len(tagTypeParams))
			for _, gid := range tagTypeParams {
				genNames = append(genNames, tc.lookupName(gid))
			}
			resultKey = fmt.Sprintf("%s<%s>", tagName, strings.Join(genNames, ","))
		} else if len(params) > 0 {
			payloadKeys := make([]string, 0, len(params))
			for _, key := range params {
				payloadKeys = append(payloadKeys, string(key))
			}
			resultKey = fmt.Sprintf("%s<%s>", tagName, strings.Join(payloadKeys, ","))
		}

		sym.Signature = &symbols.FunctionSignature{
			Params:     params,
			ParamNames: make([]source.StringID, len(params)),
			Variadic:   variadic,
			Defaults:   make([]bool, len(params)),
			Result:     symbols.TypeKey(resultKey),
			HasBody:    true,
		}
	}
}

func typeKeyForTypeExpr(builder *ast.Builder, typeID ast.TypeID) symbols.TypeKey {
	if builder == nil || !typeID.IsValid() {
		return ""
	}
	expr := builder.Types.Get(typeID)
	if expr == nil {
		return ""
	}
	switch expr.Kind {
	case ast.TypeExprPath:
		if path, ok := builder.Types.Path(typeID); ok && path != nil {
			segments := make([]string, 0, len(path.Segments))
			for _, seg := range path.Segments {
				name := builder.StringsInterner.MustLookup(seg.Name)
				if len(seg.Generics) > 0 {
					gen := make([]string, 0, len(seg.Generics))
					for _, g := range seg.Generics {
						gen = append(gen, string(typeKeyForTypeExpr(builder, g)))
					}
					name = name + "<" + strings.Join(gen, ",") + ">"
				}
				segments = append(segments, name)
			}
			return symbols.TypeKey(strings.Join(segments, "::"))
		}
	case ast.TypeExprUnary:
		if unary, ok := builder.Types.UnaryType(typeID); ok && unary != nil {
			inner := string(typeKeyForTypeExpr(builder, unary.Inner))
			switch unary.Op {
			case ast.TypeUnaryRef:
				return symbols.TypeKey("&" + inner)
			case ast.TypeUnaryRefMut:
				return symbols.TypeKey("&mut " + inner)
			case ast.TypeUnaryOwn:
				return symbols.TypeKey("own " + inner)
			case ast.TypeUnaryPointer:
				return symbols.TypeKey("*" + inner)
			}
		}
	case ast.TypeExprConst:
		if c, ok := builder.Types.Const(typeID); ok && c != nil {
			return symbols.TypeKey(builder.StringsInterner.MustLookup(c.Value))
		}
	case ast.TypeExprArray:
		if arr, ok := builder.Types.Array(typeID); ok && arr != nil {
			return symbols.TypeKey("[" + string(typeKeyForTypeExpr(builder, arr.Elem)) + "]")
		}
	case ast.TypeExprOptional:
		if opt, ok := builder.Types.Optional(typeID); ok && opt != nil {
			return symbols.TypeKey("Option<" + string(typeKeyForTypeExpr(builder, opt.Inner)) + ">")
		}
	case ast.TypeExprErrorable:
		if errable, ok := builder.Types.Errorable(typeID); ok && errable != nil {
			okKey := typeKeyForTypeExpr(builder, errable.Inner)
			errKey := typeKeyForTypeExpr(builder, errable.Error)
			if errKey == "" {
				errKey = "Error"
			}
			return symbols.TypeKey("Result<" + string(okKey) + "," + string(errKey) + ">")
		}
	}
	return symbols.TypeKey(fmt.Sprintf("type#%d", typeID))
}
