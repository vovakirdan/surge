# Surge VM

## Зачем Surge VM

Surge VM — это **референсная среда исполнения** для Surge, созданная не ради скорости, а ради:

1. **Эквивалентности с LLVM**
   Любая программа, корректно отработавшая на VM, должна отработать **так же** при компиляции через LLVM backend (при одинаковых настройках runtime-политик).

2. **Полной наблюдаемости**
   VM — основной инструмент отладки и проверки семантики: пошаговое выполнение, трассировка каждой MIR-инструкции, backtrace со span’ами, просмотр locals/heap.

3. **Снятия “магии” перед LLVM**
   Вся логика “как программа исполняется” должна быть видна в IR и runtime ABI, а не скрыта в драйвере/кодогенераторе. LLVM backend должен быть “просто lowering MIR → LLVM IR”.

---

## Главный принцип

**VM не исполняет HIR. VM исполняет MIR.**

* MIR является единственным контрактом между фронтендом и бэкендами.
* VM интерпретирует MIR **напрямую** (на первом этапе без bytecode).
* LLVM backend тоже строится **только из MIR**, а runtime-интринсики имеют один и тот же ABI.

Это ключ к эквивалентности: два backend’а (VM и LLVM) выполняют одну и ту же MIR-программу.

---

## Роль `@entrypoint` и `__surge_start`

Entrypoint в Surge — это функция, помеченная `@entrypoint`, выбранная системой сборки как точка запуска.

В MIR это отражается явно:

* если entrypoint существует, MIR-модуль содержит synthetic-функцию `__surge_start() -> nothing`
* `__surge_start`:

  * получает вход (none/argv/stdin),
  * преобразует аргументы (через runtime intrinsics / FromArgv/FromStdin),
  * вызывает user entrypoint,
  * приводит результат к `int` (если нужно через `__to(..., int)`),
  * завершает процесс через `rt_exit(code)`.

VM запускает **только `__surge_start`**. Это устраняет особые случаи и “скрытые правила”.

---

## Что VM обязана гарантировать

### 1) Детерминированность

При одинаковом входе (argv/stdin) и одинаковой версии runtime:

* последовательность выполненных MIR-инструкций предсказуема,
* результат (stdout/stderr/exit code) одинаковый,
* ошибки рантайма воспроизводимы.

### 2) Эквивалентность с LLVM backend

Эквивалентность достигается за счёт:

* единого MIR,
* единого набора runtime intrinsics,
* совпадающих runtime политик (см. ниже).

### 3) Debug-first наблюдаемость

VM обязана уметь:

* трассировать исполнение по MIR (инструкция за инструкцией),
* показывать call stack, locals, значения, spans,
* давать backtrace по panic’ам,
* показывать состояние heap и объектов.

---

## Политики определённого поведения

Чтобы исключить расхождения “на грани”, VM фиксирует поведение там, где многие языки оставляют UB.

**Выбранная политика v1 (по умолчанию):**

* **bounds checks**: выход за границы массива/среза → panic
* **integer overflow**: переполнение на `int/uint` арифметике → panic

Почему так:

* это максимально debug-friendly,
* одинаково реализуемо и в VM, и в LLVM debug runtime,
* устраняет “оно работает в VM, но падает в LLVM / наоборот”.

В будущем допускается режим `--release` с другими политиками (wrap/UB), но он должен быть явно включаемым и одинаковым для VM и LLVM.

---

## Runtime errors

Любая ошибка исполнения в VM выражается как:

* **panic**
* с полями:

  * `code` (стабильный числовой код ошибки),
  * `message`,
  * `span` (file:line:col),
  * `backtrace` (список frames: function + span).

VM должна печатать panic в стабильном формате (подходящем для golden-тестов и CI).

---

## Архитектура VM

VM состоит из двух частей:

1. **MIR Interpreter** (движок исполнения)
2. **Runtime** (системные функции + heap + IO)

### 1) MIR Interpreter

Interpreter исполняет `mir.Module`:

