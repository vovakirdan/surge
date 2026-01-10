package token

// Kind represents the category of a source token.
type Kind uint8

const (
	// Invalid indicates an erroneous token.
	Invalid Kind = iota
	// EOF marks the end of the source input.
	EOF

	// Ident represents an identifier token.
	Ident
	// KwFn represents the 'fn' keyword.
	KwFn // fn
	// KwLet represents the 'let' keyword.
	KwLet // let
	// KwConst represents the 'const' keyword.
	KwConst // const
	// KwMut represents the 'mut' keyword.
	KwMut // mut
	// KwOwn represents the 'own' keyword.
	KwOwn // own
	// KwIf represents the 'if' keyword.
	KwIf // if
	// KwElse represents the 'else' keyword.
	KwElse // else
	// KwWhile represents the 'while' keyword.
	KwWhile // while
	// KwFor represents the 'for' keyword.
	KwFor // for
	// KwIn represents the 'in' keyword.
	KwIn // in
	// KwBreak represents the 'break' keyword.
	KwBreak // break
	// KwContinue represents the 'continue' keyword.
	KwContinue // continue
	// KwReturn represents the 'return' keyword.
	KwReturn // return
	// KwImport represents the 'import' keyword.
	KwImport // import
	// KwAs represents the 'as' keyword.
	KwAs // as
	// KwType represents the 'type' keyword.
	KwType // type
	// KwContract represents the 'contract' keyword.
	KwContract // contract
	// KwTag represents the 'tag' keyword.
	KwTag // tag
	// KwExtern represents the 'extern' keyword.
	KwExtern // extern
	// KwPub represents the 'pub' keyword.
	KwPub // pub
	// KwAsync represents the 'async' keyword.
	KwAsync // async
	// KwBlocking represents the 'blocking' keyword.
	KwBlocking // blocking
	// KwCompare represents the 'compare' keyword.
	KwCompare // compare
	// KwSelect represents the 'select' keyword.
	KwSelect // select
	// KwRace represents the 'race' keyword.
	KwRace // race
	// KwFinally represents the 'finally' keyword.
	KwFinally // finally
	// KwChannel represents the 'channel' keyword.
	KwChannel // channel
	// KwTask represents the legacy 'task' token (not produced by the lexer).
	KwTask // task
	// KwSpawn represents the 'spawn' keyword.
	KwSpawn // spawn
	// KwTrue represents the 'true' keyword.
	KwTrue // true
	// KwFalse represents the 'false' keyword.
	KwFalse // false
	// KwSignal represents the 'signal' keyword.
	KwSignal // signal
	// KwParallel represents the 'parallel' keyword.
	KwParallel // parallel
	// KwMap represents the 'map' keyword.
	KwMap // map
	// KwReduce represents the 'reduce' keyword.
	KwReduce // reduce
	// KwWith represents the 'with' keyword.
	KwWith // with
	// KwMacro represents the 'macro' keyword.
	KwMacro // macro
	// KwPragma represents the 'pragma' keyword.
	KwPragma // pragma
	// KwTo represents the 'to' keyword.
	KwTo // to
	// KwHeir represents the 'heir' keyword.
	KwHeir // heir
	// KwIs represents the 'is' keyword.
	KwIs // is
	// KwField represents the 'field' keyword.
	KwField // field
	// KwEnum represents the 'enum' keyword.
	KwEnum // enum

	// NothingLit represents the nothing literal token.
	NothingLit
	// IntLit represents the integer literal token.
	IntLit
	// UintLit represents the unsigned integer literal token.
	UintLit
	// FloatLit represents the float literal token.
	FloatLit
	// BoolLit represents the boolean literal token.
	BoolLit
	// StringLit represents the string literal token.
	StringLit
	// FStringLit represents the formatted string literal token.
	FStringLit

	// Plus represents the plus operator token.
	Plus // +
	// Minus represents the minus operator token.
	Minus // -
	// Star represents the star operator token.
	Star // *
	// Slash represents the slash operator token.
	Slash // /
	// Percent represents the percent operator token.
	Percent // %
	// Assign represents the assign operator token.
	Assign // =
	// PlusAssign represents the plus assign operator token.
	PlusAssign // +=
	// MinusAssign represents the minus assign operator token.
	MinusAssign // -=
	// StarAssign represents the star assign operator token.
	StarAssign // *=
	// SlashAssign represents the slash assign operator token.
	SlashAssign // /=
	// PercentAssign represents the percent assign operator token.
	PercentAssign // %=
	// AmpAssign represents the amp assign operator token.
	AmpAssign // &=
	// PipeAssign represents the pipe assign operator token.
	PipeAssign // |=
	// CaretAssign represents the caret assign operator token.
	CaretAssign // ^=
	// ShlAssign represents the shl assign operator token.
	ShlAssign // <<=
	// ShrAssign represents the shr assign operator token.
	ShrAssign // >>=
	// EqEq represents the eq eq operator token.
	EqEq // ==
	// Bang represents the bang operator token.
	Bang // !
	// BangEq represents the bang eq operator token.
	BangEq // !=
	// Lt represents the lt operator token.
	Lt // <
	// LtEq represents the lt eq operator token.
	LtEq // <=
	// Gt represents the gt operator token.
	Gt // >
	// GtEq represents the gt eq operator token.
	GtEq // >=
	// Shl represents the shl operator token.
	Shl // <<
	// Shr represents the shr operator token.
	Shr // >>
	// Amp represents the amp operator token.
	Amp // &
	// Pipe represents the pipe operator token.
	Pipe // |
	// Caret represents the caret operator token.
	Caret // ^
	// AndAnd represents the and and operator token.
	AndAnd // &&
	// OrOr represents the or or operator token.
	OrOr // ||
	// Question represents the question operator token.
	Question // ?
	// QuestionQuestion represents the question question operator token.
	QuestionQuestion // ??
	// Colon represents the colon operator token.
	Colon // :
	// ColonColon represents the colon colon operator token.
	ColonColon // ::
	// ColonAssign represents the colon assign operator token.
	ColonAssign // :=
	// Semicolon represents the semicolon operator token.
	Semicolon // ;
	// Comma represents the comma operator token.
	Comma // ,
	// Dot represents the dot operator token.
	Dot // .
	// DotDot represents the dot dot operator token.
	DotDot // ..
	// Arrow represents the arrow operator token.
	Arrow // ->
	// FatArrow represents the fat arrow operator token.
	FatArrow // =>
	// LParen represents the left parenthesis operator token.
	LParen // (
	// RParen represents the right parenthesis operator token.
	RParen // )
	// LBrace represents the left brace operator token.
	LBrace // {
	// RBrace represents the right brace operator token.
	RBrace // }
	// LBracket represents the left bracket operator token.
	LBracket // [
	// RBracket represents the right bracket operator token.
	RBracket // ]
	// At represents the at operator token.
	At // @
	// Underscore represents the underscore operator token.
	Underscore // _
	// DotDotEq represents the dot dot eq operator token.
	DotDotEq // ..=
	// DotDotDot represents the dot dot dot operator token.
	DotDotDot // ... (vararg)
)
