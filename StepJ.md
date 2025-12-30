# Step J — Async runtime: план итераций

## J0 — Каркас: новые сущности и “not supported yet” → единый пайп

**Цель:** подготовить инфраструктуру, не реализуя семантику.

**Что делаем**

* Ввести в кодовую базу “async runtime module” (пакет), но пока пустой.
* В MIR добавить/финализировать типы:

  * `InstrSpawn`
  * `InstrAwait` (или `InstrPoll` как ты предложил)
  * `AsyncFnMeta` / маркер “эта функция async” (если ещё нет)
* В VM `execInstr`:

  * распознаёт `Await/Spawn/Poll`, но **пока** возвращает понятную ошибку “async backend not implemented” *в одном месте* (не раскидано).

**Acceptance**

* Компилится всё, включая парсинг/сему/понижение.
* Есть 1–2 golden/diag теста, что async конструкция падает предсказуемо (пока).

---

## J1 — Async lowering v1: single-suspend state machine (один await)

**Цель:** впервые пройти путь “async → state machine → poll → Pending/Ready”.

**Что делаем (MIR transform)**

* Ограничение: `async` блок/функция содержит **ровно один** `InstrAwait`.
* Генерируем:

  * state object (по сути tag/struct в твоём представлении) с 2 состояниями: `Start` и `AfterAwait`.
  * poll-fn, которая:

    * в `Start` выполняет код до await, сохраняет live-across-await в state, делает `Pending`.
    * в `AfterAwait` продолжает и возвращает `Ready(result)`.

**Что делаем (VM/runtime)**

* Ввести минимальный executor:

  * `spawn(async_expr)` создаёт Task (TCB + state handle + poll entry).
  * `await(task)` = join: если не готов — подписка и `Pending`.
* Ввести внутренний `InstrPoll { Dst, Task, ReadyBB, PendBB }` (как в твоей рекомендации):

  * VM вызывает poll и ветвится на BB.
  * `Dst` пишется только если `Ready`.

**Acceptance**

* Тест: `async { let x=1; checkpoint().await(); x+1 }` (checkpoint пока может быть “yield” примитив в рантайме).
* Тест: `let t = spawn async { ... }; let r = t.await();`
* Никаких каналов/таймеров/отмены ещё нет.

---

## J2 — Async lowering v2: multi-suspend + liveness (несколько await)

**Цель:** полноценная генерация state machine по CFG.

**Что делаем**

* Анализ await points: найти все `InstrAwait` / `InstrPoll` места.
* Разрезание CFG на сегменты между suspension points.
* Liveness analysis “live across suspension”:

  * Поднимаем только те locals, которые живы после конкретного await.
* Генерация state variants:

  * `S0`, `S1`, …, `Sn`, `Done`
  * Payload = live locals + любые промежуточные “awaited handle/result”, если нужно.
* Генерация poll:

  * `switch_tag(state)` → исполняем сегмент → при await сохраняем новый state → `Pending` → выход.

**Acceptance**

* Тесты с 2–3 await внутри одного async блока.
* Тест на ветвления: await на разных ветках `if`, чтобы state корректно отражал путь.
* Golden: отсутствие allocation Poll в VM (используем `InstrPoll`).

---

## J3 — Runtime core: детерминированный scheduler + waker API

**Цель:** отделить “исполнение state machine” от “планирования”, ввести wakeup-абстракцию.

**Что делаем**

* Executor:

  * `ready_queue` FIFO
  * run-loop: pop → poll → либо Done, либо Waiting (не requeue), либо Yielded (requeue)
* Внутренний waker:

  * `WakerKey` / “subscription token”
  * операции:

    * `park_current(wait_queue_id)`
    * `wake(task_id)` (requeue)
* Определить строго: **когда** задача requeue сама, а когда уходит в ожидание.

**Acceptance**

* Тест fairness: 2 tasks, которые по очереди yield’ятся — порядок стабилен.
* Тест `--fuzz-scheduler` (пока даже без seed, но лучше уже с seed):

  * очередь выбирается случайно, но воспроизводимо по seed.

---

