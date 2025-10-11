package source

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"
)

// Базовые тесты функциональности

func TestInternerBasic(t *testing.T) {
	interner := NewInterner()

	// NoStringID должен быть зарезервирован для пустой строки
	if s, ok := interner.Lookup(NoStringID); !ok || s != "" {
		t.Errorf("NoStringID должен возвращать пустую строку, получили: %q, ok=%v", s, ok)
	}

	// Intern новой строки
	id1 := interner.Intern("hello")
	if id1 == NoStringID {
		t.Error("Intern не должен возвращать NoStringID для непустой строки")
	}

	// Повторный Intern той же строки должен вернуть тот же ID
	id2 := interner.Intern("hello")
	if id1 != id2 {
		t.Errorf("Intern должен возвращать одинаковые ID для одинаковых строк: %d != %d", id1, id2)
	}

	// Lookup должен вернуть исходную строку
	if s, ok := interner.Lookup(id1); !ok || s != "hello" {
		t.Errorf("Lookup вернул неверную строку: %q, ok=%v", s, ok)
	}

	// Intern другой строки должен вернуть другой ID
	id3 := interner.Intern("world")
	if id3 == id1 {
		t.Error("Разные строки должны иметь разные ID")
	}

	// Len должен учитывать NoStringID
	if interner.Len() != 3 { // "", "hello", "world"
		t.Errorf("Len должен быть 3, получили: %d", interner.Len())
	}
}

func TestInternerBytes(t *testing.T) {
	interner := NewInterner()

	id1 := interner.InternBytes([]byte("test"))
	id2 := interner.Intern("test")

	if id1 != id2 {
		t.Errorf("InternBytes и Intern должны возвращать одинаковые ID для одной строки: %d != %d", id1, id2)
	}
}

func TestInternerHas(t *testing.T) {
	interner := NewInterner()

	if !interner.Has(NoStringID) {
		t.Error("Has должен возвращать true для NoStringID")
	}

	id := interner.Intern("test")
	if !interner.Has(id) {
		t.Error("Has должен возвращать true для валидного ID")
	}

	// Проверка несуществующего ID
	if interner.Has(StringID(9999)) {
		t.Error("Has должен возвращать false для несуществующего ID")
	}
}

func TestInternerMustLookup(t *testing.T) {
	interner := NewInterner()

	id := interner.Intern("test")
	s := interner.MustLookup(id)
	if s != "test" {
		t.Errorf("MustLookup вернул неверную строку: %q", s)
	}

	// Проверка паники для невалидного ID
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustLookup должен паниковать для невалидного ID")
		}
	}()
	interner.MustLookup(StringID(9999))
}

func TestInternerSnapshot(t *testing.T) {
	interner := NewInterner()

	interner.Intern("hello")
	interner.Intern("world")

	snapshot := interner.Snapshot()
	if len(snapshot) != 3 { // "", "hello", "world"
		t.Errorf("Snapshot должен содержать 3 элемента, получили: %d", len(snapshot))
	}

	// Проверка, что это копия (изменение snapshot не влияет на interner)
	snapshot[0] = "modified"
	if s, _ := interner.Lookup(NoStringID); s != "" {
		t.Error("Изменение snapshot не должно влиять на interner")
	}
}

// Тесты параллельного доступа

func TestInternerConcurrentIntern(t *testing.T) {
	interner := NewInterner()
	const numGoroutines = 100
	const numStrings = 1000

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Каждая горутина интернирует один и тот же набор строк
	for range numGoroutines {
		go func() {
			defer wg.Done()
			for i := range numStrings {
				s := fmt.Sprintf("string_%d", i)
				interner.Intern(s)
			}
		}()
	}

	wg.Wait()

	// Проверяем, что каждая строка интернирована ровно один раз
	// (не должно быть дубликатов)
	expectedLen := numStrings + 1 // +1 для NoStringID
	if interner.Len() != expectedLen {
		t.Errorf("Ожидалось %d строк, получили: %d", expectedLen, interner.Len())
	}

	// Проверяем, что все строки доступны и имеют уникальные ID
	ids := make(map[StringID]bool)
	for i := range numStrings {
		s := fmt.Sprintf("string_%d", i)
		id := interner.Intern(s)
		if ids[id] {
			t.Errorf("Дубликат ID для строки %q: %d", s, id)
		}
		ids[id] = true

		if retrieved, ok := interner.Lookup(id); !ok || retrieved != s {
			t.Errorf("Lookup вернул неверную строку для %q: %q, ok=%v", s, retrieved, ok)
		}
	}
}

func TestInternerConcurrentMixed(t *testing.T) {
	interner := NewInterner()
	const numGoroutines = 50
	const iterations = 1000

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Половина горутин делает Intern, половина - Lookup/Has
	for g := range numGoroutines {
		go func() {
			defer wg.Done()

			if g%2 == 0 {
				// Intern
				for i := range iterations {
					s := fmt.Sprintf("str_%d", i%100)
					interner.Intern(s)
				}
			} else {
				// Lookup/Has
				for i := range iterations {
					id := StringID(i % 50)
					interner.Has(id)
					interner.Lookup(id)
				}
			}
		}()
	}

	wg.Wait()

	// Проверка, что Len не паникует и возвращает разумное значение
	length := interner.Len()
	if length < 1 || length > 150 {
		t.Errorf("Неожиданный Len: %d", length)
	}
}

