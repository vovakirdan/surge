package diag

import (
	"fmt"
)

type Code uint16

const (
	// Неизвестная ошибка - на первое время
	UnknownCode Code = 0
	// Лексические
	LexInfo                     Code = 1000
	LexUnknownChar              Code = 1001
	LexUnterminatedString       Code = 1002
	LexUnterminatedBlockComment Code = 1003
	LexBadNumber                Code = 1004

	// Парсерные (зарезервируем)
	SynInfo                    Code = 2000
	SynUnexpectedToken         Code = 2001
	SynUnclosedDelimiter       Code = 2002
	SynUnclosedBlockComment    Code = 2003
	SynUnclosedString          Code = 2004
	SynUnclosedChar            Code = 2005
	SynUnclosedParen           Code = 2006
	SynUnclosedBrace           Code = 2007
	SynUnclosedBracket         Code = 2008
	SynUnclosedSquareBracket   Code = 2009
	SynUnclosedAngleBracket    Code = 2010
	SynUnclosedCurlyBracket    Code = 2011
	SynExpectSemicolon         Code = 2012
	SynForMissingIn            Code = 2013
	SynForBadHeader            Code = 2014
	SynModifierNotAllowed      Code = 2015
	SynAttributeNotAllowed     Code = 2016
	SynAsyncNotAllowed         Code = 2017
	SynTypeExpectEquals        Code = 2018
	SynTypeExpectBody          Code = 2019
	SynTypeExpectUnionMember   Code = 2020
	SynTypeFieldConflict       Code = 2021
	SynTypeDuplicateMember     Code = 2022
	SynTypeNotAllowed          Code = 2023
	SynIllegalItemInExtern     Code = 2024
	SynVisibilityReduction     Code = 2025
	SynFatArrowOutsideParallel Code = 2026
	SynPragmaPosition          Code = 2027

	// import errors & warnings
	SynInfoImportGroup    Code = 2100
	SynUnexpectedTopLevel Code = 2101
	SynExpectIdentifier   Code = 2102
	SynExpectModuleSeg    Code = 2103
	SynExpectItemAfterDbl Code = 2104
	SynExpectIdentAfterAs Code = 2105
	SynEmptyImportGroup   Code = 2106

	// type errors & warnings
	SynInfoTypeExpr       Code = 2200
	SynExpectRightBracket Code = 2201
	SynExpectType         Code = 2202
	SynExpectExpression   Code = 2203
	SynExpectColon        Code = 2204
	SynUnexpectedModifier Code = 2205

	// Семантические (резервируем)
	SemaInfo            Code = 3000
	SemaError           Code = 3001
	SemaDuplicateSymbol Code = 3002
	SemaScopeMismatch   Code = 3003

	// Ошибки I/O
	IOLoadFileError Code = 4001

	// Ошибки проекта / DAG
	ProjInfo              Code = 5000
	ProjDuplicateModule   Code = 5001
	ProjMissingModule     Code = 5002
	ProjSelfImport        Code = 5003
	ProjImportCycle       Code = 5004
	ProjInvalidModulePath Code = 5005
	ProjInvalidImportPath Code = 5006
	ProjDependencyFailed  Code = 5007

	// Observability
	ObsInfo    Code = 6000
	ObsTimings Code = 6001
)

