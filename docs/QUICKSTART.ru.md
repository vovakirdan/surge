# Быстрый старт

Этот документ является практическим введением в Surge.

Это не языковое руководство.
Это не спецификация.
Это не галерея демонстраций.

Это небольшая книга с **запускаемыми примерами**, которые показывают, как писать реальные программы на Surge и что ожидать от языка.

Копируйте код. Запускайте его. Изменяйте его. Ломайте его. Исправляйте его.

---

## 1. Ваша первая программа

Давайте начнём с самой маленькой возможной программы на Surge.

```surge
@entrypoint
fn main() {
    print("Hello, World!");
}
```

Сохраните это как `hello.sg` и запустите:

```bash
surge run hello.sg
```

Или скомпилируйте в бинарь:

```bash
surge build hello.sg
./target/debug/hello
```

### Что такое `@entrypoint`?

`@entrypoint` отмечает **функцию, с которой начинается выполнение программы**.

* Имя функции **не обязано** быть `main`
* Должна быть **ровно одна** entrypoint
* Это не магия — компилятор просто генерирует небольшой обёртку и вызывает вашу функцию

Подумайте о `@entrypoint` как о:

> «Это где точно начинается моя программа.»

---

## 2. Переменные, функции и управление потоком

Surge выглядит знакомым, если вы пришли из C, Go или Rust.

```surge
fn add(a: int, b: int) -> int {
    return a + b;
}

@entrypoint
fn main() {
    let x: int = 10;
    let y: int = 32;

    let result = add(x, y);
    print(result to string); // да, что-то конвертируется В (to) целевой тип. Не как (что-то). Не из (чего то). Выражение разворачивается в вызов метода __to(result, string).
}
```

Mutable переменные должны быть явно отмечены:

```surge
let mut sum: int = 0;

for i in 0..10 {
    sum = sum + i;
}

print(sum to string);
```

Управление потоком прямолинейно:

```surge
if sum > 10 {
    print("big");
} else {
    print("small");
}
```

Нет скрытых возвратов.
`return` всегда явно.
Так что это "по Rust-стилю":

```rust
fn foo() -> int {
    let x = 42;
    x
}
```

А вот так — по Surge-стилю:
```surge
fn foo() -> int {
    let x = 42;
    return x;
}
```

---

## 3. Типы, которые вы будете использовать всегда

Вам не нужно учить много типов, чтобы бы строить реальные программы.

Общие встроенные типы:

* `int`, `uint`, `float`
* `bool`
* `string`
* arrays: `T[]`

И фиксированные размерные типы:

* `int8`, `int16`, `int32`, `int64`
* `uint8`, `uint16`, `uint32`, `uint64`
* `float16`, `float32`, `float64`

Пример:

```surge
@entrypoint
fn main() {
    let numbers: int[] = [1, 2, 3, 4];

    print(len(numbers) to string);
    print(numbers[0] to string);

    let text: string = "hello";
    print(len(text) to string);
}
```

Массивы индексируются с нуля.
Строки — это UTF-8 и индексируются **кодовыми точками**, а не байтами.

Это значит, что развернув строку вы получите фразу наоборот:
```surge
let text: string = "hello";
print(text.reverse()); // "olleh"
```

Как и ожидалось.

---

## 4. Владение и заимствование (в одном абзаце)

Surge **не имеет сборщика мусора**.
Вместо этого он использует явное владение и заимствование.

Вы будете видеть три формы очень часто:

* `T` / `own T` — владеющее значение
* `&T` — общее заимствование (только для чтения)
* `&mut T` — эксклюзивное мутабельное заимствование

Да, я тоже сначала путался, но всё проще, чем кажется.

### Распространённая ошибка

```surge
fn push_value(xs: int[], value: int) {
    xs.push(value);
}
```

Это выглядит хорошо, но **не компилируется**.

Вы можете увидеть ошибку типа:

```
error: cannot mutate borrowed value (SEM3022)
help: consider taking '&mut int[]'
```

Почему?
Потому что `xs` передаётся **по значению**, и мутация требует эксклюзивного доступа.

### Правильная версия

```surge
fn push_value(xs: &mut int[], value: int) {
    xs.push(value);
}

@entrypoint
fn main() {
    let mut data: int[] = [1, 2, 3];
    push_value(&mut data, 4);
    print(len(data) to string);
}
```

Правила:

* Если функция **читает**, используйте `&T`
* Если функция **мутирует**, используйте `&mut T`
* Если функция **принимает владение**, используйте `own T`

Компилятор строго соблюдает эти правила.
Благодаря этому многие ошибки никогда не доходят до runtime.

---

## 5. Структуры и простая модель данных

Вы определяете данные используя `type`.

```surge
type User = {
    name: string,
    age: int,
};

fn birthday(user: &mut User) {
    user.age = user.age + 1;
}

@entrypoint
fn main() {
    let mut u: User = { name = "Alice", age = 30 };
    birthday(&mut u);
    print(u.age to string);
}
```

Структуры — это просто данные.
Поведение живёт в функциях.

Но конечно же вы можете определять методы на структурах:

```surge
type User = {
    name: string,
    age: int,
}

extern<User> {
    fn birthday(self: &User) {
        self.age = self.age + 1;
    }
}

@entrypoint
fn main() {
    let mut u: User = { name = "Alice", age = 30 };
    u.birthday();
    print(u.age to string);
}
```

Это будет развернуто в простой вызов функции. Потому что я люблю функции.

---

## 6. Option — когда что-то может не существовать