func TestInternerConcurrentSnapshot(t *testing.T) {
	interner := NewInterner()
	const numGoroutines = 20
	const numSnapshots = 100

	// Предзаполняем interner
	for i := range 100 {
		interner.Intern(fmt.Sprintf("initial_%d", i))
	}

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Половина горутин делает Snapshot, половина - Intern
	for g := range numGoroutines {
		go func() {
			defer wg.Done()

			if g%2 == 0 {
				// Snapshot
				for range numSnapshots {
					snapshot := interner.Snapshot()
					if len(snapshot) < 101 { // минимум initial strings + NoStringID
						t.Errorf("Snapshot слишком короткий: %d", len(snapshot))
					}
				}
			} else {
				// Intern
				for i := range numSnapshots {
					interner.Intern(fmt.Sprintf("concurrent_%d_%d", g, i))
				}
			}
		}()
	}

	wg.Wait()
}

// Тест на отсутствие дедлоков
func TestInternerNoDeadlock(t *testing.T) {
	if testing.Short() {
		t.Skip("Пропускаем deadlock тест в short режиме")
	}

	interner := NewInterner()
	const timeout = 5 // секунд
	const numGoroutines = 100

	done := make(chan bool, 1)

	go func() {
		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		for range numGoroutines {
			go func() {
				defer wg.Done()

				// Миксуем все операции
				for i := range 1000 {
					switch i % 7 {
					case 0:
						interner.Intern(fmt.Sprintf("s_%d", i))
					case 1:
						interner.InternBytes(fmt.Appendf([]byte{}, "s_%d", i))
					case 2:
						interner.Lookup(StringID(i % 100))
					case 3:
						interner.Has(StringID(i % 100))
					case 4:
						interner.Len()
					case 5:
						interner.Snapshot()
					case 6:
						if id := interner.Intern(fmt.Sprintf("s_%d", i%50)); interner.Has(id) {
							interner.MustLookup(id)
						}
					}
				}
			}()
		}

		wg.Wait()
		done <- true
	}()

	// Ждём с таймаутом
	select {
	case <-done:
		// Успешно завершилось
	case <-time.After(timeout * time.Second):
		t.Fatal("Тест завис - возможен дедлок")
	}
}

// Тест на race conditions (запускать с -race)
func TestInternerRaceConditions(t *testing.T) {
	if testing.Short() {
		t.Skip("Пропускаем race тест в short режиме")
	}

	interner := NewInterner()
	const numGoroutines = 100
	const numOps = 10000

	// Создаём контролируемый набор строк
	strings := make([]string, 100)
	for i := range strings {
		strings[i] = fmt.Sprintf("race_test_string_%d", i)
	}

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for range numGoroutines {
		go func() {
			defer wg.Done()

			for i := range numOps {
				str := strings[i%len(strings)]

				// Смешанные операции
				id := interner.Intern(str)
				if !interner.Has(id) {
					t.Errorf("Has вернул false для только что интернированного ID: %d", id)
				}
				if retrieved, ok := interner.Lookup(id); !ok || retrieved != str {
					t.Errorf("Lookup вернул неверную строку: ожидали %q, получили %q", str, retrieved)
				}

				// Проверка Len и Snapshot не паникуют
				_ = interner.Len()
				if i%100 == 0 {
					_ = interner.Snapshot()
				}
			}
		}()
	}

	wg.Wait()

	// Финальная проверка целостности
	for _, str := range strings {
		id := interner.Intern(str)
		if retrieved, ok := interner.Lookup(id); !ok || retrieved != str {
			t.Errorf("Финальная проверка: неверная строка для %q: %q", str, retrieved)
		}
	}
}

// Тест на корректность копирования строк
func TestInternerStringCopy(t *testing.T) {
	interner := NewInterner()

	// Создаём строку из буфера, который потом изменим
	buf := []byte("original")
	id := interner.InternBytes(buf)

	// Изменяем исходный буфер
	buf[0] = 'X'

	// Проверяем, что interner сохранил оригинальную строку
	if s, ok := interner.Lookup(id); !ok || s != "original" {
		t.Errorf("Interner должен сохранять копию строки, получили: %q", s)
	}
}

