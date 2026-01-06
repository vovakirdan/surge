package fuzztests

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	maxSeedBytes = 64 << 10 // 64 KiB — ограничение для тестового корпуса
)

func addCorpusSeeds(f *testing.F) {
	addTestdataSeeds(f)
	addLanguageSeeds(f)
}

func addTestdataSeeds(f *testing.F) {
	root := filepath.Join("..", "..", "testdata")
	if _, err := os.Stat(root); err != nil {
		return
	}
	// проходим по дереву testdata, добавляем все *.sg файлы
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".sg" {
			return nil
		}
		// #nosec G304 -- path comes from repository testdata walk
		src, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		f.Add(clampSeed(src))
		return nil
	})
	if err != nil {
		return
	}
	// добавляем хотя бы один минимальный пример на случай пустого testdata
	f.Add([]byte{})
	f.Add([]byte("fn main() -> int { return 0; }\n"))
}

func addLanguageSeeds(f *testing.F) {
	specPath := filepath.Join("..", "..", "LANGUAGE.md")
	// #nosec G304 -- path is a fixed repository location
	data, err := os.ReadFile(specPath)
	if err != nil {
		return
	}
	lines := bytes.Split(data, []byte{'\n'})
	var block [][]byte
	inSurgeBlock := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(string(line))
		if strings.HasPrefix(trimmed, "```surge") {
			inSurgeBlock = true
			block = block[:0]
			continue
		}
		if strings.HasPrefix(trimmed, "```") {
			if inSurgeBlock {
				snippet := clampSeed(bytes.Join(block, []byte{'\n'}))
				if len(snippet) > 0 {
					f.Add(snippet)
				}
			}
			inSurgeBlock = false
			block = block[:0]
			continue
		}
		if inSurgeBlock {
			// сохраняем оригинальные строки, включая отступы
			block = append(block, line)
		}
	}
}

func clampSeed(src []byte) []byte {
	if len(src) <= maxSeedBytes {
		return append([]byte(nil), src...)
	}
	return append([]byte(nil), src[:maxSeedBytes]...)
}
