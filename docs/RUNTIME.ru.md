# Рантайм Surge
[English](RUNTIME.md) | [Russian](RUNTIME.ru.md)

Этот документ описывает слои рантайма, которые исполняют уже пониженные
программы Surge. Правила async на уровне языка описаны в
`docs/CONCURRENCY.ru.md`; здесь речь о реализации VM и native/LLVM.

См. также: `docs/IR.ru.md`, `docs/CONCURRENCY.ru.md`, `docs/TRACING.ru.md`,
`docs/ABI_LAYOUT.ru.md`.

Срез топологии 2026-10-12: Tier 1 поддерживает только `N шардов × 1 carrier`.
`SURGE_THREADS`, peer stealing и compensation workers удалены.

---

## 1. Карта рантайма

В Surge есть две семьи исполнения:

- **VM-бэкенд:** Go-интерпретатор MIR в `internal/vm`, нужен для корректности,
  golden-тестов, диагностики, детерминированного планирования и record/replay.
- **Native/LLVM-бэкенд:** LLVM создает бинарь, связанный с C-рантаймом из
  `runtime/native`. Это production async-рантайм с `N` шардами, одним carrier
  на шард, native sockets, blocking pool и счетчиками native heap.

```mermaid
flowchart TD
    Source[Surge source] --> Frontend[parse, sema, HIR]
    Frontend --> MIR[MIR and async state machines]
    MIR --> VM[VM backend]
    MIR --> LLVM[LLVM backend]

    VM --> VMRT[Go VM runtime]
    LLVM --> Binary[Native binary]
    Binary --> CRT[C runtime]

    VMRT --> VMAsync[internal/asyncrt]
    CRT --> NativeAsync[sharded executor]
    CRT --> NativeNet[Native net runtime]
    CRT --> NativeHeap[Native heap and debug intrinsics]
```

VM и native-бэкенды имеют общую семантику языка, но не общий планировщик.

| Область | VM | Native/LLVM |
|---------|----|-------------|
| Основная роль | диагностика и parity | production-исполнение |
| Планировщик | один worker, детерминирован по умолчанию | multi-worker executor |
| Async tasks | Go executor поверх MIR poll state | C executor поверх скомпилированных poll-функций |
| Таймеры | virtual по умолчанию, есть VM real-time mode | executor time |
| Blocking scope | отклоняется | выделенный blocking pool |
| Network I/O | прямые MIR net waiters | native nonblocking sockets плюс poll thread |
| Heap debug | VM object heap и RC-проверки | native allocation counters |

---

## 2. VM-рантайм

VM исполняет MIR напрямую из `internal/vm`.

- Исполнение начинается с синтетической `__surge_start`, созданной lowering-ом
  `@entrypoint`.
- `VM.Step` интерпретирует MIR-инструкции и терминаторы.
- Каждый вызов пушит `Frame` с локалами и instruction pointer.
- Массивы, строки, структуры, tagged unions и владеемые значения живут в VM heap.
- Layout задается `layout.LayoutEngine` (см. `docs/ABI_LAYOUT.ru.md`).
- Значения явно дропаются; тесты проверяют порядок drop-а и утечки heap.

Интерфейс к хосту - `Runtime` в `internal/vm/runtime.go`:

- `Argv()` дает аргументы программы для `@entrypoint("argv")`.
- `StdinReadAll()` обслуживает `@entrypoint("stdin")`.
- `EntropyBytes(n)` дает host entropy для `stdlib/entropy`.
- `Exit(code)` фиксирует код выхода и останавливает исполнение.

Реализации включают `DefaultRuntime`, `TestRuntime`, `RecordingRuntime` и
`ReplayRuntime`.

### 2.1 VM async

VM async исполняется через `internal/asyncrt`:

- задачи - stackless state machines, созданные async lowering-ом;
- executor однопоточный и кооперативный;
- планирование по умолчанию детерминированное FIFO;
- fuzz-планирование может использовать фиксированный seed для воспроизводимых
  interleavings;
