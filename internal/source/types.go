package source

type (
	FileID    uint32 // просто ID источника
	FileFlags uint8  // метаданные
)

const (
	FileVirtual FileFlags = 1 << iota // добавлен не с диска (тест, stdin)
	FileHadBOM
	FileNormalizedCRLF
)

type File struct {
	ID      FileID
	Path    string
	Content []byte
	LineIdx []uint32
	Hash    [32]byte
	Flags   FileFlags
}

type LineCol struct {
	Line uint32 // 1-based
	Col  uint32 // 1-based
}
