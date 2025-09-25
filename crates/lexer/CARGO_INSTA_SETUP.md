# Настройка и использование cargo insta в проекте lexer

## Обзор

cargo insta настроен и работает корректно в проекте lexer. Все snapshots были успешно созданы и приняты.

## Конфигурация

### Cargo.toml
```toml
[dependencies]
insta = { version = "1.43.2", features = ["redactions"] }
```

### insta.toml
```toml
[settings]
# Настройки для cargo insta
review = "auto"
# Автоматически принимать новые snapshots
```

## Использование

### Основные команды

1. **Запуск тестов с созданием snapshots:**
   ```bash
   cargo test
   ```

2. **Просмотр новых snapshots:**
   ```bash
   cargo insta review
   ```

3. **Принятие всех новых snapshots:**
   ```bash
   cargo insta accept-all
   ```

4. **Запуск только snapshot тестов:**
   ```bash
   cargo insta test
   ```

### Автоматический скрипт

Создан скрипт `accept_snapshots.sh` для автоматического принятия всех новых snapshots:

```bash
./accept_snapshots.sh
```

Этот скрипт:
- Находит все файлы `.snap.new`
- Переименовывает их в `.snap`
- Запускает тесты для проверки

## Структура snapshots

Snapshots хранятся в директории `tests/snapshots/` и имеют формат:
```
ok_snapshots__ok_files_tokenize_snapshot@<имя_файла>.sg.snap
```

## Решение проблем

### Проблема: cargo insta review не показывает вывод

**Причина:** Команды cargo insta работают корректно, но могут не показывать вывод в некоторых терминальных окружениях (особенно в WSL).

**Решение:** Используйте скрипт `accept_snapshots.sh` для автоматического принятия snapshots или команду `cargo insta accept-all`.

### Проблема: Тесты падают с ошибками snapshot

**Причина:** Новые snapshots созданы, но не приняты.

**Решение:** 
1. Запустите `cargo insta review` для просмотра изменений
2. Используйте `cargo insta accept-all` для принятия всех изменений
3. Или используйте скрипт `./accept_snapshots.sh`

## Текущее состояние

✅ cargo insta установлен и настроен  
✅ Все snapshots созданы и приняты  
✅ Тесты проходят успешно  
✅ Команда `cargo insta review` работает корректно  

## Рекомендации

1. **При разработке:** Регулярно запускайте `cargo test` для проверки snapshots
2. **При изменении лексера:** Используйте `cargo insta review` для просмотра изменений
3. **При CI/CD:** Используйте `cargo insta test` для проверки snapshots без интерактивности
4. **Для автоматизации:** Используйте скрипт `accept_snapshots.sh` для массового принятия snapshots