- scopes отслеживают structured concurrency и дочерние задачи;
- channels, timers, joins, select, race и cancellation паркуют задачи, а не
  блокируют OS thread.

VM - самый удобный бэкенд для семантических багов, потому что планировщик малый
и детерминированный. Native-only races и worker-pinning всё равно требуют
native/LLVM тестов.

---

## 3. Native/LLVM-рантайм

Native runtime - это C-код в `runtime/native`. Компилятор генерирует вызовы
runtime entry points для задач, каналов, network I/O, heap diagnostics, terminal
support и числовых helpers.

Native async state глобален на процесс и лениво инициализируется при первом
использовании рантайма. Центральная структура - `rt_executor` в
`rt_async_internal.h`.

Сгенерированная `__surge_start` — внешний адаптер: она дожидается synthetic
root task и получает только exit code типа `int64`. Подготовка аргументов,
глобальная инициализация, исполнение entrypoint и преобразование `ExitCode`
проходят на root carrier через обычные coroutine frames. Это относится и к
синхронному `fn main()`, и к `no_std`: внутренние типы root ABI не становятся
объявлениями, доступными исходной программе.

После join потоков при штатном shutdown освобождается пустая ёмкость таблиц и
очередей. Живые task/scope объекты эта очистка не уничтожает. Уже допущенная
блокирующая работа может остаться detached по модели V2; нужное ей состояние
сохраняется до завершения процесса.

```mermaid
flowchart LR
    subgraph Binary[Native Surge process]
        Main[main thread]
        Workers[executor workers]
        IO[IO poll thread]
        Blocking[blocking pool]
        Exec[rt_executor]
    end

    Main --> Exec
    Workers --> Exec
    IO --> Exec
    Blocking --> Exec

    Exec --> Tasks[tasks and scopes]
    Exec --> Ready[local queues and inject queue]
    Exec --> Waiters[waiter list]
    Exec --> Timers[executor time]
    Exec --> Channels[channels]
    Exec --> Net[net waiters]
```

Важное состояние, которым владеет executor:

- segmented tables задач и областей;
- per-shard waiter stores;
- по одной `local_queue` и `inject` queue на шард;
- `worker_cv` и poller state каждого шарда; `done_cv` только для внешнего await;
- `blocking_*`: отдельный pool для `blocking { ... }`.

### 3.1 Жизненный цикл задачи

```mermaid
stateDiagram-v2
    [*] --> READY: create or wake
    READY --> RUNNING: worker polls
    RUNNING --> READY: yields
    RUNNING --> WAITING: parks on waker key
    WAITING --> READY: wake_task
    RUNNING --> DONE: success or cancelled
    WAITING --> DONE: cancellation completes
    DONE --> [*]: last handle released
```

Базовые инварианты:

- Задача никогда не poll'ится одновременно несколькими carriers.
- Переходы READY/WAITING сериализует owner-shard lane; состояние области пишет
  только её owner shard, а foreign completion приходит событием.
- Ready queues хранят task IDs с установленным флагом `enqueued`; дубли в
  очередях отбрасываются.
- `wake_token` закрывает wake-before-park race, когда wake приходит во время
  подготовки задачи к park.
- User poll functions выполняются вне runtime locks; переход в `TASK_RUNNING`
  и выход из него защищает owner-shard lane.

### 3.2 Планирование

Native runtime создаёт `N` шардов и ровно один carrier на каждый. У шарда есть
одна локальная LIFO queue и одна injection FIFO. Peer stealing в Tier 1 нет.

```mermaid
flowchart TD
    Carrier[carrier шарда] --> Local{есть local work?}
    Local -->|да| Poll[poll задачи]
    Local -->|нет| Inject{есть inject work?}
    Inject -->|да| Poll
    Inject -->|нет| Service{timer, transport или net ready?}
    Service -->|да| Publish[опубликовать owner shard]
    Service -->|нет| Sleep[wait на worker_cv шарда]
    Publish --> Carrier
```