// Стресс-тест для проверки утечек памяти и производительности
func TestInternerStress(t *testing.T) {
	if testing.Short() {
		t.Skip("Пропускаем stress тест в short режиме")
	}

	interner := NewInterner()
	const numGoroutines = 50
	const numStrings = 10000

	var before runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := range numGoroutines {
		go func(gid int) {
			defer wg.Done()

			// Каждая горутина работает со своим набором строк
			// плюс общие строки для проверки дедупликации
			for i := range numStrings {
				// 50% уникальные для горутины, 50% общие
				var s string
				if i%2 == 0 {
					s = fmt.Sprintf("unique_%d_%d", gid, i)
				} else {
					s = fmt.Sprintf("shared_%d", i%1000)
				}

				id := interner.Intern(s)
				if retrieved, ok := interner.Lookup(id); !ok || retrieved != s {
					t.Errorf("Lookup вернул неверную строку")
				}
			}
		}(g)
	}

	wg.Wait()

	var after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&after)

	// Проверяем, что нет огромной утечки памяти
	allocDiff := after.Alloc - before.Alloc
	t.Logf("Использовано памяти: %d bytes, строк в interner: %d", allocDiff, interner.Len())

	// Проверяем ожидаемое количество строк
	// Уникальных: numGoroutines * numStrings / 2
	// Общих: max 1000 (из-за %1000)
	// Плюс NoStringID
	expectedMin := 1000 // минимум общие + NoStringID
	expectedMax := numGoroutines*numStrings/2 + 1000 + 1

	actualLen := interner.Len()
	if actualLen < expectedMin || actualLen > expectedMax {
		t.Errorf("Неожиданное количество строк: %d (ожидалось между %d и %d)",
			actualLen, expectedMin, expectedMax)
	}
}

// Бенчмарки

func BenchmarkInternerIntern(b *testing.B) {
	interner := NewInterner()
	strings := make([]string, 1000)
	for i := range strings {
		strings[i] = fmt.Sprintf("benchmark_string_%d", i)
	}

	b.ResetTimer()
	for i := range b.N {
		interner.Intern(strings[i%len(strings)])
	}
}

func BenchmarkInternerInternUnique(b *testing.B) {
	interner := NewInterner()

	b.ResetTimer()
	for i := range b.N {
		interner.Intern(fmt.Sprintf("unique_string_%d", i))
	}
}

func BenchmarkInternerInternDuplicate(b *testing.B) {
	interner := NewInterner()
	const str = "duplicate_string"

	// Предварительно интернируем
	interner.Intern(str)

	b.ResetTimer()
	for b.Loop() {
		interner.Intern(str)
	}
}

func BenchmarkInternerLookup(b *testing.B) {
	interner := NewInterner()
	ids := make([]StringID, 1000)
	for i := range ids {
		ids[i] = interner.Intern(fmt.Sprintf("string_%d", i))
	}

	b.ResetTimer()
	for i := range b.N {
		interner.Lookup(ids[i%len(ids)])
	}
}

func BenchmarkInternerHas(b *testing.B) {
	interner := NewInterner()
	ids := make([]StringID, 1000)
	for i := range ids {
		ids[i] = interner.Intern(fmt.Sprintf("string_%d", i))
	}

	b.ResetTimer()
	for i := range b.N {
		interner.Has(ids[i%len(ids)])
	}
}

func BenchmarkInternerSnapshot(b *testing.B) {
	interner := NewInterner()
	for i := range 1000 {
		interner.Intern(fmt.Sprintf("string_%d", i))
	}

	b.ResetTimer()
	for b.Loop() {
		_ = interner.Snapshot()
	}
}

// Бенчмарки параллельного доступа

func BenchmarkInternerConcurrentIntern(b *testing.B) {
	interner := NewInterner()
	strings := make([]string, 100)
	for i := range strings {
		strings[i] = fmt.Sprintf("concurrent_string_%d", i)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			interner.Intern(strings[i%len(strings)])
			i++
		}
	})
}

func BenchmarkInternerConcurrentInternUnique(b *testing.B) {
	interner := NewInterner()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			// Каждая горутина создаёт свои уникальные строки
			interner.Intern(fmt.Sprintf("unique_%d_%d", b.N, i))
			i++
		}
	})
}

func BenchmarkInternerConcurrentLookup(b *testing.B) {
	interner := NewInterner()
	ids := make([]StringID, 100)
	for i := range ids {
		ids[i] = interner.Intern(fmt.Sprintf("string_%d", i))
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			interner.Lookup(ids[i%len(ids)])
			i++
		}
	})
}

func BenchmarkInternerConcurrentMixed(b *testing.B) {
	interner := NewInterner()

	// Предзаполняем
	ids := make([]StringID, 100)
	for i := range ids {
		ids[i] = interner.Intern(fmt.Sprintf("string_%d", i))
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			switch i % 4 {
			case 0:
				interner.Intern(fmt.Sprintf("string_%d", i%100))
			case 1:
				interner.Lookup(ids[i%len(ids)])
			case 2:
				interner.Has(ids[i%len(ids)])
			case 3:
				_ = interner.Len()
			}
			i++
		}
	})
}

// Бенчмарк для сравнения с версией без блокировок
func BenchmarkInternerSequentialWorkload(b *testing.B) {
	interner := NewInterner()

	b.ResetTimer()
	for i := range b.N {
		// Типичная последовательная нагрузка
		id := interner.Intern(fmt.Sprintf("string_%d", i%1000))
		interner.Has(id)
		interner.Lookup(id)
	}
}
