package source

import (
	"crypto/sha256"
	"os"
	"path/filepath"
)

type FileSet struct {
	files   []File
	index   map[string]FileID // path -> id
	baseDir string            // базовая директория для относительных путей
}

func NewFileSet() *FileSet {
	return &FileSet{
		files:   make([]File, 0),
		index:   make(map[string]FileID),
		baseDir: "", // будет установлен при первом Load() или явно
	}
}

// NewFileSetWithBase создаёт FileSet с заданной базовой директорией.
func NewFileSetWithBase(baseDir string) *FileSet {
	return &FileSet{
		files:   make([]File, 0),
		index:   make(map[string]FileID),
		baseDir: baseDir,
	}
}

// SetBaseDir устанавливает базовую директорию для относительных путей.
func (fs *FileSet) SetBaseDir(dir string) {
	fs.baseDir = dir
}

// BaseDir возвращает текущую базовую директорию.
func (fs *FileSet) BaseDir() string {
	if fs.baseDir == "" {
		// Если не установлена, используем текущую рабочую директорию
		if wd, err := os.Getwd(); err == nil {
			return wd
		}
	}
	return fs.baseDir
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

// GetLine возвращает строку с заданным номером (1-based) из файла.
// Если строка не существует, возвращает пустую строку.
func (f *File) GetLine(lineNum uint32) string {
	if lineNum == 0 {
		return ""
	}

	// Определяем начало и конец строки
	var start, end uint32

	if lineNum == 1 {
		start = 0
	} else if int(lineNum-2) < len(f.LineIdx) {
		start = f.LineIdx[lineNum-2] + 1
	} else {
		return ""
	}

	if int(lineNum-1) < len(f.LineIdx) {
		end = f.LineIdx[lineNum-1]
	} else {
		end = uint32(len(f.Content))
	}

	if start >= uint32(len(f.Content)) {
		return ""
	}
	if end > uint32(len(f.Content)) {
		end = uint32(len(f.Content))
	}

	return string(f.Content[start:end])
}

// FormatPath форматирует путь к файлу в зависимости от режима.
// mode: "absolute", "relative", "basename", "auto"
// baseDir: базовая директория для относительных путей (игнорируется для других режимов)
func (f *File) FormatPath(mode string, baseDir string) string {
	switch mode {
	case "absolute":
		if abs, err := AbsolutePath(f.Path); err == nil {
			return abs
		}
		return f.Path

	case "relative":
		if baseDir == "" {
			// Если базовая директория не указана, используем текущую
			if wd, err := os.Getwd(); err == nil {
				baseDir = wd
			}
		}
		if rel, err := RelativePath(f.Path, baseDir); err == nil {
			return rel
		}
		return f.Path

	case "basename":
		return BaseName(f.Path)

	case "auto":
		// Auto: если путь короткий или относительный - как есть, иначе basename
		if len(f.Path) < 40 || !filepath.IsAbs(f.Path) {
			return f.Path
		}
		return BaseName(f.Path)

	default:
		return f.Path
	}
}
