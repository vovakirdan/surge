package source

import (
	"crypto/sha256"
	"os"
)

type FileSet struct {
	files []File
	index map[string]FileID // path -> id
}

func NewFileSet() *FileSet {
	return &FileSet{
		files: make([]File, 0),
		index: make(map[string]FileID),
	}
}

// Добавляет файл из готовых байт (уже нормализованных), считает LineIdx и Hash, возвращает новый FileID.
// Всегда создает новый FileID, даже если файл с таким путем уже существует.
func (fs *FileSet) Add(path string, content []byte, flags FileFlags) FileID {
	hash := sha256.Sum256(content)
	lineIdx := buildLineIndex(content)
	normalizedPath := normalizePath(path)

	id := FileID(len(fs.files))
	fs.files = append(fs.files, File{
		ID:      id,
		Path:    normalizedPath,
		Content: content,
		LineIdx: lineIdx,
		Hash:    hash,
		Flags:   flags,
	})
	// Всегда обновляем индекс на последнюю версию файла
	fs.index[normalizedPath] = id
	return id
}

// Читает файл с диска (делает нормализацию CRLF→LF, удаляет BOM), вызывает Add.
func (fs *FileSet) Load(path string) (FileID, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	content, had_BOM := removeBOM(content)
	content, had_CRLF := normalizeCRLF(content)

	flags := FileFlags(0)
	if had_BOM {
		flags |= FileHadBOM
	}
	if had_CRLF {
		flags |= FileNormalizedCRLF
	}
	return fs.Add(path, content, flags), nil
}

// Добавляет "виртуальный" файл (stdin, тест, автогенерированный), флаг FileVirtual.
func (fs *FileSet) AddVirtual(name string, content []byte) FileID {
	return fs.Add(name, content, FileVirtual)
}

func (fs *FileSet) Get(id FileID) *File {
    // TODO: optional bounds check in debug builds
	return &fs.files[id]
}

// Возвращает последнюю версию файла по пути, если он существует
func (fs *FileSet) GetLatest(path string) (FileID, bool) {
	id, ok := fs.index[normalizePath(path)]
	return id, ok
}

func (fs *FileSet) Resolve(span Span) (start LineCol, end LineCol) {
	f := fs.files[span.File]
	return toLineCol(f.LineIdx, span.Start), toLineCol(f.LineIdx, span.End)
}
