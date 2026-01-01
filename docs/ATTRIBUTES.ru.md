# Атрибуты языка Surge
[English](ATTRIBUTES.md) | [Russian](ATTRIBUTES.ru.md)

Атрибуты — это декларативные аннотации, которые добавляют дополнительные ограничения или метаданные к
функциям, типам, полям, параметрам, блокам и инструкциям (statements). Компилятор
проверяет цели применения, конфликты и аргументы. Некоторые атрибуты **парсятся**, но
пока не влияют на семантику; они отмечены явно.

---

## Синтаксис

```sg
@attribute
fn example() { return nothing; }

@attribute("arg")
type Data = { value: int };

@a
@b
fn multiple() { return nothing; }
```

Заметки:
- Атрибуты указываются непосредственно перед объявлением, которое они модифицируют.
- Неизвестные атрибуты считаются ошибкой.
- Атрибуты инструкций: разрешен только `@drop expr;` (без аргументов).
- Атрибуты async-блоков: принимается только `@failfast`.
- Аргументы атрибутов должны быть литералами (строка или целое число, как требуется).

---

## Индекс атрибутов (текущее поведение)

Легенда статусов:
- **Enforced**: влияет на семантику или генерирует диагностику.
- **Validated**: проверяются аргументы/цели; пока нет семантического эффекта.
- **Parsed**: принимается, но нет проверок или поведения помимо проверки цели.

| Атрибут | Цели | Аргументы | Статус | Примечания |
| --- | --- | --- | --- | --- |
| `@allow_to` | fn, param | нет | Enforced | Разрешает неявное преобразование `__to`. |
| `@backend` | fn | string | Validated | Предупреждает о неизвестных целях; нет эффекта на кодогенерацию. |
| `@copy` | type | нет | Enforced | Все поля/члены должны быть Copy. |
| `@deprecated` | fn, type, field, let, const | опц. string | Enforced | Выдает предупреждения при использовании. |
| `@drop` | stmt | нет | Enforced | Явная точка сброса/окончания заимствования. |
| `@entrypoint` | fn | опц. string | Enforced | Точка входа в программу. |
| `@failfast` | async fn, async block | нет | Enforced | Отмена структурированной конкурентности. |
| `@guarded_by` | field | string | Enforced | Требует удержания блокировки для доступа. |
| `@hidden` | fn, type, field, let, const | нет | Enforced (top-level) | На уровне полей только парсится. |
| `@intrinsic` | fn, type | нет | Enforced | Только объявление; ограничения тела типа. |
| `@noinherit` | type, field | нет | Enforced | Предотвращает наследование. |
| `@nosend` | type | нет | Enforced | Запрещает пересечение границ задач. |
| `@nonblocking` | fn | нет | Enforced | Запрещает блокирующие вызовы. |
| `@overload` | fn | нет | Enforced | Добавляет новую сигнатуру. |
| `@override` | fn | нет | Enforced | Заменяет существующую сигнатуру. |
| `@packed` | type, field | нет | Enforced (type) | На уровне полей не влияет на layout. |
| `@align` | type, field | int pow2 | Enforced | Переопределение выравнивания layout. |
| `@raii` | type | нет | Parsed | Зарезервировано. |
| `@arena` | type, field, param | string | Parsed | Зарезервировано. |
| `@shared` | type, field | нет | Parsed | Зарезервировано. |
| `@weak` | field | нет | Parsed | Зарезервировано. |
| `@atomic` | field | нет | Enforced | Ограничения типа + правила доступа. |
| `@readonly` | field | нет | Enforced | Запрещает запись после инициализации. |
| `@requires_lock` | fn | string | Enforced | Вызывающий должен держать блокировку. |
| `@acquires_lock` | fn | string | Enforced | Вызываемая функция захватывает блокировку. |
| `@releases_lock` | fn | string | Enforced | Вызываемая функция освобождает блокировку. |
| `@waits_on` | fn | string | Enforced | Помечает потенциальную блокировку. |
| `@send` | type | нет | Enforced | Композиция полей должна быть sendable. |
| `@sealed` | type | нет | Enforced | Нельзя расширять. |
| `@pure` | fn | нет | Parsed | Проверки чистоты пока отсутствуют. |

---

## Атрибуты функций

### `@overload`

Добавляет новую сигнатуру для существующего имени функции.

Правила:
- Первое объявление **не должно** использовать `@overload`.
- `@overload` должен вводить **другую сигнатуру**.
- Если сигнатура идентична, используйте `@override`.

```sg
fn parse(x: int) -> int { return x; }

@overload
fn parse(x: string) -> int { return x.to_int(); }
```

