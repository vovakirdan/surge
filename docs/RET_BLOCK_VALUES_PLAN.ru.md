# План: `ret` для значений блоков

> **Статус:** рабочий план
> **Дата:** 2026-03-27
> **Ветка:** отдельная рабочая ветка, не поверх unrelated изменений

## 1. Что меняем

Цель: убрать неявный возврат значения из `{ ... }` в value-позиции и заменить его явным `ret`.

Принятые решения:

- Короткая форма `pattern => expr` остаётся без изменений.
- Brace-block в value-позиции возвращает значение только через `ret`.
- В первой версии поддерживаются обе формы:
  - `ret;`
  - `ret nothing;`
- `ret;` и `ret nothing;` эквивалентны.
- `return` остаётся только выходом из функции.
- `ret` используется только как выход из block expression, а не из тела функции.
- `pragma`, edition-mode и режимы обратной совместимости не вводятся.
- Старый implicit-tail style не сохраняется как семантика языка.
- Для мест, где старый код, вероятно, рассчитывал на implicit block value, добавляется warning с autofix: вставить `ret`.

## 2. Почему это нужно

Сейчас модель неоднородная:

- parser в value-контексте нормализует `{ ... }` как block value;
- последний `StmtExpr` может неявно становиться значением блока;
- при этом `return` внутри block expression всё ещё означает выход из функции, а не из блока.

Это создаёт двусмысленность:

- imperative block внутри `compare` arm может внезапно начать “возвращать” значение;
- семантика тяжело читается глазами;
- диагностики приходится строить на эвристиках поверх parser normalization;
- код вроде `sigil#1` показывает, что block-as-value и block-as-control-flow сейчас смешаны.

Целевая модель проще:

- `expr` в коротком arm-е даёт значение arm-а;
- `{ ... }` по умолчанию ведёт себя как императивный блок;
- если из него нужно значение, это пишется явно через `ret`.

## 3. Итоговая семантика

После завершения миграции действуют такие правила:

- `compare x { A => expr; B => expr2; }` работает как сейчас.
- `compare x { A => { foo(); ret 1; }; B => { ret 2; }; }` валиден и имеет тип `int`.
- `let x = { foo(); ret 1; };` валиден.
- `let x = { foo(); 1; };` больше не использует implicit tail return.
- `compare x { A => { updated = true; } finally => {} }` трактуется как control-flow block с типом `nothing`.
- `ret;` и `ret nothing;` завершают текущий block expression значением `nothing`.
- `return` в nested block, как и раньше, возвращает из функции.
- `ret` вне block expression запрещён.

### Конкретные примеры

Короткая форма arm не меняется:

```sg
compare x {
    Some(v) => v;
    nothing => 0;
}
```

Brace-block с явным block-return:

```sg
let x = {
    log("before");
    ret 1;
};
```

Brace-block с `nothing`-результатом:

```sg
let x = {
    touch();
    ret;
};
```

`ret nothing;` допустим и эквивалентен `ret;`:

```sg
let x = {
    audit();
    ret nothing;
};
```

Императивный `compare`-arm без значения:

```sg
compare entry {
    VManyString(arr) => {
        arr.push(clone(s));
        updated = true;
    }
    finally => {}
};
```

После завершения миграции этот пример остаётся корректным и трактуется как control-flow block, а не как `bool`-producing arm.

Невалидный legacy-style после финального flip:

```sg
let x = {
    foo();
    1;
};
```

Чтобы код был валиден, нужно:

```sg
let x = {
    foo();
    ret 1;
};
```

Отличие `ret` от `return`:

```sg
fn demo() -> int {
    let x = {
        ret 1;      // завершает только блок
    };
    if x == 1 {
        return 2;   // завершает функцию
    }
    return 0;
}
```

## 4. Не-цели

В этот блок работ не входят:

- новый `pragma` или feature-gate;
- edition/migration mode;
- изменение semantics короткой формы `=> expr`;
- переосмысление `return` как block-return;
- параллельная переделка `if` в полноценный expression feature.

## 5. Инварианты миграции

Эти условия должны сохраняться на каждом этапе:

