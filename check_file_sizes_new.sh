#!/bin/bash

# Скрипт для проверки размера файлов в директории
# Логика:
# 1. Считаем среднее LOC по .go файлам (без тестов, без комментариев)
# 2. Порог = max(среднее, BASE_THRESHOLD)
# 3. Репортим файлы с отклонением > DEVIATION_THRESHOLD%

# Цветовые коды для вывода
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Настройки по умолчанию
BASE_THRESHOLD=675        # Минимальный порог строк
DEVIATION_THRESHOLD=10    # Допустимое отклонение в %
MIN_LINES_FOR_AVG=100     # Минимум строк для включения в расчёт среднего

# count_code_lines подсчитывает строки кода в файле, исключая однострочные комментарии (//) и пустые строки
count_code_lines() {
    local file=$1
    grep -v '^\s*//' "$file" 2>/dev/null | grep -v '^\s*$' | wc -l
}

# is_test_file проверяет, является ли файл тестовым
is_test_file() {
    local file=$1
    local basename=$(basename "$file")
    [[ $basename == *_test.go ]]
}

# calculate_average вычисляет среднее количество строк кода по .go файлам >= MIN_LINES_FOR_AVG (без тестов)
# Возвращает: среднее, кол-во файлов в расчёте, всего строк, всего файлов
calculate_average() {
    local dir=$1
    local total_lines=0
    local file_count=0
    local all_files=0

    while IFS= read -r -d '' file; do
        if ! is_test_file "$file"; then
            local lines=$(count_code_lines "$file")
            if [ "$lines" -gt 0 ]; then
                all_files=$((all_files + 1))
                # Включаем в расчёт среднего только файлы >= MIN_LINES_FOR_AVG
                if [ "$lines" -ge "$MIN_LINES_FOR_AVG" ]; then
                    total_lines=$((total_lines + lines))
                    file_count=$((file_count + 1))
                fi
            fi
        fi
    done < <(find "$dir" -name "*.go" -type f -print0 2>/dev/null)

    if [ $file_count -gt 0 ]; then
        echo "$((total_lines / file_count)) $file_count $total_lines $all_files"
    else
        echo "0 0 0 $all_files"
    fi
}

# check_directory выполняет основную проверку директории
check_directory() {
    local dir=${1:-.}

    echo "Анализ .go файлов в директории: $dir"
    echo "=================================================="

    # Первый проход: вычисляем среднее
    local result=$(calculate_average "$dir")
    local average=$(echo "$result" | cut -d' ' -f1)
    local files_in_avg=$(echo "$result" | cut -d' ' -f2)
    local total_lines=$(echo "$result" | cut -d' ' -f3)
    local all_files=$(echo "$result" | cut -d' ' -f4)

    if [ "$all_files" -eq 0 ]; then
        echo "Не найдено .go файлов (без тестов)"
        exit 0
    fi

    # Определяем порог: max(average, BASE_THRESHOLD)
    local threshold=$BASE_THRESHOLD
    local threshold_source="базовый"
    if [ "$files_in_avg" -gt 0 ] && [ "$average" -gt "$BASE_THRESHOLD" ]; then
        threshold=$average
        threshold_source="среднее"
    fi

    echo "Всего файлов: $all_files"
    echo "Файлов >= $MIN_LINES_FOR_AVG строк: $files_in_avg"
    echo "Среднее LOC (по файлам >= $MIN_LINES_FOR_AVG): $average"
    echo "Используемый порог: $threshold ($threshold_source)"
    echo "Допустимое отклонение: ${DEVIATION_THRESHOLD}%"
    echo ""

    # Второй проход: проверяем файлы на превышение
    local bad_files=0
    local bad_output=""

    while IFS= read -r -d '' file; do
        if ! is_test_file "$file"; then
            local lines=$(count_code_lines "$file")
            if [ "$lines" -gt 0 ]; then
                # Вычисляем отклонение: (lines/threshold - 1) * 100
                # Используем bc для точных вычислений с плавающей точкой
                local deviation=$(echo "scale=1; ($lines / $threshold - 1) * 100" | bc)
                local deviation_int=$(echo "$deviation" | cut -d'.' -f1)

                # Если отклонение отрицательное или пустое, пропускаем
                if [ -z "$deviation_int" ] || [ "$deviation_int" -lt 0 ] 2>/dev/null; then
                    continue
                fi

                # Проверяем, превышает ли отклонение порог
                local exceeds=$(echo "$deviation > $DEVIATION_THRESHOLD" | bc)
                if [ "$exceeds" -eq 1 ]; then
                    bad_files=$((bad_files + 1))
                    bad_output+=$(printf "%-55s %6d    +%-5s%%  ${RED}BAD${NC}\n" "$file" "$lines" "$deviation")
                    bad_output+=$'\n'
                fi
            fi
        fi
    done < <(find "$dir" -name "*.go" -type f -print0 2>/dev/null)

    # Выводим результаты
    if [ $bad_files -gt 0 ]; then
        echo -e "${RED}Файлы, требующие рефакторинга:${NC}"
        printf "%-55s %6s    %-8s\n" "Файл" "Строки" "Откл."
        echo "--------------------------------------------------"
        echo -e "$bad_output"
        echo "=================================================="
        echo -e "${RED}Найдено файлов с превышением: $bad_files${NC}"
        exit 1
    else
        echo -e "${GREEN}Все файлы в пределах допустимого отклонения!${NC}"
        exit 0
    fi
}

# show_help выводит справку по использованию скрипта
show_help() {
    echo "Использование: $0 [опции] [директория]"
    echo ""
    echo "Проверяет размер .go файлов (без тестов) на основе относительного отклонения."
    echo ""
    echo "Логика:"
    echo "  1. Вычисляет среднее LOC по .go файлам >= MIN_LINES (без тестов, без комментариев)"
    echo "  2. Порог = max(среднее, BASE_THRESHOLD)"
    echo "  3. Репортит файлы с отклонением > DEVIATION_THRESHOLD%"
    echo ""
    echo "Опции:"
    echo "  -h, --help                 - показать эту справку"
    echo "  -b, --base-threshold N     - базовый порог строк (по умолчанию: $BASE_THRESHOLD)"
    echo "  -d, --deviation N          - допустимое отклонение в % (по умолчанию: $DEVIATION_THRESHOLD)"
    echo "  -m, --min-lines N          - мин. строк для расчёта среднего (по умолчанию: $MIN_LINES_FOR_AVG)"
    echo ""
    echo "Примеры:"
    echo "  $0                         # проверить текущую директорию"
    echo "  $0 -b 600                  # использовать базовый порог 600"
    echo "  $0 -d 15                   # допустить отклонение до 15%"
    echo "  $0 -m 50                   # включить файлы >= 50 строк в расчёт среднего"
    echo "  $0 -b 700 -d 5 ./internal  # порог 700, отклонение 5%, директория ./internal"
}

# Парсинг аргументов
while [[ $# -gt 0 ]]; do
    case $1 in
        -h|--help)
            show_help
            exit 0
            ;;
        -b|--base-threshold)
            BASE_THRESHOLD="$2"
            shift 2
            ;;
        -d|--deviation)
            DEVIATION_THRESHOLD="$2"
            shift 2
            ;;
        -m|--min-lines)
            MIN_LINES_FOR_AVG="$2"
            shift 2
            ;;
        -*)
            echo "Неизвестная опция: $1" >&2
            echo "Используйте -h или --help для справки" >&2
            exit 1
            ;;
        *)
            # Это директория
            DIRECTORY="$1"
            shift
            ;;
    esac
done

# Запускаем проверку
check_directory "${DIRECTORY:-.}"