`SURGE_SCHED=seeded` воспроизводимо выбирает между одновременно непустыми local
и inject queues при том же seed и порядке внешних событий. OS scheduling, socket
readiness, FFI и blocking pool он не контролирует.

- `SURGE_SHARDS=<n>` задаёт число шардов и carriers Tier 1.
- Без override используется число online CPU, максимум 64.
- `rt_worker_count()` равен числу шардов.
- `SURGE_THREADS` удалён и не читается.

`wake_pending` — один durable wake token шарда, закрывающий гонку публикации со
сном. Это не per-task credit и не выбор среди peer workers.

### 3.3 Каналы

Native channels - FIFO handles с опциональным ограниченным buffer. Прямые
операции каналов в async-коде используют task parking:

```mermaid
sequenceDiagram
    participant Sender
    participant Channel
    participant Receiver
    participant Scheduler

    Sender->>Channel: send value
    alt receiver already waiting
        Channel->>Receiver: set resume value
        Channel->>Scheduler: wake receiver
        Channel-->>Sender: send complete
    else buffer has capacity
        Channel-->>Sender: buffer value
    else no receiver and buffer full
        Channel->>Scheduler: park sender on CHAN_SEND
    end

    Receiver->>Channel: recv
    alt value buffered
        Channel-->>Receiver: return value
        Channel->>Scheduler: wake one parked sender
    else sender waiting
        Channel-->>Receiver: take sender value
        Channel->>Scheduler: wake sender with send ack
    else empty
        Channel->>Scheduler: park receiver on CHAN_RECV
    end
```

Direct async channel operations паркуют задачу и возвращают carrier
планировщику. Компилятор обязан понизить `send` и `recv` внутри async task в
suspendable path. Попадание в blocking channel ABI из работающей задачи
fail-closed с `channel send requires async lowering` или
`channel recv requires async lowering`; VM держит ту же границу. Внешний
синхронный caller может использовать blocking helper, потому что он не держит
carrier Tier 1. Compensation workers и task-context compatibility path удалены.

### 3.4 Network I/O

Network readiness waits паркуют текущую async-задачу напрямую:

- `rt_net_wait_accept`, `rt_net_wait_readable` и `rt_net_wait_writable` — это
  suspendable intrinsics, которые lowering превращает в ready/pending poll
  branches.
- Runtime сначала пробует `poll(..., timeout=0)` для fd.
- Если fd не готов, текущая задача паркуется на net waker key.
- I/O thread следит за зарегистрированными net waiters через `poll`.
- Когда fd готов, он будит matching parked tasks.

Эти ожидания не аллоцируют `Task<nothing>` handles и не добавляют join layer
между socket readiness и пользовательской задачей.

I/O thread сигналится, когда executor становится idle, когда регистрируются net
waiters или когда меняется shutdown state. `TRACE_NET` counters выводятся как
часть execution tracing.

### 3.5 Blocking pool

`blocking { ... }` выполняется вне executor workers:

- block становится `TASK_KIND_BLOCKING` task;
- работа отправляется в выделенный blocking pool;
- async task ожидает завершения через blocking waker key;
- cancellation best-effort, потому что underlying OS call может быть
  непрерываемым.

Размер pool по умолчанию равен числу executor workers и переопределяется через
`SURGE_BLOCKING_THREADS=<n>`.

### 3.6 Heap и debug intrinsics

Native allocations проходят через `rt_alloc`, `rt_free` и `rt_realloc`.
Runtime записывает allocation/free/realloc events в heap-accounting cells,
которые принадлежат runtime или shard state. Intrinsic `rt_heap_stats()`
агрегирует эти cells и отдает allocation count, free count, live blocks и live
bytes.

У VM собственная heap model и похожее debug-facing поведение, где это возможно,
но native counters описывают только native allocation traffic.

---

## 4. Runtime tracing

