package diagfmt

type PathMode uint8

const (
	PathModeAuto PathMode = iota
	PathModeAbsolute
	PathModeRelative
	PathModeBasename
)

type PrettyOpts struct {
	Color bool
	Context int8
	PathMode PathMode
	Width uint8 // максимальная ширина строки, 0 - не ограничено
}

type JSONOpts struct {
	// todo
}

type SarifRunMeta struct {
	ToolName string
	ToolVersion string
	InvocationArgs []string
}
