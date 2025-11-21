package token

var keywords = map[string]Kind{
	"fn":       KwFn,
	"let":      KwLet,
	"const":    KwConst,
	"mut":      KwMut,
	"own":      KwOwn,
	"if":       KwIf,
	"else":     KwElse,
	"while":    KwWhile,
	"for":      KwFor,
	"in":       KwIn,
	"break":    KwBreak,
	"continue": KwContinue,
	"return":   KwReturn,
	"import":   KwImport,
	"as":       KwAs,
	"type":     KwType,
	"contract": KwContract,
	"tag":      KwTag,
	"extern":   KwExtern,
	"pub":      KwPub,
	"async":    KwAsync,
	"await":    KwAwait,
	"compare":  KwCompare,
	"finally":  KwFinally,
	"channel":  KwChannel,
	"spawn":    KwSpawn,
	"true":     KwTrue,
	"false":    KwFalse,
	"signal":   KwSignal,
	"parallel": KwParallel,
	"map":      KwMap,
	"reduce":   KwReduce,
	"with":     KwWith,
	"macro":    KwMacro,
	"pragma":   KwPragma,
	"to":       KwTo,
	"heir":     KwHeir,
	"is":       KwIs,
	"field":    KwField,
	"nothing":  NothingLit,
}

// LookupKeyword возвращает тип и bool если это ключевое слово.
// Ключевые слова регистрозависимые — только lowercase версии распознаются.
func LookupKeyword(ident string) (Kind, bool) {
	k, ok := keywords[ident]
	return k, ok
}