Runtime tracing отделен от compiler tracing в `docs/TRACING.ru.md`.

| Управление | Эффект |
|------------|--------|
| `SURGE_TRACE_EXEC=1` | выводит `TRACE_EXEC`, `TRACE_EXEC_SNAPSHOT` и `TRACE_NET` в stderr |
| `SIGUSR1` вместе с `SURGE_TRACE_EXEC=1` | запрашивает live execution snapshot на поддерживаемых платформах |
| `SURGE_SCHED_TRACE=1` | выводит summary lines `SCHED_TRACE` |
| `SURGE_SCHED=seeded` | включает seeded scheduler decisions |
| `SURGE_SCHED_SEED=<n>` | задает seed для seeded scheduler |
| `SURGE_ASYNC_DEBUG=1` | включает подробные native async debug prints |
| `SURGE_CHANNEL_WAKE_INJECT=1` | принудительно отправляет channel wake через inject для экспериментов |

Полезные поля `TRACE_EXEC`:

- `wake_called`, `wake_enqueued`, `park_attempt`, `park_committed`: активность
  wake/park.
- `channel_handoff_yield`: direct async channel handoff yields.
- `waiters_*`, `tasks_*`, `local_total`, `inject_len`: форма scheduler snapshot.

Полезные поля `SCHED_TRACE`:

- `local`, `inject`: сколько задач взято из local или injection queue шарда.
- `events`: общее число scheduler pops в summary line.

Полезные поля `TRACE_NET`:

- `io_poll_calls`, `io_poll_timeouts`, `io_poll_timeout_max_ms`;
- `io_poll_wake_fd`, `io_poll_net_ready`, `io_poll_errors`;
- `io_poll_waiters_last`, `io_poll_waiters_max`, `io_poll_waiters_total`;
- `io_direct_waits`: прямые парковки задач на network readiness;
- `io_waiter_scan_entries`, `io_waiter_net_entries`: стоимость сканирования
  общего waiter-list для net polling;
- `io_poll_rebuilds`, `io_poll_allocs`, `io_poll_dedup_checks`: стоимость
  текущей пересборки poll set;
- `io_waiter_complete_calls`, `io_waiter_completed`: wake/remove активность
  для net readiness.

Синхронный channel ABI из работающей задачи не имеет trace-only fallback и
немедленно отказывается: нужен async lowering.

---

## 5. Диагностика runtime issues

Начинайте с самого маленького слоя, который воспроизводит симптом.

1. **Backend check:** запустите программу на VM и native/LLVM, если возможно.
   VM-only failure обычно указывает на MIR/semantics; native-only failure - на
   C runtime, LLVM lowering или real I/O.
2. **Shard count:** сравните `SURGE_SHARDS=1`, `2` и `8`; в каждой строке один
   carrier на shard.
3. **Trace counters:** запустите с `SURGE_TRACE_EXEC=1` и проверьте channel,
   waiter, queue, transport и net counters.
4. **Scheduler mode:** используйте
   `SURGE_SCHED=seeded SURGE_SCHED_SEED=<n>` для воспроизводимых scheduler
   choices. Не ждите полной воспроизводимости внешнего I/O.
5. **Channel path:** panic `requires async lowering` означает, что sync helper
   скрыл channel operation от suspension lowering.
6. **Network path:** если net latency остается после чистых channel counters,
   сравните с маленькой native echo/accept программой до обвинения application
   logic.

---

## 6. Текущие ограничения

- VM остается single-worker по дизайну.
- Native/LLVM scheduling между шардами не является глобально детерминированным.
- Seeded scheduling best-effort и зависит от порядка внешних событий.
- Waiter stores принадлежат шардам; порядок foreign completions зависит от
  порядка transport delivery.
- Синхронные channel send/recv из task context не поддерживаются и отказываются;
  требуется async lowering.
- `parallel map/reduce` и `signal` остаются зарезервированными возможностями
  языка.
