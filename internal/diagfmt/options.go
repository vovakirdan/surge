package diagfmt

// PathMode specifies how file paths are displayed.
type PathMode uint8

const (
	// PathModeAuto chooses relative or absolute path automatically.
	PathModeAuto PathMode = iota
	// PathModeAbsolute always uses absolute paths.
	PathModeAbsolute
	PathModeRelative
	PathModeBasename
)

// PrettyOpts configures pretty-printing of diagnostics.
type PrettyOpts struct {
	Color       bool
	Context     int8
	PathMode    PathMode
	Width       uint8 // максимальная ширина строки, 0 - не ограничено
	ShowNotes   bool
	ShowFixes   bool
	ShowPreview bool
}

// JSONOpts configures JSON output of diagnostics.
type JSONOpts struct {
	IncludePositions bool // добавить line/col
	PathMode         PathMode
	Max              int // обрезка вывода, не Bag
	IncludeNotes     bool
	IncludeFixes     bool
	IncludePreviews  bool
	IncludeSemantics bool
}

// SarifRunMeta provides metadata for SARIF output.
type SarifRunMeta struct {
	ToolName       string
	ToolVersion    string
	InvocationArgs []string
}
