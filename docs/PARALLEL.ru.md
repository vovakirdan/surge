# Параллельность в Surge (текущий статус)
[English](PARALLEL.md) | [Russian](PARALLEL.ru.md)

> **Коротко:** в Surge есть кооперативная конкурентность через `async`/`spawn` и каналы.
> Native/LLVM используют многопоточный исполнитель, VM однопоточная.
> Ключевые слова `parallel` и `signal` зарезервированы и не поддерживаются.

---

## 1. Что есть сегодня

### 1.1. Кооперативная конкурентность

- Кооперативное планирование на точках ожидания.
- Native/LLVM используют несколько воркеров, VM остаётся однопоточной.
- Инструменты: `async`, `spawn`, `.await()`, `Channel<T>`.
- Результат ожидания: `TaskResult<T> = Success(T) | Cancelled`.

См. `docs/CONCURRENCY.ru.md` для точной модели.

### 1.2. Текущие ограничения

- `parallel map/reduce` не поддерживаются (ошибка `FutParallelNotSupported`).
- `signal` не поддерживается (ошибка `FutSignalNotSupported`).

---

## 2. Альтернатива для data-parallel сегодня

Если нужна обработка коллекции, используйте `spawn` + `.await()`:

```sg
async fn concurrent_map<T, U>(xs: T[], f: fn(T) -> U) -> U[] {
    let mut tasks: Task<U>[] = [];
    for x in xs {
        tasks.push(spawn f(x));
    }

    // `.await()` поглощает task handle, поэтому по задачам удобно идти по значению.
    let mut out: U[] = [];
    for t in tasks {
        compare t.await() {
            Success(v) => out.push(v);
            Cancelled() => return [];
        };
    }
    return out;
}
```

Если нужно взаимодействие между задачами, используйте `Channel<T>`.

---

## 3. Зарезервированные конструкции

### 3.1. `parallel map/reduce`

Синтаксис зарезервирован, но в v1 отклоняется семантикой:

```sg
parallel map xs with (x) => x * x
parallel reduce xs with 0, (acc, x) => acc + x
```

Текущий статус: ошибка `FutParallelNotSupported`.

### 3.2. `signal`

Синтаксис зарезервирован, но в v1 отклоняется семантикой:

```sg
signal total := price + tax;
```

Текущий статус: ошибка `FutSignalNotSupported`.

---

## 4. План на будущее (вкратце)

- Data-parallel конструкции (`parallel map/reduce`).
- Реактивные вычисления (`signal`).

Детали будут уточняться по мере реализации; это **не часть текущей спецификации**.
