package driver

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/vmihailenco/msgpack/v5"

	"surge/internal/project"
	"surge/internal/source"
)

// Current schema version - increment when DiskPayload format changes
const diskCacheSchemaVersion uint16 = 1

// DiskCache хранит полезные артефакты по ModuleHash на диске.
// Сейчас — только заглушка под будущие экспорты семантики.
// Thread-safe for concurrent access.
type DiskCache struct {
	mu  sync.RWMutex
	dir string
}

// DiskPayload stores cached module metadata and status for fast recompilation.
type DiskPayload struct {
	// Schema version for safe invalidation when format changes
	Schema uint16

	// Module metadata
	Name            string
	Path            string
	Dir             string
	Kind            uint8 // ModuleKind
	NoStd           bool
	HasModulePragma bool

	// Imports (paths only, spans not cached)
	ImportPaths []string

	// Files (paths and hashes)
	FilePaths  []string
	FileHashes []project.Digest

	// Hashes for validation and invalidation
	ContentHash    project.Digest // Hash of module's own content
	ModuleHash     project.Digest // Aggregate hash including dependencies
	DependencyHash project.Digest // Hash of all dependency exports (for invalidation)

	// Status
	Broken bool // Whether module has errors

	// Reserved for future expansion (exports, IR, etc.)
}

// OpenDiskCache initializes and returns a disk cache at the standard location.
func OpenDiskCache(app string) (*DiskCache, error) {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		base = filepath.Join(home, ".cache")
	}
	dir := filepath.Join(base, app)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &DiskCache{dir: dir}, nil
}

func (c *DiskCache) pathFor(key project.Digest) string {
	hexKey := hex.EncodeToString(key[:])
	// Для удобства читаемости/очистки — подкаталог "mods".
	return filepath.Join(c.dir, "mods", hexKey+".mp")
}

// Put serializes and writes a payload to the disk cache.
func (c *DiskCache) Put(key project.Digest, payload *DiskPayload) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	p := c.pathFor(key)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), "tmp-*")
	if err != nil {
		return err
	}
	defer func() {
		if err = os.Remove(f.Name()); err != nil {
			fmt.Printf("failed to remove temp file: %v", err)
		}
	}()

	enc := msgpack.NewEncoder(f)
	err = enc.Encode(payload)
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// Атомарная замена
	return os.Rename(f.Name(), p)
}

// Get reads and deserializes a payload from the disk cache.
func (c *DiskCache) Get(key project.Digest, out *DiskPayload) (bool, error) {
	if c == nil {
		return false, nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()

	p := c.pathFor(key)
	f, err := os.Open(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			panic(closeErr)
		}
	}()
	dec := msgpack.NewDecoder(f)
	if err := dec.Decode(out); err != nil {
		return false, err
	}
	return true, nil
}

// DropAll invalidates the cache, useful after format changes.
func (c *DiskCache) DropAll() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	// тривиально: переименуем каталог и удалим в фоне
	old := c.dir + ".old-" + time.Now().Format("20060102150405")
	if err := os.Rename(c.dir, old); err != nil {
		return err
	}
	return os.RemoveAll(old)
}

// IsSHA256 performs a basic sanity check that the digest is a non-zero SHA-256 value.
func IsSHA256(d project.Digest) bool {
	var z project.Digest
	if d == z {
		return false
	}
	// не строгое доказательство, но хотя бы исключим нули
	_ = sha256.BlockSize // "фиксируем" мотивацию через импорт
	return true
}

// moduleToDiskPayload converts ModuleMeta to DiskPayload for caching
func moduleToDiskPayload(meta *project.ModuleMeta, broken bool, depHash project.Digest) *DiskPayload {
	if meta == nil {
		return nil
	}

	payload := &DiskPayload{
		Schema:          diskCacheSchemaVersion,
		Name:            meta.Name,
		Path:            meta.Path,
		Dir:             meta.Dir,
		Kind:            uint8(meta.Kind),
		NoStd:           meta.NoStd,
		HasModulePragma: meta.HasModulePragma,
		ContentHash:     meta.ContentHash,
		ModuleHash:      meta.ModuleHash,
		DependencyHash:  depHash,
		Broken:          broken,
	}

	// Extract import paths
	payload.ImportPaths = make([]string, len(meta.Imports))
	for i, imp := range meta.Imports {
		payload.ImportPaths[i] = imp.Path
	}

	// Extract file paths and hashes
	payload.FilePaths = make([]string, len(meta.Files))
	payload.FileHashes = make([]project.Digest, len(meta.Files))
	for i, f := range meta.Files {
		payload.FilePaths[i] = f.Path
		payload.FileHashes[i] = f.Hash
	}

	return payload
}

// diskPayloadToModule converts DiskPayload back to ModuleMeta (without spans)
func diskPayloadToModule(payload *DiskPayload) *project.ModuleMeta {
	if payload == nil || payload.Schema != diskCacheSchemaVersion {
		return nil
	}

	meta := &project.ModuleMeta{
		Name:            payload.Name,
		Path:            payload.Path,
		Dir:             payload.Dir,
		Kind:            project.ModuleKind(payload.Kind),
		NoStd:           payload.NoStd,
		HasModulePragma: payload.HasModulePragma,
		ContentHash:     payload.ContentHash,
		ModuleHash:      payload.ModuleHash,
	}

	// Restore imports (without spans - use zero spans)
	meta.Imports = make([]project.ImportMeta, len(payload.ImportPaths))
	for i, path := range payload.ImportPaths {
		meta.Imports[i] = project.ImportMeta{
			Path: path,
			Span: source.Span{}, // Zero span - not cached
		}
	}

	// Restore files (without spans)
	meta.Files = make([]project.ModuleFileMeta, len(payload.FilePaths))
	for i := range payload.FilePaths {
		meta.Files[i] = project.ModuleFileMeta{
			Path: payload.FilePaths[i],
			Hash: payload.FileHashes[i],
			Span: source.Span{}, // Zero span - not cached
		}
	}

	return meta
}
