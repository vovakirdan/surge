package goldencheck

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// Entry is one filesystem object in a golden corpus snapshot.
type Entry struct {
	Path          string `json:"path"`
	Kind          string `json:"kind"`
	Mode          uint32 `json:"mode"`
	ContentSHA256 string `json:"content_sha256"`
}

// Snapshot is a lexical, content-addressed view of a golden tree.
type Snapshot struct {
	Entries []Entry
}

// Change describes one difference between two snapshots.
type Change struct {
	Path   string
	Before *Entry
	After  *Entry
}

// Scan snapshots every filesystem entry below root without following symlinks.
func Scan(root string) (snapshot Snapshot, returnErr error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Snapshot{}, fmt.Errorf("absolute golden root: %w", err)
	}
	rootHandle, err := os.OpenRoot(root)
	if err != nil {
		return Snapshot{}, fmt.Errorf("open golden root: %w", err)
	}
	defer func() {
		if closeErr := rootHandle.Close(); closeErr != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("close golden root: %w", closeErr))
		}
	}()
	var entries []Entry
	err = fs.WalkDir(rootHandle.FS(), ".", func(relativePath string, dirent fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if relativePath == "." {
			return nil
		}
		info, statErr := rootHandle.Lstat(relativePath)
		if statErr != nil {
			return statErr
		}
		entry := Entry{Path: relativePath, Mode: uint32(info.Mode().Perm())}
		var content []byte
		var readErr error
		switch {
		case info.Mode().IsRegular():
			entry.Kind = "file"
			content, readErr = rootHandle.ReadFile(relativePath)
		case info.Mode()&os.ModeSymlink != 0:
			entry.Kind = "symlink"
			var target string
			target, readErr = rootHandle.Readlink(relativePath)
			content = []byte(target)
		case info.IsDir():
			entry.Kind = "directory"
		default:
			return fmt.Errorf("unsupported golden entry %q with mode %s", entry.Path, info.Mode())
		}
		if readErr != nil {
			return fmt.Errorf("read golden entry %q: %w", entry.Path, readErr)
		}
		sum := sha256.Sum256(content)
		entry.ContentSHA256 = hex.EncodeToString(sum[:])
		entries = append(entries, entry)
		return nil
	})
	if err != nil {
		return Snapshot{}, fmt.Errorf("scan golden root: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return Snapshot{Entries: entries}, nil
}

// Digest hashes length-framed entry fields, so path and content boundaries
// cannot collide even when paths contain whitespace or newlines.
func (snapshot Snapshot) Digest() string {
	digest := sha256.New()
	writeField(digest, []byte("surge-golden-snapshot-v1"))
	for _, entry := range snapshot.Entries {
		writeField(digest, []byte(entry.Path))
		writeField(digest, []byte(entry.Kind))
		var mode [4]byte
		binary.BigEndian.PutUint32(mode[:], entry.Mode)
		writeField(digest, mode[:])
		content, err := hex.DecodeString(entry.ContentSHA256)
		if err != nil {
			content = []byte(entry.ContentSHA256)
		}
		writeField(digest, content)
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func writeField(digest hash.Hash, value []byte) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	if _, err := digest.Write(size[:]); err != nil {
		panic(fmt.Sprintf("sha256 length write: %v", err))
	}
	if _, err := digest.Write(value); err != nil {
		panic(fmt.Sprintf("sha256 value write: %v", err))
	}
}

// Diff returns added, removed, content, kind, and mode changes in lexical order.
func Diff(before, after Snapshot) []Change {
	oldEntries := make(map[string]Entry, len(before.Entries))
	newEntries := make(map[string]Entry, len(after.Entries))
	paths := make(map[string]struct{}, len(before.Entries)+len(after.Entries))
	for _, entry := range before.Entries {
		oldEntries[entry.Path] = entry
		paths[entry.Path] = struct{}{}
	}
	for _, entry := range after.Entries {
		newEntries[entry.Path] = entry
		paths[entry.Path] = struct{}{}
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	var changes []Change
	for _, path := range ordered {
		oldEntry, hadOld := oldEntries[path]
		newEntry, hasNew := newEntries[path]
		if hadOld && hasNew && oldEntry == newEntry {
			continue
		}
		change := Change{Path: path}
		if hadOld {
			change.Before = &oldEntry
		}
		if hasNew {
			change.After = &newEntry
		}
		changes = append(changes, change)
	}
	return changes
}
