package diag

import (
	"fmt"
)

type Code uint16

const (
	// Неизвестная ошибка - на первое время
	UnknownCode Code = 0
	// Лексические
	LexUnknownChar              Code = 1001
	LexUnterminatedString       Code = 1002
	LexUnterminatedBlockComment Code = 1003
	LexBadNumber                Code = 1004

	// Парсерные (зарезервируем)
	SynUnexpectedToken       Code = 2001
	SynUnclosedDelimiter     Code = 2002
	SynUnclosedBlockComment  Code = 2003
	SynUnclosedString        Code = 2004
	SynUnclosedChar          Code = 2005
	SynUnclosedParen         Code = 2006
	SynUnclosedBrace         Code = 2007
	SynUnclosedBracket       Code = 2008
	SynUnclosedSquareBracket Code = 2009
	SynUnclosedAngleBracket  Code = 2010
	SynUnclosedCurlyBracket  Code = 2011
)

var (
	codeDescription = map[Code]string{
		UnknownCode:                 "Unknown error",
		LexUnknownChar:              "Unknown character",
		LexUnterminatedString:       "Unterminated string",
		LexUnterminatedBlockComment: "Unterminated block comment",
		LexBadNumber:                "Bad number",
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
	}
)

func (c Code) ID() string {
	switch ic := int(c); {
	case ic >= 1000 && ic < 2000:
		return fmt.Sprintf("LEX%04d", ic)
	case ic >= 2000 && ic < 3000:
		return fmt.Sprintf("SYN%04d", ic)
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
