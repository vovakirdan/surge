package sema

import (
	"surge/internal/source"
	"surge/internal/symbols"
)

// LockKind represents the type of lock operation
type LockKind int

const (
	LockKindMutex   LockKind = iota // Mutex.lock()
	LockKindRwRead                  // RwLock.read_lock()
	LockKindRwWrite                 // RwLock.write_lock()
)

func (k LockKind) String() string {
	switch k {
	case LockKindMutex:
		return "mutex"
	case LockKindRwRead:
		return "read"
	case LockKindRwWrite:
		return "write"
	default:
		return "unknown"
	}
}

// LockKey uniquely identifies a held lock
type LockKey struct {
	Base      symbols.SymbolID // Root variable (self, parameter, local)
	FieldName source.StringID  // Field name of the lock
	Kind      LockKind
}

// LockAcquisition records where a lock was acquired
type LockAcquisition struct {
	Key  LockKey
	Span source.Span // Location where lock was acquired
}

// LockState tracks currently held locks within a function
type LockState struct {
	held []LockAcquisition // Stack of held locks (in acquisition order)
}

// NewLockState creates a new empty lock state
func NewLockState() *LockState {
	return &LockState{
		held: make([]LockAcquisition, 0, 4),
	}
}

// Clone creates a copy of the lock state (for branch analysis)
func (s *LockState) Clone() *LockState {
	clone := &LockState{
		held: make([]LockAcquisition, len(s.held)),
	}
	copy(clone.held, s.held)
	return clone
}

// IsHeld checks if a lock is currently held
func (s *LockState) IsHeld(key LockKey) bool {
	for _, acq := range s.held {
		if acq.Key == key {
			return true
		}
	}
	return false
}

// FindAcquisition returns the acquisition info for a held lock, if any
func (s *LockState) FindAcquisition(key LockKey) (LockAcquisition, bool) {
	for _, acq := range s.held {
		if acq.Key == key {
			return acq, true
		}
	}
	return LockAcquisition{}, false
}

// Acquire attempts to acquire a lock. Returns error info if double-lock detected.
func (s *LockState) Acquire(key LockKey, span source.Span) (prevSpan source.Span, doubleLock bool) {
	if prev, found := s.FindAcquisition(key); found {
		return prev.Span, true
	}
	s.held = append(s.held, LockAcquisition{Key: key, Span: span})
	return source.Span{}, false
}

// Release attempts to release a lock. Returns false if lock was not held.
func (s *LockState) Release(key LockKey) bool {
	for i, acq := range s.held {
		if acq.Key == key {
			// Remove from held list
			s.held = append(s.held[:i], s.held[i+1:]...)
			return true
		}
	}
	return false
}

// HeldLocks returns all currently held locks
func (s *LockState) HeldLocks() []LockAcquisition {
	return s.held
}

// IsEmpty returns true if no locks are held
func (s *LockState) IsEmpty() bool {
	return len(s.held) == 0
}

// PathOutcome represents what happened on a control flow path
type PathOutcome int

const (
	PathContinues     PathOutcome = iota // Path reaches merge point normally
	PathReturns                          // Path exits via return
	PathBreaks                           // Path exits via break
	PathContinuesLoop                    // Path exits via continue (loop)
)
