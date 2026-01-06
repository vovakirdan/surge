# Параллельность в Surge (статус v1)
[English](PARALLEL.md) | [Russian](PARALLEL.ru.md)

> **Коротко:** в v1 нет настоящего параллелизма. Есть только кооперативная
> конкурентность через `async`/`task` и каналы. Ключевые слова `parallel` и
> `signal` зарезервированы и не поддерживаются.

---

## 1. Что есть в v1

### 1.1. Кооперативная конкурентность

- Один поток исполнения, задачи переключаются в точках ожидания.
- Инструменты: `async`, `task`, `.await()`, `Channel<T>`.
- Результат ожидания: `TaskResult<T> = Success(T) | Cancelled`.

См. `docs/CONCURRENCY.ru.md` для точной модели.

### 1.2. Ограничения v1

- **Нет параллелизма** на нескольких ядрах.
- `parallel map/reduce` не поддерживаются (ошибка `FutParallelNotSupported`).
- `signal` не поддерживается (ошибка `FutSignalNotSupported`).

---

## 2. Альтернатива для data-parallel в v1

Если нужна обработка коллекции, используйте `task` + ожидание. В v1 нельзя
делать `await` в циклах, поэтому ожидание задач оформляется через рекурсию:

```sg
async fn await_all<T>(tasks: Task<T>[], idx: int, mut out: T[]) -> T[] {
    if idx >= (len(tasks) to int) { return out; }
    compare tasks[idx].await() {
        Success(v) => out.push(v);
        Cancelled() => return [];
    };
    return await_all(tasks, idx + 1, out);
}

async fn concurrent_map<T, U>(xs: T[], f: fn(T) -> U) -> U[] {
    let mut tasks: Task<U>[] = [];
    for x in xs {
        tasks.push(task f(x));
    }
    return await_all(tasks, 0, []);
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

## 4. План на v2+ (вкратце)

- Настоящий параллелизм на нескольких потоках.
- Data-parallel конструкции (`parallel map/reduce`).
- Реактивные вычисления (`signal`).

Детали будут уточняться по мере реализации; в v1 это **не часть спецификации**.
