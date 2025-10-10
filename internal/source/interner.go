package source

import (
	"slices"
)

type StringID uint32

const NoStringID StringID = 0

type Interner struct {
	byID  []string            // индекс -> строка (byID[0] = "" для NoStringID)
	index map[string]StringID // строка -> ID
}

func NewInterner() *Interner {
	return &Interner{
		byID:  []string{""},                    // NoStringID → пустая строка
		index: map[string]StringID{"": 0}, // сохраняем явное соответствие
	}
}

// Intern вставляет строку в иннер и возвращает её ID.
// Если строка уже есть, возвращает её ID.
func (i *Interner) Intern(s string) StringID {
	if id, ok := i.index[s]; ok {
		return id
	}

	// Создаём собственную копию строки, чтобы не зависеть от исходного буфера.
	cpy := string([]byte(s))
	id := StringID(len(i.byID))
	i.byID = append(i.byID, cpy)
	i.index[cpy] = id
	return id
}

// InternBytes вставляет байты в иннер и возвращает ID строки.
// Если строка уже есть, возвращает её ID.
func (i *Interner) InternBytes(b []byte) StringID {
	return i.Intern(string(b))
}

// Lookup возвращает строку по ID.
// Если ID не валиден, возвращает пустую строку и false.
func (i *Interner) Lookup(id StringID) (string, bool) {
	if !i.Has(id) {
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
func (i *Interner) Has(id StringID) bool {
	return int(id) >= 0 && int(id) < len(i.byID)
}

// Len возвращает количество строк в иннер.
// NoStringID тоже учитывается. Не может быть меньше 1.
func (i *Interner) Len() int {
	return len(i.byID)
}

// Возвращает копию всех строк в иннер.
func (i *Interner) Snapshot() []string {
	return slices.Clone(i.byID)
}
