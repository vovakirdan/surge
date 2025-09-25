#!/bin/bash
# Скрипт для автоматического принятия всех новых snapshots

cd "$(dirname "$0")"

echo "Принимаем все новые snapshots..."

# Находим все файлы .snap.new и переименовываем их в .snap
find tests/snapshots -name "*.snap.new" -type f | while read -r file; do
    new_name="${file%.snap.new}.snap"
    echo "Принимаем: $file -> $new_name"
    mv "$file" "$new_name"
done

echo "Все snapshots приняты!"
echo "Запускаем тесты для проверки..."
cargo test