Surge не имеет `null`. Не имеет `None`. Нет `void`. Сначала казалось, что это плохо. Но так проще, поверьте.
Если что-то может не существовать, используйте `Option<T>` (краткая форма: `T?`).

```surge
fn head(xs: int[]) -> int? { // здесь возвращаемый объект может не быть
    if len(xs) == 0 {
        return nothing;
    }
    return xs[0];
}
```

Обработка `Option` явная:

```surge
@entrypoint
fn main() {
    let xs: int[] = [];

    let value = head(xs);

    compare value {
        Some(v) => print(v to string);
        nothing => print("empty");
    }
}
```

Нет сюрпризов.
Нет скрытых проверок на null.

---

## 7. Erring — ошибки — это значения

Surge не использует исключения.

Ошибки — это обычные значения типа `Erring<T, Error>` (краткая форма: `T!`).

```surge
fn parse_int(s: string) -> int! {
    if s == "42" {
        return 42;
    }

    let err: Error = {
        message = "not a number",
        code = 1,
    };

    return err;
}
```

Обработка ошибок:

```surge
@entrypoint
fn main() {
    let result = parse_int("hello");

    compare result {
        Success(v) => print(v to string);
        err => print("error: " + err.message);
    }
}
```

Нет `try`.
Нет `catch`.
Тип говорит вам, что может сломаться.

Всё, что не предусмотрели - паникует.

---

## 8. Pattern matching with `compare`

`compare` — это конструкция pattern matching в Surge.

Она работает с:

* `Option`
* `Erring`
* tagged unions
* простыми условиями

Пример:

```surge
fn describe(x: int?) -> string {
    return compare x {
        Some(v) if v > 0 => "positive";
        Some(v) => "non-positive";
        nothing => "missing";
    }
}
```

`compare` является исчерпывающим для тегированных типов.
Если вы забыли один из случаев, компилятор вам скажет.

---

## 9. Entrypoint с аргументами командной строки

`@entrypoint` может парсить аргументы для вас.

```surge
@entrypoint("argv")
fn main(name: string, times: int = 1) {
    for i in 0..times {
        print("Hello " + name);
    }
}
```

Запустите его так:

```bash
surge run greet.sg -- Alice 3
```

### Важное замечание

Это **просто синтаксический сахар**.

Концептуально, компилятор:

* читает `argv`
* парсит значения
* вызывает вашу функцию

Что-то вроде (псевдокод):

```
__surge_start:
    let args = argv();
    let name = args[0];
    let times = args[1];
    main(name, times);
```

Вы можете всегда писать логику парсинга самостоятельно, если хотите.

---

## 10. Async: первое знакомство

Async в Surge является явным и структурированным.

```surge
async fn fetch_data() -> string {
    return "data";
}

@entrypoint
fn main() {
    let t = fetch_data();

    compare t.await() {
        Success(v) => print(v);
        Cancelled() => print("cancelled");
    }
}
```

`async fn` возвращает `Task<T>`.
Вызывая `.await()`, вы ожидаете его.

---

## 11. Запуск задач

Вы можете запускать задачи параллельно используя `spawn`.

```surge
async fn work(id: int) -> int {
    return id * 2;
}

@entrypoint
fn main() {
    let t1 = spawn work(1);
    let t2 = spawn work(2);

    let r1 = t1.await();
    let r2 = t2.await();

    print("done");
}
```

Важные свойства:

* Задачи **не выходят за пределы** своей области видимости
* Вы должны ожидать их или возвращать их
* Компилятор строго соблюдает эти правила

Это называется **структурированной конкурентностью**.
Да, это конечный автомат.

---

## 12. Каналы

Каналы позволяют задачам взаимодействовать.

```surge
async fn producer(ch: &Channel<int>) {
    for i in 0..5 {
        ch.send(i);
    }
    ch.close();
}

async fn consumer(ch: &Channel<int>) {
    while true {
        let v = ch.recv();
        compare v {
            Some(x) => print(x to string);
            nothing => return;
        }
    }
}

@entrypoint
fn main() {
    let ch = make_channel<int>(2);

    spawn producer(&ch);
    spawn consumer(&ch);
}
```

Каналы типизированы.
Отправка и получение являются явными точками приостановки.

---

## 13. Собираем всё вместе

Вот небольшая программа, которая объединяет несколько идей:

```surge
async fn parse_and_send(ch: &Channel<int>, text: string) {
    let r = parse_int(text);

    compare r {
        Success(v) => ch.send(v);
        err => print("skip: " + err.message);
    }
}

@entrypoint("argv")
fn main(values: string[]) {
    let ch = make_channel<int>(4);

    async {
        for v in values {
            spawn parse_and_send(&ch, v);
        }
        ch.close();
    };

    let mut sum: int = 0;

    while true {
        let v = ch.recv();
        compare v {
            Some(x) => sum = sum + x;
            nothing => break;
        }
    }

    print("sum = " + (sum to string));
}
```

Эта программа показывает:

* аргументы entrypoint
* обработку ошибок
* асинхронные задачи
* каналы
* владение и заимствование

---

## 14. Что читать дальше

Если вы хотите изучить подробнее:

* [`docs/LANGUAGE.md`](LANGUAGE.ru.md) — обзор языка и синтаксиса
* [`docs/CONCURRENCY.md`](CONCURRENCY.ru.md) — асинхронность, задачи, каналы
* [`docs/DIRECTIVES.md`](DIRECTIVES.ru.md) — тесты и сценарии в коде
* [`showcases/`](../showcases/) — примеры побольше

Этот быстрый старт намеренно поверхностный.
Его цель — помочь вам **начать писать**, а не объяснять всё.

---

Удачного кодинга!
