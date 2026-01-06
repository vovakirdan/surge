package diag

import (
	"fmt"
	"sort"

	"fortio.org/safecast"
)

// Bag holds a collection of diagnostics.
type Bag struct {
	items   []*Diagnostic
	maximum uint16
}

// NewBag creates a Bag with a capacity limit.
func NewBag(maximum int) *Bag {
	result, err := safecast.Conv[uint16](maximum)
	if err != nil {
		panic(fmt.Errorf("bag maximum overflow: %w", err))
	}
	return &Bag{
		items:   make([]*Diagnostic, 0, result),
		maximum: result,
	}
}

// Add добавляет диагностику, учитывая лимит.
// Возвращает false, если диагностика не добавлена (достигнут лимит).
func (b *Bag) Add(d *Diagnostic) bool {
	if d == nil {
		return false
	}
	if len(b.items) >= int(b.maximum) {
		return false
	}
	b.items = append(b.items, d)
	return true
}

// Cap returns the maximum capacity of the bag.
func (b *Bag) Cap() uint16 {
	return b.maximum
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

// Len returns the number of diagnostics in the bag.
func (b *Bag) Len() int {
	return len(b.items)
}

// Items возвращает read-only slice диагностик.
// ВАЖНО: не модифицируйте возвращаемый срез! (он указывает на внутренний массив Bag)
func (b *Bag) Items() []*Diagnostic {
	return b.items
}

// Merge объединяет диагностики из другого Bag.
// Увеличивает max, если нужно вместить все элементы.
func (b *Bag) Merge(other *Bag) {
	newTotal := len(b.items) + len(other.items)
	newTotalUint16, err := safecast.Conv[uint16](newTotal)
	if err != nil {
		panic(fmt.Errorf("bag merge overflow: %w", err))
	}
	if newTotalUint16 > b.maximum {
		b.maximum = newTotalUint16
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

// Dedup performs a simple de-duplication by Code and Primary span.
func (b *Bag) Dedup() {
	seen := make(map[string]bool)
	newitems := make([]*Diagnostic, 0, len(b.items))
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

// Filter удаляет диагностики, которые не проходят проверку predicate
func (b *Bag) Filter(predicate func(*Diagnostic) bool) {
	newitems := make([]*Diagnostic, 0, len(b.items))
	for _, d := range b.items {
		if predicate(d) {
			newitems = append(newitems, d)
		}
	}
	b.items = newitems
}

// Transform применяет функцию к каждой диагностике
func (b *Bag) Transform(transformer func(*Diagnostic) *Diagnostic) {
	for i := range b.items {
		next := transformer(b.items[i])
		if next == nil {
			panic("diag: transformer returned nil")
		}
		b.items[i] = next
	}
}