### `@override`

Заменяет существующее объявление с той же сигнатурой.

Правила:
- Ранее соответствующее объявление уже должно существовать.
- Сигнатура должна совпадать точно.
- Нельзя понижать видимость (`pub` должен быть сохранен при переопределении публичного).
- Нельзя переопределять встроенные (builtin) функции.
- Нельзя комбинировать с `@overload` или `@intrinsic`.

```sg
fn encode(buf: &byte[]) -> uint; // forward decl

@override
fn encode(buf: &byte[]) -> uint { return 0:uint; }
```

### `@intrinsic` (функции)

Объявляет функцию, предоставляемую компилятором/рантаймом.

Правила:
- Должна быть **только объявлением** (без тела).
- Нельзя комбинировать с `@override` или `@entrypoint`.
- Разрешено в любом модуле, но бэкенды реализуют только известные intrinsics.
- Разрешает сырые указатели в сигнатурах.

```sg
@intrinsic fn rt_alloc(size: uint) -> *byte;
```

### `@entrypoint`

Помечает точку входа в программу.

Режимы:
- Нет режима: `@entrypoint` требует, чтобы все параметры имели значения по умолчанию.
- `@entrypoint("argv")`: парсинг позиционных аргументов через `T.from_str(&string)`.
- `@entrypoint("stdin")`: парсинг одного параметра из stdin.
- `"env"` и `"config"` зарезервированы (`FutEntrypointModeEnv` / `FutEntrypointModeConfig`).

Тип возврата:
- `nothing` или `int`, или любой тип, реализующий `ExitCode<T>` (`__to(self, int) -> int`).
- `Option<T>` и `Erring<T, E>` реализуют это преобразование по умолчанию.

Парсинг параметров:
- `"argv"` требует, чтобы каждый тип параметра (без значения по умолчанию) реализовывал `FromArgv<T>`.
- `"stdin"` требует единственного типа параметра, реализующего `FromStdin<T>`.

Контракты объявлены в `core/entrypoint.sg`.

Поведение рантайма (v1):
- `argv`: отсутствие обязательного аргумента завершает с кодом 1; ошибки парсинга вызывают `exit(err)`.
- `stdin`: поддерживается только один параметр; несколько параметров завершают с кодом 7001.

```sg
@entrypoint("argv")
fn main(count: int, name: string = "guest") -> int {
    return 0;
}
```

### `@allow_to`

Разрешает неявное преобразование `__to`, когда типы аргументов не совпадают точно.

- На **функции**: применяется ко всем параметрам.
- На **параметре**: применяется только к этому параметру.

```sg
fn takes_string(@allow_to s: string) { print(s); }
```

### `@nonblocking` и `@waits_on`

- `@nonblocking` запрещает блокирующие вызовы.
- `@waits_on("field")` помечает функцию как потенциально блокирующую.
- Они **конфликтуют**, если используются вместе.

Блокирующие методы, проверяемые сегодня:
- `Mutex.lock`
- `RwLock.read_lock` / `RwLock.write_lock`
- `Condition.wait`
- `Semaphore.acquire`
- `Channel.send` / `Channel.recv` / `Channel.close`

`@waits_on` требует имя поля типа `Condition` или `Semaphore`.

### Атрибуты контрактов блокировок

`@requires_lock`, `@acquires_lock` и `@releases_lock` ссылаются на поле блокировки
в типе получателя (обычно `self`). Они управляют межпроцедурными проверками блокировок.

```sg
type Counter = { lock: Mutex, value: int };

extern<Counter> {
    @requires_lock("lock")
    fn get(self: &Counter) -> int { return self.value; }
}
```

### `@backend`

Валидирует строку цели выполнения (известные: `cpu`, `gpu`, `tpu`, `wasm`,
`native`). Неизвестные цели вызывают предупреждение. Пока нет эффекта на кодогенерацию.

### `@failfast`

- Разрешено на `async fn` и блоках `@failfast async { ... }`.
- Отменяет братские задачи (sibling tasks) в той же области видимости, когда одна из них отменяется.

### `@pure`

Парсится, но пока не проверяется (not enforced). Проверки чистоты сегодня не выполняются.

### `@deprecated`

Выдает предупреждение при каждом использовании элемента. Опциональное строковое сообщение.

```sg
@deprecated("use new_api")
fn old_api() { return nothing; }
```

### `@hidden`

На элементах верхнего уровня: делает символ приватным для файла и исключает его из экспорта.
Использование `pub` вместе с `@hidden` вызывает предупреждение.

---

## Атрибуты типов

### `@intrinsic` (типы)

Объявляет тип, предоставляемый компилятором/рантаймом.

