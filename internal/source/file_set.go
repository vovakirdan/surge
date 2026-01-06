package source

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"

	"fortio.org/safecast"
)

// FileSet manages a collection of source files and provides global byte offset resolution.
type FileSet struct {
	files   []File
	index   map[string]FileID // path -> id
	baseDir string            // базовая директория для относительных путей
}

// NewFileSet creates a new empty FileSet.
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
func (fileSet *FileSet) SetBaseDir(dir string) {
	fileSet.baseDir = dir
}

// BaseDir возвращает текущую базовую директорию.
func (fileSet *FileSet) BaseDir() string {
	if fileSet.baseDir == "" {
		// Если не установлена, используем текущую рабочую директорию
		if wd, err := os.Getwd(); err == nil {
			return wd
		}
	}
	return fileSet.baseDir
}

// Add stores a file from normalized bytes, computes LineIdx and Hash, and returns a new FileID.
// It always creates a new FileID even if a file with the same path already exists.
func (fileSet *FileSet) Add(path string, content []byte, flags FileFlags) FileID {
	hash := sha256.Sum256(content)
	lineIdx := buildLineIndex(content)
	normalizedPath := normalizePath(path)

	lenFiles, err := safecast.Conv[uint32](len(fileSet.files))
	if err != nil {
		panic(fmt.Errorf("len files overflow: %w", err))
	}
	id := FileID(lenFiles)
	fileSet.files = append(fileSet.files, File{
		ID:      id,
		Path:    normalizedPath,
		Content: content,
		LineIdx: lineIdx,
		Hash:    hash,
		Flags:   flags,
	})
	// Всегда обновляем индекс на последнюю версию файла
	fileSet.index[normalizedPath] = id
	return id
}

// Load reads a file from disk, normalizes CRLF/BOM, and calls Add.
func (fileSet *FileSet) Load(path string) (FileID, error) {
	// #nosec G304 -- path is provided by the caller
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	content, hadBOM := removeBOM(content)
	content, hadCRLF := normalizeCRLF(content)

	flags := FileFlags(0)
	if hadBOM {
		flags |= FileHadBOM
	}
	if hadCRLF {
		flags |= FileNormalizedCRLF
	}
	return fileSet.Add(path, content, flags), nil
}

// AddVirtual adds a virtual file (stdin, test, or generated) with the FileVirtual flag.
func (fileSet *FileSet) AddVirtual(name string, content []byte) FileID {
	return fileSet.Add(name, content, FileVirtual)
}

// Get returns the file metadata for the given ID.
func (fileSet *FileSet) Get(id FileID) *File {
	// TODO: optional bounds check in debug builds
	return &fileSet.files[id]
}

// GetLatest returns the latest file ID for the given path, if it exists.
func (fileSet *FileSet) GetLatest(path string) (FileID, bool) {
	id, ok := fileSet.index[normalizePath(path)]
	return id, ok
}

// GetByPath возвращает *File по пути, если был загружен в этот FileSet.
func (fileSet *FileSet) GetByPath(path string) (*File, bool) {
	if id, ok := fileSet.index[normalizePath(path)]; ok {
		return &fileSet.files[id], true
	}
	return nil, false
}

// Resolve converts a span into line and column positions.
func (fileSet *FileSet) Resolve(span Span) (start, end LineCol) {
	f := fileSet.files[span.File]
	return toLineCol(f.LineIdx, span.Start), toLineCol(f.LineIdx, span.End)
}

// GetLine возвращает строку с заданным номером (1-based) из файла.
// Если строка не существует, возвращает пустую строку.
func (f *File) GetLine(lineNum uint32) string {
	if lineNum == 0 {
		return ""
	}

	// Определяем начало и конец строки
	var start, end, lenLineIdx, lenContent uint32
	var err error
	lenLineIdx, err = safecast.Conv[uint32](len(f.LineIdx))
	if err != nil {
		panic(fmt.Errorf("line index length overflow: %w", err))
	}
	lenContent, err = safecast.Conv[uint32](len(f.Content))
	if err != nil {
		panic(fmt.Errorf("content length overflow: %w", err))
	}

	switch {
	case lineNum == 1:
		start = 0
	case (lineNum - 2) < lenLineIdx:
		start = f.LineIdx[lineNum-2] + 1
	default:
		return ""
	}

	if (lineNum - 1) < lenLineIdx {
		end = f.LineIdx[lineNum-1]
	} else {
		end = lenContent
	}

	if start >= lenContent {
		return ""
	}
	if end > lenContent {
		end = lenContent
	}

	return string(f.Content[start:end])
}

// FormatPath форматирует путь к файлу в зависимости от режима.
// mode: "absolute", "relative", "basename", "auto"
// baseDir: базовая директория для относительных путей (игнорируется для других режимов)
func (f *File) FormatPath(mode, baseDir string) string {
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