## J4 — Join/await semantics: TaskResult + completion

**Цель:** зафиксировать контракт `.await()` и результаты.

**Что делаем**

* Ввести `TaskResult<T> = Ok(T) | Cancelled`.
* (Опционально сразу) `TaskOutcome<T,E> = Success(T)|Failed(E)|Cancelled` для Task<Erring>.
* Join waiters:

  * если task not done → подписываем current task в waiters list; current уходит в Waiting.
  * при completion → wake waiters FIFO (детерминированно).

**Acceptance**

* Тест: ожидание нескольких joiners одного task.
* Тест: join уже завершённого task (без park).

---

## J5 — Cancellation v1: cooperative cancel + checkpoint

**Цель:** отмена как control-flow, без ошибок.

**Что делаем**

* В TCB: `cancelled bool`.
* API:

  * `task.cancel()`
  * `checkpoint().await()`:

    * если cancelled → завершить текущую задачу как `Cancelled`
    * иначе yield (или просто return Ready)
* Правило:

  * cancel проверяется в await/checkpoint (кооперативно).

**Acceptance**

* Тест: cancel child → child возвращает Cancelled, parent это видит.
* Тест: cancel propagates в structured scope (см. J6).

---

## J6 — Structured concurrency v1: scope owns children + implicit join

**Цель:** “ничего не утекает за scope”.

**Что делаем (lowering + runtime)**

* В lowering:

  * при выходе из async scope вставить `join_all_children`.
  * при early-exit: policy:

    * либо “cancel children then join”
    * либо “join without cancel” (я бы выбрал cancel+join для failfast-like поведения в v1, но это твоё решение).
* В runtime safety net:

  * если scope завершён, а есть live children → deterministic panic/ICE (на этапе разработки).

**Acceptance**

* Тест: spawned task обязательно завершён до выхода из блока.
* Тест: early return cancels siblings (если выберете этот policy).
* Тест: попытка “утечки” (если можно смоделировать) → детерминированная ошибка.

---

## J7 — Channels v1: send/recv как awaitable + wait-queues

**Цель:** первый реально “блокирующий” примитив.

**Что делаем**

* `make_channel<T>(cap)` → handle.
* `send(ch, v).await()`:

  * если есть waiting receiver → передать и wake receiver
  * else если есть capacity → enqueue
  * else park sender (в очереди senders)
* `recv(ch).await()`:

  * если есть buffered value → вернуть
  * else если есть waiting sender → взять и wake sender
  * else park receiver
* `close(ch)` семантика (минимум):

  * recv на закрытом пустом → либо Option<T>, либо error/tag (решить сейчас).

**Acceptance**

* ping-pong 2 tasks через channel (детерминированный trace).
* backpressure test (cap=0 / cap=1).
* close behavior tests.

---

## J8 — Timers v1: sleep/timeout + min-heap

**Цель:** события времени как источник wakeups.

**Что делаем**

* Executor хранит min-heap таймеров.
* `sleep(ms).await()`: park task до deadline.
* `timeout(task, ms).await()`:

  * либо реализовать как “race” между join и timer (потребует select/race),
  * либо как primitive в runtime на первое время.

**Acceptance**

* sleep wakes correctly.
* timeout cancels/returns Cancelled по истечению времени (или отдельный Timeout tag — решите).

---

## J9 — Scheduler fuzzing: seed, режимы, тестовый harness

**Цель:** инструмент для ловли ordering-багов.

**Что делаем**

* `--fuzz-scheduler --fuzz-seed=<u64> --fuzz-steps=<n>`
* В CI (опционально): прогон некоторых async-тестов под несколькими seed.

**Acceptance**

* Падение воспроизводимо по seed.
* Нет flaky без fuzz.

---

# Два “замораживающих решения”, которые надо принять перед погружением в реализацию

1. **Форма poll в MIR:**  hybrid `InstrPoll` . Это фиксируем как v1 и не меняем.
2. **Политика structured concurrency на early-exit:**

   * `cancel+join` (мне кажется лучше для v1),
   * или “только join”.
     Это влияет на тесты и UX, лучше решить сейчас.