Правила:
- Тип должен быть пустой структурой или содержать только одно поле `__opaque`.
- Полный layout разрешен только в `core/intrinsics.sg` или `core_stdlib/intrinsics.sg`.
- Разрешает поля с сырыми указателями внутри типа.

```sg
@intrinsic
pub type Task<T> = { __opaque: int };
```

### `@packed` и `@align`

- `@packed` убирает padding между полями для размещения структуры.
- `@align(N)` переопределяет выравнивание; `N` должно быть положительной степенью двойки.
- `@packed` конфликтовать с `@align` на одном объявлении.

### `@send` / `@nosend`

- `@send` требует, чтобы все поля были sendable (рекурсивно).
- `@nosend` запрещает пересечение границ задач.
- Они конфликтуют друг с другом.

### `@copy`

Помечает структуру или объединение как Copy, если все поля/члены являются Copy. Циклы
отвергаются. Если валидно, тип становится способным к копированию (Copy-capable).

### `@sealed` / `@noinherit`

- `@sealed`: нельзя расширять через наследование или `extern<T>`.
- `@noinherit`: предотвращает использование типа в качестве базового.

### `@raii`, `@arena`, `@shared`

Только парсятся; пока нет семантических проверок или поведения рантайма.

---

## Атрибуты полей

### `@readonly`

Поле нельзя записывать после инициализации.

### `@atomic`

- Тип поля должен быть `int`, `uint`, `bool` или `*T`.
- Прямое чтение/запись запрещены; используйте атомарные intrinsics через взятие адреса.

### `@guarded_by("lock")`

Доступ требует удержания именованной блокировки. Чтение разрешает read/write блокировки; запись
требует мьютекса или write lock.

### `@align` / `@packed`

`@align` применяется; `@packed` принимается, но в данный момент не имеет эффекта на layout
на уровне поля.

### `@noinherit`

Поле не наследуется производными типами.

### `@deprecated` / `@hidden`

Парсятся для полей; только `@deprecated` в настоящее время влияет на диагностику. `@hidden`
на уровне полей зарезервирован (проверки доступа пока отсутствуют).

### `@weak`, `@shared`, `@arena`

Только парсятся; пока нет семантического эффекта.

---

## Атрибуты параметров

### `@allow_to`

См. описание атрибута функции; разрешает неявное `__to` для этого параметра.

### `@arena`

Только парсится; пока нет семантического эффекта.

---

## Атрибут инструкции (Statement Attribute)

### `@drop`

Явная точка сброса/окончания заимствования. Валидно только как `@drop binding;` без
аргументов. Целью должно быть имя привязки (binding name).

```sg
let r = &mut value;
@drop r; // заканчивает заимствование досрочно
```

---

## Сводка конфликтов и валидации

- `@packed` + `@align` (одно объявление)
- `@send` + `@nosend`
- `@nonblocking` + `@waits_on`
- `@overload` + `@override`
- `@intrinsic` + `@override` или `@entrypoint`
- `@failfast` требует async контекста

---

## Диагностика (выборочно)

- `SemaAttrPackedAlign` `@packed` конфликтует с `@align`
- `SemaAttrSendNosend` `@send` конфликтует с `@nosend`
- `SemaAttrNonblockingWaitsOn` `@nonblocking` конфликтует с `@waits_on`
- `SemaAttrAlignNotPowerOfTwo` `@align` не степень двойки
- `SemaAttrBackendUnknown` `@backend` неизвестная цель (предупреждение)
- `SemaAttrGuardedByNotField` / `SemaAttrGuardedByNotLock` `@guarded_by` неверное поле/тип
- `SemaLockGuardedByViolation` `@guarded_by` доступ без блокировки
- `SemaLockNonblockingCallsWait` `@nonblocking` вызывает блокирующую операцию
- `SemaAttrWaitsOnNotCondition` `@waits_on` поле должно быть Condition/Semaphore
- `SemaAttrAtomicInvalidType` `@atomic` неверный тип поля
- `SemaAtomicDirectAccess` `@atomic` прямой доступ
- `SemaAttrCopyNonCopyField` / `SemaAttrCopyCyclicDep` ошибки валидации `@copy`
- `SemaEntrypointModeInvalid` / `SemaEntrypointNoModeRequiresNoArgs` / `SemaEntrypointReturnNotConvertible` / `SemaEntrypointParamNoFromArgv` / `SemaEntrypointParamNoFromStdin` валидация entrypoint
- `FutEntrypointModeEnv` / `FutEntrypointModeConfig` зарезервированные режимы entrypoint

См. `internal/diag/codes.go` для полного списка.