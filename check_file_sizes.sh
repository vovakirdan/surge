#!/bin/bash

# Скрипт для проверки размера файлов в директории
# Оценка по количеству строк:
# <=525 +- 50 OK green
# 575 - 675 Yellow acceptable  
# 675+ BAD need refactoring

# Цветовые коды для вывода
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Настройки по умолчанию
EXTENSIONS="go"
EXCLUDE_TESTS=true

# get_file_rating outputs a colored rating for a file based on its line count: "OK" for 575 lines or fewer, "ACCEPTABLE" for 576–675 lines, and "BAD - need refactoring" for more than 675 lines.
get_file_rating() {
    local lines=$1
    local filename=$2
    
    if [ $lines -le 575 ]; then
        echo -e "${GREEN}OK${NC}"
    elif [ $lines -le 675 ]; then
        echo -e "${YELLOW}ACCEPTABLE${NC}"
    else
        echo -e "${RED}BAD - need refactoring${NC}"
    fi
}

# has_allowed_extension determines whether the file's extension is permitted by the comma-separated EXTENSIONS variable (empty EXTENSIONS allows all files) and returns success (0) when allowed, failure (1) otherwise.
has_allowed_extension() {
    local file=$1
    
    # Если EXTENSIONS пустой, разрешаем все файлы
    if [ -z "$EXTENSIONS" ]; then
        return 0
    fi
    
    local ext="${file##*.}"
    
    # Проверяем, есть ли расширение в списке разрешенных
    # Заменяем запятые на пробелы для корректного парсинга
    local extensions_list=$(echo "$EXTENSIONS" | tr ',' ' ')
    for allowed_ext in $extensions_list; do
        if [ "$ext" = "$allowed_ext" ]; then
            return 0
        fi
    done
    return 1
}

# is_test_file checks whether a file path corresponds to a test file by matching common filename patterns: suffix "_test.", prefix "test_", or containing "Test.".
# Returns 0 if the file is recognized as a test file, 1 otherwise.
is_test_file() {
    local file=$1
    local basename=$(basename "$file")
    
    # Проверяем различные паттерны тестовых файлов
    if [[ $basename == *_test.* ]] || [[ $basename == test_* ]] || [[ $basename == *Test.* ]]; then
        return 0
    fi
    return 1
}

