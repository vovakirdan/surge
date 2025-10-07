package diag

import (
	"fmt"
	"sort"
)

type Bag struct {
	items []Diagnostic
	max   uint16
}

func NewBag(max int) *Bag {
	return &Bag{
		items: make([]Diagnostic, 0, max),
		max:   uint16(max),
	}
}

// Add добавляет диагностику, учитывая лимит.
// Возвращает false, если диагностика не добавлена (достигнут лимит).
func (b *Bag) Add(d Diagnostic) bool {
	if len(b.items) >= int(b.max) {
		return false
	}
	b.items = append(b.items, d)
	return true
}

func (b *Bag) Cap() uint16 {
	return b.max
}

// HasErrors возвращает true, если есть хотя бы одна диагностика с Severity >= Error
func (b *Bag) HasErrors() bool {
	for i := range b.items {
		if b.items[i].Severity >= SevError {
			return true
		}
	}
	return false
}

// HasWarnings возвращает true, если есть хотя бы одна диагностика с Severity >= Warning
func (b *Bag) HasWarnings() bool {
	for i := range b.items {
		if b.items[i].Severity >= SevWarning {
			return true
		}
	}
	return false
}

// длина
func (b *Bag) Len() int {
	return len(b.items)
}

// Items возвращает read-only slice диагностик.
// ВАЖНО: не модифицируйте возвращаемый срез! (он указывает на внутренний массив Bag)
func (b *Bag) Items() []Diagnostic {
	return b.items
}

// Merge объединяет диагностики из другого Bag.
// Увеличивает max, если нужно вместить все элементы.
func (b *Bag) Merge(other *Bag) {
	newTotal := len(b.items) + len(other.items)
	if uint16(newTotal) > b.max {
		b.max = uint16(newTotal)
	}
	b.items = append(b.items, other.items...)
}

// Sort сортирует диагностики по: file, start, end, severity (desc), code (asc)
// для стабильного и детерминированного порядка вывода.
func (b *Bag) Sort() {
	sort.SliceStable(b.items, func(i, j int) bool {
		di, dj := b.items[i], b.items[j]
		// сначала по файлу
		if di.Primary.File != dj.Primary.File {
			return di.Primary.File < dj.Primary.File
		}
		// затем по старту
		if di.Primary.Start != dj.Primary.Start {
			return di.Primary.Start < dj.Primary.Start
		}
		// затем по концу
		if di.Primary.End != dj.Primary.End {
			return di.Primary.End < dj.Primary.End
		}
		// затем по severity (по убыванию: Error > Warning > Info)
		if di.Severity != dj.Severity {
			return di.Severity > dj.Severity
		}
		// затем по коду (по возрастанию)
		return di.Code.String() < dj.Code.String()
	})
}

// простая дедупликация (по Code+Primary)
func (b *Bag) Dedup() {
	seen := make(map[string]bool)
	newitems := make([]Diagnostic, 0, len(b.items))
	for _, d := range b.items {
		key := fmt.Sprintf("%s:%s", d.Code.String(), d.Primary.String())
		if seen[key] {
			continue
		}
		seen[key] = true
		newitems = append(newitems, d)
	}
	b.items = newitems
}