* call stack: список `Frame`
* frame содержит:

  * ссылку на `mir.Func`
  * текущий basic block `bb`
  * позицию внутри блока `ip`
  * массив locals (`[]Value`) фиксированной длины, как в `mir.Func.locals`

#### Выполнение

* каждая MIR-инструкция выполняется последовательно (как написано),
* terminator завершает блок и выбирает следующий блок/возврат.

#### Контракт с MIR

Interpreter предполагает, что MIR уже прошёл:

* `SimplifyCFG`
* `Validate`

То есть:

* нет неизвестных типов,
* все блоки и переходы валидны,
* нет generics/type-param’ов.

### 2) Runtime

Runtime — отдельная подсистема, которую будут использовать **и VM, и LLVM backend** (в разных реализациях).

Runtime включает:

#### 2.1. IO / process

* `rt_exit(code: int) -> nothing`
* `rt_argv() -> string[]`
* `rt_stdin_read_all() -> string`

#### 2.2. Heap

VM использует **реальный heap**, независимый от языка реализации VM.

Heap — debug-friendly:

* каждый объект имеет:

  * `type_id`
  * `size`
  * `allocation_id`
  * состояние `alive/freed`
* проверки:

  * double-free → panic
  * use-after-free → panic
  * invalid handle → panic
* optional: leak report при завершении (в debug режиме)

#### 2.3. Типы значений (Value)

В VM значения представляются как:

* `Value { type: TypeID, payload: ... }`

Payload:

* примитивы (int, bool) — inline
* ссылочные типы (string, array, struct) — через handle на heap object

Это обеспечивает:

* корректную печать значений,
* одинаковую модель владения/перемещения,
* сильную диагностику ошибок.

---

## Ownership, Move/Copy и `EndBorrow`

MIR различает:

* `copy place`
* `move place`

VM обязана:

* для `move` помечать local как “moved-out”, и любые последующие чтения должны приводить к panic (use-after-move) **в debug policy**.
* `end_borrow place` — это не destruction. Это сигнал, полезный для:

  * дебага и трассировки lifetime,
  * будущих проверок корректности borrow-семантики на runtime уровне (если захочется).

---

## Tag unions и `SwitchTag`

`SwitchTag` — ключевой терминатор для Option/Erring и любых union/tag конструкций.

VM обязана:

* хранить tag у значения union/tag,
* на `switch_tag` выбирать ветку deterministically,
* на неверный доступ payload / неверный tag — panic.

---

## Debug-инструменты

VM предоставляет (как CLI режимы):

### Trace

* `--vm-trace` печатает каждую MIR-инструкцию:

  * func/bb/ip
  * instruction text
  * span
  * изменения locals (delta)
  * при желании: изменения heap (alloc/free)

### Step debugger

* `--vm-step` интерактивный режим:

  * step/next/continue
  * breakpoints по `file:line` и по `fn`
  * watch locals
  * bt (backtrace)
  * locals / stack / heap summary

### Source mapping

Любая MIR-инструкция хранит `Span`, чтобы:

* breakpoint ставился по исходнику,
* паники были с точной ссылкой на код.

---

## Эквивалентность с LLVM: практический критерий

Один и тот же тест должен выполняться в двух режимах:

* `--backend=vm`
* `--backend=llvm`

и давать одинаковые:

* exit code
* stdout/stderr (или нормализованные снапшоты)
* паники (code/message/span/backtrace форматированно совместимы)

Так VM становится “эталонным исполнителем”, а LLVM — проверяемым backend’ом.

---

## Невключённые в VM v1 темы

Чтобы держать шаги управляемыми, в VM v1 **не обязаны** быть:

* bytecode (может появиться позже для скорости)
* оптимизации
* полноценная многопоточность
* async/await state machine runtime (это отдельный этап поверх MIR, когда MIR уже поддерживает async lowering)

---

## Итоговая целевая картина

Surge VM — это:

* интерпретатор MIR,
* с отдельным runtime ABI,
* с реальным heap и строгими проверками,
* с panic’ами (code/span/backtrace),
* с trace/step-debugger,
* и с целью быть **эталоном корректности** для LLVM backend.
