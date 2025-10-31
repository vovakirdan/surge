package project

import (
	"crypto/sha256"
)

// Digest - фиксированный 256 битный хеш (совместим с source.File.Hash)
type Digest [32]byte

// Combine строит модульный хеш: H( content || dep1 || dep2 ... ).
// Порядок deps должен быть детерминированным (у нас Edges уже отсортированы).
func Combine(content Digest, deps ...Digest) Digest {
	h := sha256.New()
	_, _ = h.Write(content[:])
	for _, d := range deps {
		_, _ = h.Write(d[:])
	}
	var out Digest
	copy(out[:], h.Sum(nil))
	return out
}
