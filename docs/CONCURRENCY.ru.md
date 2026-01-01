# Модель конкурентности Surge v1
[English](CONCURRENCY.md) | [Russian](CONCURRENCY.ru.md)

> **Статус:** Реализовано в VM (однопоточный кооперативный планировщик)
> **Область:** async/await, Task/TaskResult, spawn, каналы, отмена, таймауты
> **Вне области:** параллелизм на уровне потоков ОС, сигналы, параллельный map/reduce

---

## 1. Модель вкратце

Surge v1 использует **однопоточный** исполнитель (executor) с **кооперативным планированием**:

- Задачи (Tasks) — это **конечные автоматы**, а не потоки ОС.
- Задача выполняется до тех пор, пока не достигнет точки приостановки (`await`, операции с каналами, `sleep`, `checkpoint`).
- `spawn` **планирует** задачу для конкурентного выполнения.
- Отмена является **кооперативной** и наблюдается только в точках приостановки.

Это сохраняет модель владения (ownership model) надежной без проверки заимствований (borrow checking) между потоками.

---

## 2. Task и TaskResult

Основные определения находятся в `core/intrinsics.sg`:

```sg
pub type Task<T> = { __opaque: int };

pub tag Cancelled();
pub type TaskResult<T> = Success(T) | Cancelled;

extern<Task<T>> {
    @intrinsic pub fn clone(self: &Task<T>) -> Task<T>;
    @intrinsic pub fn cancel(self: &Task<T>) -> nothing;
    @intrinsic pub fn await(self: own Task<T>) -> TaskResult<T>;
}
```

Ключевые моменты:

- `Task<T>` — это непрозрачный handle (дескриптор) конечного автомата.
- `.await()` **поглощает** `own Task<T>` и возвращает `TaskResult<T>`.
- Используйте `task.clone()`, если вам нужно несколько дескрипторов.
- `cancel()` работает по принципу "best-effort"; задачи замечают отмену в точках приостановки.

Пример:

```sg
let t = spawn fetch_user(42);
compare t.await() {
    Success(user) => print(user.name);
    Cancelled() => print("cancelled");
}
```

---

## 3. async функции и async блоки

```sg
async fn fetch_user(id: int) -> User {
    let raw = http_get("/users/" + id).await();
    return parse(raw);
}

let t: Task<User> = fetch_user(42);
```

- `async fn` возвращает `Task<T>` немедленно; она не запускается, пока не будет вызвана `await` или `spawn`.
- `async { ... }` создает анонимную `Task<T>` из блока.

`@failfast` разрешен на **async функциях** и **async блоках**:

```sg
@failfast
async fn pipeline() -> nothing {
    let a = spawn step_a();
    let b = spawn step_b();

    compare a.await() {
        Success(_) => nothing;
        Cancelled() => return;
    };
    compare b.await() {
        Success(_) => nothing;
        Cancelled() => return;
    };
}
```

Failfast означает: если дочерняя задача завершается с `Cancelled`, область видимости отменяет
оставшихся детей, и родитель возвращает `Cancelled`.

---

## 4. spawn

```sg
spawn expr
```

Правила:

- `expr` должно быть `Task<T>` (вызов async функции или async блок).
- `spawn` планирует задачу и возвращает дескриптор `Task<T>`.
- Только `own` значения могут пересекать границу spawn.
- Типы с `@nosend` отвергаются в spawn (`SemaNosendInSpawn`).
- `spawn checkpoint()` вызывает предупреждение как бесполезный вызов (`SemaSpawnCheckpointUseless`).

Пример:

```sg
async fn work(x: int) -> int { return x * 2; }

let t1 = spawn work(10);
let t2 = spawn work(20);

compare t1.await() {
    Success(v) => print("t1=" + (v to string));
    Cancelled() => print("t1 cancelled");
}
compare t2.await() {
    Success(v) => print("t2=" + (v to string));
    Cancelled() => print("t2 cancelled");
}
```

---

## 5. await

`.await()` — это **вызов метода**, возвращающий `TaskResult<T>`:

```sg
compare fetch_user(42).await() {
    Success(user) => print(user.name);
    Cancelled() => print("cancelled");
}
```

Правила:

- Разрешено внутри `async` функций/блоков и `@entrypoint` функций.
- Запрещено в простых синхронных функциях (`SemaIntrinsicBadContext`).
- `await` внутри циклов в настоящее время **не поддерживается** (MIR lowering отвергает это).

---

## 6. Структурированная конкурентность (Scopes)

Surge принуждает к структурированной конкурентности на этапе семантического анализа (sema):

- Порожденные (spawned) задачи должны быть **ожидаемы (awaited) или возвращены**.
- Утечка задачи из области видимости вызывает ошибки:
  - `SemaTaskNotAwaited` (3107)
  - `SemaTaskEscapesScope` (3108)
  - `SemaTaskLeakInAsync` (3109)
  - `SemaTaskLifetimeError` (3110)

В рантайме каждая async функция/блок создает область видимости (scope). При выходе из области
рантайм джойнит (joins) всех детей перед завершением. Возврат `Task<T>` передает
ответственность вызывающему.

---

## 7. Отмена, Таймауты и Yielding (Уступка)

Intrinsics:

```sg
@intrinsic pub fn checkpoint() -> Task<nothing>;
@intrinsic pub fn sleep(ms: uint) -> Task<nothing>;
@intrinsic pub fn timeout<T>(t: Task<T>, ms: uint) -> TaskResult<T>;
```

Заметки:

- `checkpoint().await()` уступает управление планировщику и проверяет отмену.
- `sleep(ms).await()` приостанавливает выполнение на `ms` (виртуальное время в VM).
- `timeout(t, ms)` ждет до `ms` и возвращает `Success` или `Cancelled`.
  Он отменяет цель по истечении времени.

Пример:

```sg
let t = spawn slow_call();
compare timeout(t, 500:uint) {
    Success(v) => print("done " + (v to string));
    Cancelled() => print("timed out");
}
```

---

## 8. Каналы (Channels)

`Channel<T>` — это типизированный FIFO дескриптор (копируемый):

```sg
let ch = make_channel::<int>(16);
ch.send(42);
let v = ch.recv();
```

API (core intrinsics):

- `make_channel<T>(capacity: uint) -> own Channel<T>`
- `Channel<T>::new(capacity: uint) -> own Channel<T>`
- `send(self: &Channel<T>, value: own T) -> nothing` (блокирующий)
- `recv(self: &Channel<T>) -> Option<T>` (блокирующий)
- `try_send(self: &Channel<T>, value: own T) -> bool`
- `try_recv(self: &Channel<T>) -> Option<T>`
- `close(self: &Channel<T>) -> nothing`

Заметки:

- `send`/`recv` являются точками приостановки в async коде.
- `recv` возвращает `nothing`, когда канал закрыт и пуст.
- Отправка в закрытый канал — ошибка времени выполнения.
- Значения с `@nosend` нельзя передавать через каналы (`SemaChannelNosendValue`).

---

## 9. Ограничения (v1)

- Однопоточный рантайм; нет истинного параллелизма.
- `parallel map/reduce` и `signal` являются зарезервированными ключевыми словами (не поддерживаются).
- `await` внутри циклов отвергается во время async lowering.
- Справедливость (fairness) между задачами не гарантируется.

См. `docs/PARALLEL.ru.md` для статуса параллельных функций.