package driver

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/vmihailenco/msgpack/v5"

	"surge/internal/project"
)

// DiskCache хранит полезные артефакты по ModuleHash на диске.
// Сейчас — только заглушка под будущие экспорты семантики.
type DiskCache struct {
	dir string
}

// DiskPayload — пример полезной нагрузки, которую позже заменим на экспорты/IR.
type DiskPayload struct {
	// Версия схемы, чтобы безопасно инвалидировать.
	Schema uint16
	// Метаданные (минимум) — удобно для отладки.
	Path       string
	ModuleHash project.Digest
	// Зарезервировано под "Exports"/"SemaSummary"/и т.п.
}

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

func (c *DiskCache) Put(key project.Digest, payload *DiskPayload) error {
	if c == nil {
		return nil
	}
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

func (c *DiskCache) Get(key project.Digest, out *DiskPayload) (bool, error) {
	if c == nil {
		return false, nil
	}
	p := c.pathFor(key)
	f, err := os.Open(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	defer f.Close()
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
