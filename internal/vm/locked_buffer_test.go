package vm_test

import (
	"bytes"
	"sync"
)

// lockedBuffer is an io.Writer a test can read from while the child process is
// still writing to it. exec fills a command's Stdout/Stderr from its own
// goroutine, so a bare bytes.Buffer read mid-run is a race the race detector
// will find and, before it does, a torn read.
//
// It lives in its own untagged file because every stand that photographs a
// misbehaving fixture BEFORE killing it needs one, and those stands are on
// both sides of the runtime_v2_pending tag.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