- Каждая итерация даёт проверяемый промежуточный результат.
- Временные изменения не должны ломать `make check`.
- Golden snapshots обновляются только после стабилизации соответствующего этапа.
- Отдельно проверяются оба репозитория:
  - `surge`
  - `sigil`
- Мы не держим два семантических режима языка.

## 6. Семантические инварианты

Эти правила должны быть истинны в целевой модели языка:

- Короткая форма `=> expr` не меняет semantics в ходе всей миграции.
- `ret` никогда не завершает функцию.
- `return` никогда не рассматривается как block-return.
- `ret;` и `ret nothing;` всегда эквивалентны.
- Brace-block без `ret` не должен приобретать значение только из-за trailing assignment или trailing call.
- Discarded `compare` / `select` / `race` не должны провоцировать type mismatch лишь потому, что внутри arm-ов есть императивные блоки.
- Значение brace-block определяется только явными `ret`-exit paths.
- Если brace-block используется как значение, все reachable `ret`-ветки обязаны быть совместимы по типу.
- Если brace-block используется как значение, но reachable `ret` нет, его результат трактуется как `nothing`.
- Сема, VM и LLVM должны соглашаться: недопустимый block-value code не должен “проходить diag, но падать позже”.

## 7. Этапы работ

### Этап 0. Зафиксировать базовый набор кейсов

Задача этапа: до изменения semantics собрать минимальную acceptance-матрицу и защитить её тестами.

Что делаем:

- Добавляем regression cases на текущие проблемные сценарии:
  - issue `#53`: arm block не должен маскироваться под корректное value-return.
  - `sigil#1`: imperative arm block не должен считаться ошибкой только из-за `updated = true;`.
- Добавляем corpus для будущего `ret`:
  - `let x = { ret 1; };`
  - `let x = { ret; };`
  - `let x = { ret nothing; };`
  - nested block inside `compare`/`select`.
- Явно фиксируем distinction:
  - block used for value;
  - block used for control flow.

Проверка:

- targeted parser/sema tests;
- targeted driver/vm tests;
- `make check`.

Критерий завершения:

- У нас есть список acceptance cases, который покрывает и `#53`, и `sigil#1`, и будущий `ret`.

### Этап 1. Ввести `ret` как новый syntax + IR path

Задача этапа: добавить новый оператор без смены semantics старого кода.

Что делаем:

- Вводим keyword `ret` в lexer/token.
- Добавляем новый AST stmt для block-return.
- Добавляем новый HIR stmt для block-return.
- Добавляем lowering в MIR/CF lowering:
  - `ret expr;` завершает текущий block expression;
  - `ret;` и `ret nothing;` дают `nothing`.
- Добавляем parser support:
  - `ret expr;`
  - `ret;`
  - `ret nothing;`
- Запрещаем `ret` вне expression-block context.

Точки изменений:

- `internal/token/*`
- `internal/ast/stmt.go`
- `internal/parser/*`
- `internal/hir/stmt.go`
- `internal/hir/lower_stmt.go`
- `internal/mir/lower_expr_cf.go`
- `internal/sema/*` для контекста block-result

Проверка:

- parser unit tests;
- sema tests;
- HIR/MIR tests;
- targeted end-to-end tests на `let x = { ret 1; };`.

Критерий завершения:

- `ret` полностью распознаётся и работает end-to-end;
- старый код всё ещё компилируется как раньше;
- `make check` зелёный.

### Этап 2. Добавить warning + autofix для legacy implicit block value

Задача этапа: научить компилятор показывать места, где раньше использовался implicit tail return и где теперь нужен `ret`.

Что делаем:

- Детектируем brace-block в value-позиции, где значение появляется только через legacy implicit tail expr.
- Выдаём warning.
- Даём fix suggestion:
  - вставить `ret ` перед tail expr.
- Не предупреждаем там, где блок используется как control-flow и его корректный результат должен быть `nothing`.
- Отдельно проверяем `compare`, `select`, nested block, `let`, `return compare ...`.

Важно:

- На этом этапе semantics старого кода ещё не flip-ается.
- Цель этапа: собрать полный список мест миграции и сделать их механически исправимыми.

