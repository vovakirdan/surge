package diagfmt

type PathMode uint8

const (
	PathModeAuto PathMode = iota
	PathModeAbsolute
	PathModeRelative
	PathModeBasename
)

type PrettyOpts struct {
	Color     bool
	Context   int8
	PathMode  PathMode
	Width     uint8 // максимальная ширина строки, 0 - не ограничено
	ShowNotes bool
	ShowFixes bool
}

type JSONOpts struct {
	IncludePositions bool // добавить line/col
	PathMode         PathMode
	Max              int // обрезка вывода, не Bag
	IncludeNotes     bool
	IncludeFixes     bool
}

type SarifRunMeta struct {
	ToolName       string
	ToolVersion    string
	InvocationArgs []string
}
