# Промежуточные представления Surge (HIR/MIR)
[English](IR.md) | [Russian](IR.ru.md)

Этот документ описывает **фактический** IR-пайплайн в текущей версии компилятора,
а не план работ.

---

## 1. Общая схема

```
AST + Sema
   │
   ▼
HIR (typed) + borrow graph + move plan
   │
   ▼
Monomorphization (generic -> concrete)
   │
   ▼
MIR (CFG + instr/term)
   │
   ▼
CFG simplification + switch_tag recognition
   │
   ▼
Async lowering (poll state machines)
   │
   ▼
MIR validation
   │
   ▼
VM execution
```

Ключевые пакеты:

- HIR: `internal/hir`
- Monomorphization: `internal/mono`
- MIR: `internal/mir`
- ABI layout: `internal/layout`
- VM: `internal/vm`

---

## 2. HIR (High-level IR)

HIR строится после успешной семантики:

- Вход: AST + результаты `sema`
- Выход: `hir.Module` (типизированное дерево функций и выражений)

### 2.1. Что делает `hir.Lower`

`internal/hir/lower.go`:

- минимальный desugaring (например, убирает ExprGroup)
- нормализация высокоуровневых конструкций:
  - `compare` -> условные ветвления
  - `for` -> `while`
- **не** разворачивает async/spawn (это делает MIR)

### 2.2. Borrow graph и move plan

HIR включает дополнительные артефакты для анализа и отладки:

- `BorrowGraph`: рёбра заимствований и события (borrow/move/write/drop)
- `MovePlan`: политика перемещений для локалов (`MoveCopy`, `MoveAllowed`, ...)

Построение: `internal/hir/borrow_build.go`.

### 2.3. Как посмотреть HIR

Команда:

```bash
surge diag file.sg --emit-hir
surge diag file.sg --emit-borrow   # вместе с borrow graph + move plan
```

Дамп: `hir.DumpWithOptions`.

---

## 3. Monomorphization (generic -> concrete)

Пакет `internal/mono` превращает HIR с generics в конкретные инстансы:

- использует карту инстансов (`mono.InstantiationMap`)
- инстансы собираются в sema при `--emit-instantiations`
- есть DCE (dead code elimination) для моно-версий

Флаги CLI:

```bash
surge diag file.sg --emit-instantiations
surge diag file.sg --emit-mono --mono-dce --mono-max-depth=64
```

Дамп: `mono.DumpMonoModule`.

Примечание: `--emit-mono` поддерживается только для одиночных файлов (директории отклоняются).

---

## 4. MIR (Mid-level IR)

MIR — это CFG + инструкции + терминаторы.
Структуры: `internal/mir/*`.

### 4.1. Lowering в MIR

`mir.LowerModule` принимает моно-HIR и строит `mir.Module`:

- локалы, блоки, инструкции
- константы и статические строки
- метаданные ABI layout (`layout.LayoutEngine`)
- таблицы layout для tag/union

### 4.2. MIR-проходы

В `surge diag --emit-mir` выполняются следующие шаги:

1. `SimplifyCFG` — убирает тривиальные `goto`
2. `RecognizeSwitchTag` — превращает цепочки `if` в `switch_tag`
3. `SimplifyCFG` ещё раз
4. `LowerAsyncStateMachine` — async lowering
5. `SimplifyCFG` ещё раз
6. `Validate` — проверка инвариантов MIR

### 4.3. Дамп MIR

```bash
surge diag file.sg --emit-mir
```

Дамп: `mir.DumpModule`.

Примечание: `--emit-mir` поддерживается только для одиночных файлов.

---

## 5. Async lowering

`mir.LowerAsyncStateMachine`:

- превращает `async fn` в **poll state machine**
- разбивает `await` в отдельные suspend-блоки
- сохраняет/восстанавливает live locals между подвесами
- добавляет structured concurrency (`rt_scope_*`)

Примечание:

- `await` внутри циклов поддерживается.

---

## 6. Entrypoint lowering

Если есть `@entrypoint`, MIR строит синтетическую функцию
`__surge_start` (`internal/mir/entrypoint_*.go`).

Она:

- обрабатывает `@entrypoint("argv")` / `@entrypoint("stdin")`
- парсит аргументы через `from_str`
- возвращает корректный код выхода

---

## 7. Исполнение (VM)

MIR исполняется в VM (`internal/vm`).

Полезные флаги:

```bash
surge run file.sg --vm-trace
```

---

## 8. Где смотреть код

- HIR: `internal/hir/*`
- Borrow graph: `internal/hir/borrow_build.go`
- Monomorphization: `internal/mono/*`
- MIR: `internal/mir/*`
- Async lowering: `internal/mir/async_*`
- Entrypoint: `internal/mir/entrypoint_*.go`
- VM: `internal/vm/*`
