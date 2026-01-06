package source

type (
	// FileID uniquely identifies a source file within a FileSet.
	FileID uint32 // просто ID источника
	// FileFlags encodes metadata about a source file.
	FileFlags uint8 // метаданные
)

const (
	// FileVirtual indicates the file was added from memory (test, stdin, etc.).
	FileVirtual FileFlags = 1 << iota // добавлен не с диска (тест, stdin)
	FileHadBOM
	FileNormalizedCRLF
)

// File captures metadata and content for a single source file.
type File struct {
	ID      FileID
	Path    string
	Content []byte
	LineIdx []uint32
	Hash    [32]byte
	Flags   FileFlags
}

// LineCol represents a human-readable position in a source file.
type LineCol struct {
	Line uint32 // 1-based
	Col  uint32 // 1-based
}