# is_text_file checks whether the given path is a regular text file and returns success if it is.
is_text_file() {
    local file=$1
    # Проверяем, что файл существует и не является директорией
    if [ ! -f "$file" ]; then
        return 1
    fi
    
    # Проверяем MIME-тип файла
    if command -v file >/dev/null 2>&1; then
        local mime_type=$(file -b --mime-type "$file")
        if [[ $mime_type == text/* ]]; then
            return 0
        fi
    fi
    
    # Альтернативная проверка: пытаемся прочитать первые несколько байт
    if head -c 1000 "$file" 2>/dev/null | grep -qP '[\x00-\x08\x0E-\x1F\x7F]'; then
        return 1  # Содержит бинарные символы
    fi
    
    return 0
}

# count_effective_lines считает строки для оценки:
# - для go-тестов исключает пустые строки и строки-комментарии, начинающиеся с //
# - для остальных файлов использует обычный подсчет строк
count_effective_lines() {
    local file=$1
    local ext="${file##*.}"

    if [[ "$ext" = "go" ]]; then
        # Для go-файлов считаем только строки с кодом (без пустых и чисто //)
        local count=$(awk '
            /^[[:space:]]*$/ {next}
            /^[[:space:]]*\/\// {next}
            {c++}
            END {print c+0}
        ' "$file" 2>/dev/null)
        echo "${count:-0}"
    else
        # Используем awk, чтобы учитывать последнюю строку без завершающего \n
        local lines=$(awk 'END {print NR+0}' "$file" 2>/dev/null)
        echo "${lines:-0}"
    fi
}

# check_directory scans a directory recursively, filters files by configured extensions and test-file settings, counts lines for each text file, prints a per-file rating and aggregated statistics, and exits with code 0 unless the percentage of good files (OK or ACCEPTABLE) is below 60% (then exits 1).
check_directory() {
    local dir=${1:-.}
    local total_files=0
    local ok_files=0
    local acceptable_files=0
    local bad_files=0
    
    echo "Проверка размера файлов в директории: $dir"
    echo "=================================================="
    printf "%-50s %-8s %-20s\n" "Файл" "Строки" "Оценка"
    echo "--------------------------------------------------"
    
    # Проходим по всем файлам в директории рекурсивно
    while IFS= read -r -d '' file; do
        # Проверяем расширение файла
        if has_allowed_extension "$file"; then
            # Проверяем, нужно ли исключить тестовые файлы
            if [ "$EXCLUDE_TESTS" = true ] && is_test_file "$file"; then
                continue
            fi
            
            # Проверяем, что это текстовый файл
            if is_text_file "$file"; then
                local lines=$(count_effective_lines "$file")
                if [ -n "$lines" ] && [ "$lines" -gt 0 ]; then
                    local rating=$(get_file_rating $lines "$file")
                    printf "%-50s %-8d %s\n" "$file" "$lines" "$rating"
                    
                    total_files=$((total_files + 1))
                    
                    if [ $lines -le 575 ]; then
                        ok_files=$((ok_files + 1))
                    elif [ $lines -le 675 ]; then
                        acceptable_files=$((acceptable_files + 1))
                    else
                        bad_files=$((bad_files + 1))
                    fi
                fi
            fi
        fi
    done < <(find "$dir" -type f -print0 2>/dev/null)
    
    echo "=================================================="
    echo "Статистика:"
    echo "Всего проверено файлов: $total_files"
    echo -e "OK (≤575 строк): ${GREEN}$ok_files${NC}"
    echo -e "Acceptable (576-675 строк): ${YELLOW}$acceptable_files${NC}"
    echo -e "BAD (>675 строк): ${RED}$bad_files${NC}"
    
    # Рассчитываем процент "хороших" файлов (OK + ACCEPTABLE)
    local good_files=$((ok_files + acceptable_files))
    local percentage=0
    if [ $total_files -gt 0 ]; then
        percentage=$((good_files * 100 / total_files))
    fi
    
    echo ""
    echo "Процент хороших файлов: $percentage%"
    
    # Определяем общую оценку на основе процента
    local overall_rating=""
    local overall_color=""
    local exit_code=0
    
    if [ $percentage -ge 90 ]; then
        overall_rating="ОТЛИЧНО"
        overall_color="$GREEN"
        echo -e "Общая оценка: ${overall_color}$overall_rating${NC} (≥90% хороших файлов)"
    elif [ $percentage -ge 75 ]; then
        overall_rating="ХОРОШО"
        overall_color="$GREEN"
        echo -e "Общая оценка: ${overall_color}$overall_rating${NC} (75-89% хороших файлов)"
    elif [ $percentage -ge 60 ]; then
        overall_rating="УДОВЛЕТВОРИТЕЛЬНО"
        overall_color="$YELLOW"
        echo -e "Общая оценка: ${overall_color}$overall_rating${NC} (60-74% хороших файлов)"
    else
        overall_rating="ТРЕБУЕТ УЛУЧШЕНИЯ"
        overall_color="$RED"
        echo -e "Общая оценка: ${overall_color}$overall_rating${NC} (<60% хороших файлов)"
        exit_code=1
    fi
    
    # Дополнительные сообщения
    if [ $bad_files -gt 0 ]; then
        exit_code=1
        echo -e "\n${RED}ВНИМАНИЕ: Найдены файлы, требующие рефакторинга!${NC}"
    elif [ $acceptable_files -gt 0 ]; then
        echo -e "\n${YELLOW}ВНИМАНИЕ: Найдены файлы с приемлемым размером, но стоит рассмотреть оптимизацию.${NC}"
    fi
    
    exit $exit_code
}

# show_help prints usage information, available options, rating criteria, examples, and overall grading thresholds for the script.
show_help() {
    echo "Использование: $0 [опции] [директория]"
    echo ""
    echo "Проверяет размер файлов в указанной директории (по умолчанию текущая)."
    echo ""
    echo "Критерии оценки:"
    echo "  ≤575 строк    - OK (зеленый)"
    echo "  576-675 строк - ACCEPTABLE (желтый)"
    echo "  >675 строк    - BAD - need refactoring (красный)"
    echo ""
    echo "Опции:"
    echo "  -h, --help              - показать эту справку"
    echo "  -e, --extensions EXT    - расширения файлов (по умолчанию: go)"
    echo "                           пример: -e 'go,js,ts' или -e 'go'"
    echo "  -t, --include-tests     - включить тестовые файлы (по умолчанию исключены)"
    echo "  -a, --all-files         - проверить все текстовые файлы (игнорировать расширения)"
    echo ""
    echo "Общая оценка:"
    echo "  ≥90% хороших файлов     - ОТЛИЧНО (зеленый)"
    echo "  75-89% хороших файлов   - ХОРОШО (зеленый)"
    echo "  60-74% хороших файлов   - УДОВЛЕТВОРИТЕЛЬНО (желтый)"
    echo "  <60% хороших файлов     - ТРЕБУЕТ УЛУЧШЕНИЯ (красный)"
    echo ""
    echo "Примеры:"
    echo "  $0                                    # проверить .go файлы (тесты исключены)"
    echo "  $0 -t                                 # проверить .go файлы, включив тесты"
    echo "  $0 -e 'go,js,ts'                     # проверить .go, .js, .ts файлы"
    echo "  $0 -a                                 # проверить все текстовые файлы"
    echo "  $0 -t -e 'go' /path/to/project       # проверить .go файлы в /path/to/project, включив тесты"
}

# Парсинг аргументов
while [[ $# -gt 0 ]]; do
    case $1 in
        -h|--help)
            show_help
            exit 0
            ;;
        -e|--extensions)
            EXTENSIONS="$2"
            shift 2
            ;;
        -t|--include-tests)
            EXCLUDE_TESTS=false
            shift
            ;;
        -a|--all-files)
            EXTENSIONS=""
            shift
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