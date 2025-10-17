package source

import (
	"slices"
	"sync"
)

type StringID uint32

const NoStringID StringID = 0

type Interner struct {
	mu    sync.RWMutex
	byID  []string            // индекс -> строка (byID[0] = "" для NoStringID)
	index map[string]StringID // строка -> ID
}

func NewInterner() *Interner {
	return &Interner{
		byID:  []string{""},               // NoStringID → пустая строка
		index: map[string]StringID{"": 0}, // сохраняем явное соответствие
	}
}

// Intern вставляет строку в иннер и возвращает её ID.
// Если строка уже есть, возвращает её ID.
// Потокобезопасно.
func (i *Interner) Intern(s string) StringID {
	// Быстрая ветка: проверяем наличие под read lock
	i.mu.RLock()
	if id, ok := i.index[s]; ok {
		i.mu.RUnlock()
		return id
	}
	i.mu.RUnlock()

	// Создаём собственную копию строки, чтобы не зависеть от исходного буфера.
	cpy := string([]byte(s))

	// Переходим к записи
	i.mu.Lock()
	// Double-check: между RUnlock и Lock другая горутина могла добавить строку
	if id, ok := i.index[cpy]; ok {
		i.mu.Unlock()
		return id
	}
	id := StringID(len(i.byID))
	i.byID = append(i.byID, cpy)
	i.index[cpy] = id
	i.mu.Unlock()
	return id
}

// InternBytes вставляет байты в иннер и возвращает ID строки.
// Если строка уже есть, возвращает её ID.
func (i *Interner) InternBytes(b []byte) StringID {
	return i.Intern(string(b))
}

// Lookup возвращает строку по ID.
// Если ID не валиден, возвращает пустую строку и false.
// Потокобезопасно.
func (i *Interner) Lookup(id StringID) (string, bool) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	if int(id) < 0 || int(id) >= len(i.byID) {
		return "", false
	}
	return i.byID[id], true
}

// MustLookup возвращает строку по ID.
// Если ID не валиден, паникует.
func (i *Interner) MustLookup(id StringID) string {
	s, ok := i.Lookup(id)
	if !ok {
		panic("invalid string ID")
	}
	return s
}

// Has проверяет, валиден ли ID.
// Потокобезопасно.
func (i *Interner) Has(id StringID) bool {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return int(id) >= 0 && int(id) < len(i.byID)
}

// Len возвращает количество строк в иннер.
// NoStringID тоже учитывается. Не может быть меньше 1.
// Потокобезопасно.
func (i *Interner) Len() int {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return len(i.byID)
}

// Snapshot возвращает копию всех строк в иннер.
// Потокобезопасно.
func (i *Interner) Snapshot() []string {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return slices.Clone(i.byID)
}
