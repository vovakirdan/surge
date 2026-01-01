# Трассировка компилятора Surge
[English](TRACING.md) | [Russian](TRACING.ru.md)

Surge включает встроенную систему трассировки для диагностики зависаний компилятора, проблем
производительности и поведения пайплайна. Трассировка управляется глобальными флагами CLI и
выдает структурированные события во время `surge diag`, `surge parse` и других команд.

---

## Быстрый старт

```bash
# Высокоуровневые фазы в stderr
surge diag file.sg --trace=- --trace-level=phase --trace-mode=stream

# Полная детализация (парсер + внутренности sema) в файл
surge diag file.sg --trace=trace.log --trace-level=debug

# С heartbeat (обнаружение зависаний)
surge diag file.sg --trace=trace.log --trace-level=debug --trace-heartbeat=1s
```

---

## Флаги

Глобальные флаги (см. `cmd/surge/main.go`):

- `--trace=<path>`: файл вывода (`-` для stderr, пусто для отключения)
- `--trace-level=off|error|phase|detail|debug`
- `--trace-mode=stream|ring|both` (по умолчанию: `ring`)
- `--trace-format=auto|text|ndjson|chrome`
- `--trace-ring-size=<n>` (по умолчанию: 4096)
- `--trace-heartbeat=<duration>` (0 отключает)

**Авто-поведение:**

- Если `--trace` — это путь к файлу и `--trace-mode=ring`, режим
  автоматически переключается на **stream**. Чтобы принудительно включить ring, явно установите
  `--trace-mode=ring`.
- `--trace-format=auto` определяет формат по расширению:
  - `.ndjson` => NDJSON
  - `.json` или `.chrome.json` => Chrome trace
  - иначе => text

---

## Уровни трассировки

| Уровень | Выдает | Примечания |
|-------|-------|-------|
| `off` | ничего | трассировка отключена |
| `error` | нет спанов (зарезервировано) | только heartbeat + инфраструктура краш-дампов |
| `phase` | спаны драйвера + проходов | высокоуровневый пайплайн |
| `detail` | + спаны модулей | разрешение модулей + граф |
| `debug` | + спаны узлов | парсер + внутренности sema |

`error` в настоящее время не выдает спаны; используйте `phase` или выше для реального вывода.

---

## Режимы трассировки

### Stream (Поток)

Пишет события немедленно в вывод.

```bash
surge diag file.sg --trace=trace.log --trace-level=detail --trace-mode=stream
```

### Ring (Кольцо, по умолчанию)

Хранит последние N событий в памяти (кольцевой буфер). Вывод не пишется,
если вы явно не сдампите его.

```bash
surge diag file.sg --trace-level=detail --trace-mode=ring
```

Если вы установите `--trace` при принудительном режиме ring, кольцевой буфер **дампится при
панике или SIGINT** в:

```
<path>.panic.trace
<path>.interrupt.trace
```

Формат дампа всегда **text**.

### Both (Оба)

Отправляет события и в поток, и в кольцо:

```bash
surge diag file.sg --trace=trace.log --trace-level=debug --trace-mode=both
```

---

## Форматы вывода

### Text (читаемый человеком)

Формат: `[seq NNNNNN] <indent><event> name (detail) {extra=...}`

Пример:

```
[seq      1] → diagnose
[seq      2]   → tokenize
[seq      3]   ← tokenize (diags=0)
[seq      4] → parse
[seq      5] ← parse (items=12)
[seq      6] • parse_items_progress (item=100)
[seq      7] ♡ heartbeat (#1)
```

Легенда:

- `→` начало спана
- `←` конец спана
- `•` точечное событие
- `♡` heartbeat

Отступ — один уровень, когда существует родительский спан.

### NDJSON

```bash
surge diag file.sg --trace=trace.ndjson --trace-level=debug --trace-format=ndjson
```

Каждая строка — объект JSON:

```json
{"time":"2025-12-05T12:00:00.123456Z","seq":1,"kind":"begin","scope":"pass","span_id":42,"parent_id":0,"gid":1,"name":"parse"}
```

Поля:

- `time`, `seq`, `kind`, `scope`
- `span_id`, `parent_id`, `gid`
- `name`, `detail`, `extra`

### Chrome Trace

```bash
surge diag file.sg --trace=trace.json --trace-level=detail --trace-format=chrome
```

Откройте `chrome://tracing` и загрузите JSON файл. Писатель потока производит
массив `traceEvents`, совместимый с просмотрщиком трассировки Chrome.

---

## Heartbeat (Пульс)

`--trace-heartbeat=1s` выдает периодические события `heartbeat`. Это полезно для
идентификации зависаний: пульс продолжается, пока работа стоит.

Пример:

```
[seq      1] → parse
[seq      2] ♡ heartbeat (#1)
[seq      3] ♡ heartbeat (#2)
# нет новых спанов -> вероятно зависание в parse
```

---

## Инструментированные компоненты (v1)

Общие спаны включают:

- Фазы драйвера: `diagnose`, `load_file`, `tokenize`, `parse`, `symbols`, `sema`
- Граф модулей: `parse_module_dir`, `analyze_dependency`, `process_module`
- Узлы парсера (debug): `parse_items`, `parse_block`, `parse_binary_expr`, `parse_postfix_expr`
- Внутренности Sema (debug): `sema_check`, `walk_item`, `walk_stmt`, `type_expr`,
  `call_result_type`, `check_contract_satisfaction`, `methods_for_type`
- Анализ HIR (когда HIR строится): `hir_build_borrow_graph`, `hir_build_move_plan`

---

## Заметки о производительности

- `phase` имеет очень низкие накладные расходы и безопасен для регулярного использования.
- `debug` может быть дорогим (спаны парсера + sema на каждый узел).
- `ring` уменьшает накладные расходы на I/O, но не пишет вывод, если не сдамплен.

---

## Связанные флаги трассировки

Отдельно от трассировки компилятора:

- `--runtime-trace=<file>`: трассировка рантайма Go
- `surge run --vm-trace`: трассировка выполнения VM

Они **не** являются частью потока трассировки компилятора.