Проверка:

- golden diagnostics/fix snapshots;
- `surge diag .` в `surge`;
- `surge diag .` в `sigil`.

Критерий завершения:

- Компилятор стабильно находит legacy implicit block values;
- autofix безопасен и предсказуем;
- список мест для миграции конечен и понятен.

### Этап 3. Миграция кода на `ret`

Задача этапа: переписать кодовую базу и зависимые репозитории на новую форму до смены semantics.

Что делаем:

- Применяем autofix там, где он корректен.
- Ручные правки там, где нужны более явные изменения.
- Обновляем код в:
  - `surge`
  - `sigil`
- Проверяем, что imperative blocks без `ret` остались именно imperative blocks.

Особенно проверяем:

- `compare` arms с brace-block;
- nested blocks;
- `select`/`race` arms;
- локальные helper-блоки;
- block expressions, возвращающие ссылки, `Option`, `Erring`, tuple.

Проверка:

- `make check`
- `make golden-check`
- `surge diag .` в `surge`
- `surge diag .` в `sigil`

Критерий завершения:

- Внутренний код уже не зависит от implicit block value.

### Этап 4. Финальный flip semantics

Задача этапа: удалить старую семантику implicit block value и оставить только `ret`.

Что делаем:

- Убираем semantic reliance на parser normalization of tail expr as block value.
- Brace-block без `ret` в value-контексте теперь даёт `nothing`.
- Старые кейсы компилируются только если были мигрированы на `ret`.
- Проверяем, что `sigil#1` теперь корректно воспринимается как imperative block, а не как `bool`-producing arm.
- Обновляем docs:
  - `docs/LANGUAGE.md`
  - `docs/LANGUAGE.ru.md`
  - при необходимости follow-up notes

Проверка:

- полный `make check`
- полный `make golden-check`
- контрольный прогон `surge diag .` по `surge`
- контрольный прогон `surge diag .` по `sigil`

Критерий завершения:

- язык больше не имеет implicit block return;
- `ret` является единственным способом вернуть значение из brace-block.

## 8. Что проверяем на каждом этапе

Обязательная матрица проверки:

- `ret 1;`
- `ret;`
- `ret nothing;`
- `compare` с short-form arm
- `compare` с brace-block arm
- `select`/`race` с brace-block result
- nested block inside `let`
- nested block inside `return compare ...`
- imperative block с последним assignment statement
- imperative block с последним call statement
- block expression, возвращающий reference
- block expression, возвращающий union/tag

Дополнительные edge cases:

- nested `ret` внутри блока, который сам находится в `compare` arm
- `ret` в `select` arm result block
- `ret` в `race` arm result block
- `ret` внутри блока, который возвращает `Option<&T>`
- `ret` внутри блока, который возвращает `Erring<T, E>`
- discarded nested `compare` внутри outer discarded `compare`
- imperative block с последним `if` statement без `ret`
- block с unreachable trailing code после `ret`

## 9. Основные риски

### Риск 1. Смешение function return и block return

Если `ret` будет опираться на старый `returnStack`, semantics снова станут двусмысленными.

Решение:

- держать `ret` как отдельный stmt и отдельный lowering path.

### Риск 2. Ложные warning-и

Если warning будет слишком широким, он начнёт стрелять по корректным control-flow blocks.

Решение:

- warning привязывать только к value-context;
- отдельно тестировать `sigil#1`-класс кейсов.

### Риск 3. Частичный flip semantics

Если сменить semantics раньше миграции собственного кода, мы сломаем `surge` и зависимые библиотеки одновременно.

Решение:

- flip только после завершения этапа 3.

## 10. Definition of Done

Работа считается завершённой, когда одновременно выполнены все условия:

- `ret` поддерживает `ret expr;`, `ret;`, `ret nothing;`;
- implicit block value больше не существует как семантика языка;
- `return` остаётся function-return only;
- `surge` проходит `make check` и `make golden-check`;
- `sigil` проходит `surge diag .`;
- документация языка обновлена;
- regression tests закрывают и `#53`, и `sigil#1`.