var ( // todo расширить описания и использовать как notes
	codeDescription = map[Code]string{
		UnknownCode:                 "Unknown error",
		LexInfo:                     "Lexical information",
		LexUnknownChar:              "Unknown character",
		LexUnterminatedString:       "Unterminated string",
		LexUnterminatedBlockComment: "Unterminated block comment",
		LexBadNumber:                "Bad number",
		SynInfo:                     "Syntax information",
		SynUnexpectedToken:          "Unexpected token",
		SynUnclosedDelimiter:        "Unclosed delimiter",
		SynUnclosedBlockComment:     "Unclosed block comment",
		SynUnclosedString:           "Unclosed string",
		SynUnclosedChar:             "Unclosed char",
		SynUnclosedParen:            "Unclosed parenthesis",
		SynUnclosedBrace:            "Unclosed brace",
		SynUnclosedBracket:          "Unclosed bracket",
		SynUnclosedSquareBracket:    "Unclosed square bracket",
		SynUnclosedAngleBracket:     "Unclosed angle bracket",
		SynUnclosedCurlyBracket:     "Unclosed curly bracket",
		SynInfoImportGroup:          "Import group information",
		SynUnexpectedTopLevel:       "Unexpected top level",
		SynExpectSemicolon:          "Expect semicolon",
		SynForMissingIn:             "Missing 'in' in for-in loop",
		SynForBadHeader:             "Malformed for-loop header",
		SynModifierNotAllowed:       "Modifier not allowed here",
		SynAttributeNotAllowed:      "Attribute not allowed here",
		SynAsyncNotAllowed:          "'async' not allowed here",
		SynTypeExpectEquals:         "Expected '=' in type declaration",
		SynTypeExpectBody:           "Expected type body",
		SynTypeExpectUnionMember:    "Expected union member",
		SynTypeFieldConflict:        "Duplicate field in type",
		SynTypeDuplicateMember:      "Duplicate union member",
		SynTypeNotAllowed:           "Type declaration is not allowed here",
		SynIllegalItemInExtern:      "Illegal item inside extern block",
		SynVisibilityReduction:      "Visibility reduction is not allowed",
		SynFatArrowOutsideParallel:  "Fat arrow is only allowed in parallel expressions or compare arms",
		SynPragmaPosition:           "Pragma must appear at the top of the file",
		SynExpectIdentifier:         "Expect identifier",
		SynExpectModuleSeg:          "Expect module segment",
		SynExpectItemAfterDbl:       "Expect item after double colon",
		SynExpectIdentAfterAs:       "Expect identifier after as",
		SynEmptyImportGroup:         "Empty import group",
		SynInfoTypeExpr:             "Type expression information",
		SynExpectRightBracket:       "Expect right bracket",
		SynExpectType:               "Expect type",
		SynExpectExpression:         "Expect expression",
		SynExpectColon:              "Expect colon",
		SynUnexpectedModifier:       "Unexpected modifier",
		SemaInfo:                    "Semantic information",
		SemaError:                   "Semantic error",
		SemaDuplicateSymbol:         "Duplicate symbol",
		SemaScopeMismatch:           "Scope stack mismatch",
		IOLoadFileError:             "I/O load file error",
		ProjInfo:                    "Project information",
		ProjDuplicateModule:         "Duplicate module definition",
		ProjMissingModule:           "Missing module",
		ProjSelfImport:              "Module imports itself",
		ProjImportCycle:             "Import cycle detected",
		ProjInvalidModulePath:       "Invalid module path",
		ProjInvalidImportPath:       "Invalid import path",
		ProjDependencyFailed:        "Dependency module has errors",
		ObsInfo:                     "Observability information",
		ObsTimings:                  "Pipeline timings",
	}
)

func (c Code) ID() string {
	switch ic := int(c); {
	case ic >= 1000 && ic < 2000:
		return fmt.Sprintf("LEX%04d", ic)
	case ic >= 2000 && ic < 3000:
		return fmt.Sprintf("SYN%04d", ic)
	case ic >= 3000 && ic < 4000:
		return fmt.Sprintf("SEM%04d", ic)
	case ic >= 4000 && ic < 5000:
		return fmt.Sprintf("IO%04d", ic)
	case ic >= 5000 && ic < 6000:
		return fmt.Sprintf("PRJ%04d", ic)
	case ic >= 6000 && ic < 7000:
		return fmt.Sprintf("OBS%04d", ic)
	}
	return "E0000"
}

func (c Code) Title() string {
	desc, ok := codeDescription[c]
	if !ok {
		return codeDescription[Code(0)]
	}
	return desc
}

func (c Code) String() string {
	return fmt.Sprintf("[%s]: %s", c.ID(), c.Title())
}
