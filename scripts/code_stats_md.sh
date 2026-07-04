#!/bin/bash

# Скрипт для вывода статистики по строкам кода компилятора Surge в формате Markdown

# Функция для форматирования чисел с разделителями тысяч
format_number() {
    printf "%'d" "$1" 2>/dev/null || echo "$1"
}

# Функция для получения статистики по директории
get_dir_stats() {
    local dir=$1
    local exclude_tests=${2:-true}
    
    if [ ! -d "$dir" ]; then
        echo "0 0"
        return
    fi
    
    local find_cmd="find \"$dir\" -name \"*.go\""
    if [ "$exclude_tests" = "true" ]; then
        find_cmd="$find_cmd -not -name \"*_test.go\""
    fi
    # Exclude tool/vendor checkouts nested under the repo root (e.g. agent
    # worktrees under .claude/worktrees/, and any target/ build output) so a
    # dir-scoped scan of "." does not double-count a full nested repo copy.
    find_cmd="$find_cmd -not -path \"./testdata/*\" -not -path \"./stdlib/*\" -not -path \"./core/*\" -not -path \"./.claude/*\" -not -path \"./target/*\""

    local file_count=$(eval "$find_cmd" 2>/dev/null | wc -l)
    local line_count=$(eval "$find_cmd -exec wc -l {} +" 2>/dev/null | tail -1 | awk '{print $1}')

    echo "${file_count:-0} ${line_count:-0}"
}

# Функция для получения статистики по C коду из runtime/native
get_c_stats() {
    local dir="runtime/native"
    
    if [ ! -d "$dir" ]; then
        echo "0 0"
        return
    fi
    
    # Считаем .c и .h файлы
    local find_cmd="find \"$dir\" \\( -name \"*.c\" -o -name \"*.h\" \\)"
    
    local file_count=$(eval "$find_cmd" 2>/dev/null | wc -l)
    local line_count=$(eval "$find_cmd -exec wc -l {} +" 2>/dev/null | tail -1 | awk '{print $1}')
    
    echo "${file_count:-0} ${line_count:-0}"
}

# Функция для получения топ пакетов
get_top_packages() {
    local limit=${1:-10}
    local exclude_tests=${2:-true}
    
    local find_cmd="find cmd internal -type d 2>/dev/null"
    local packages=""
    
    while IFS= read -r dir; do
        if [ -z "$dir" ] || [ ! -d "$dir" ]; then
            continue
        fi
        
        # Пропускаем общие директории cmd и internal
        if [ "$dir" = "cmd" ] || [ "$dir" = "internal" ]; then
            continue
        fi
        
        # Считаем файлы только в текущей директории (конкретный пакет)
        local find_files="find \"$dir\" -maxdepth 1 -name \"*.go\""
        if [ "$exclude_tests" = "true" ]; then
            find_files="$find_files -not -name \"*_test.go\""
        fi
        
        find_files="$find_files -not -path \"./testdata/*\" -not -path \"./stdlib/*\" -not -path \"./core/*\""
        
        local count=$(eval "$find_files -exec wc -l {} +" 2>/dev/null | tail -1 | awk '{print $1}')
        if [ -n "$count" ] && [ "$count" != "0" ]; then
            packages="${packages}${dir}|${count}\n"
        fi
    done < <(eval "$find_cmd")
    
    # Убираем дубликаты и сортируем
    echo -e "$packages" | sort -t'|' -k2 -rn | head -n "$limit"
}

# Получаем статистику
echo "# Codebase stats for the Surge compiler"
echo ""
echo "---"
echo ""

# Основной код компилятора (без тестов)
main_stats=$(get_dir_stats "." true)
main_files=$(echo $main_stats | awk '{print $1}')
main_lines=$(echo $main_stats | awk '{print $2}')

# Статистика по директориям
cmd_stats=$(get_dir_stats "cmd" true)
cmd_files=$(echo $cmd_stats | awk '{print $1}')
cmd_lines=$(echo $cmd_stats | awk '{print $2}')

internal_stats=$(get_dir_stats "internal" true)
internal_files=$(echo $internal_stats | awk '{print $1}')
internal_lines=$(echo $internal_stats | awk '{print $2}')

# Статистика по C коду из runtime/native
c_stats=$(get_c_stats)
c_files=$(echo $c_stats | awk '{print $1}')
c_lines=$(echo $c_stats | awk '{print $2}')

# Тестовые файлы
test_stats=$(get_dir_stats "." false)
test_files_total=$(echo $test_stats | awk '{print $1}')
test_lines_total=$(echo $test_stats | awk '{print $2}')

# Вычитаем основные файлы из общего количества для получения только тестов
test_only_stats=$(get_dir_stats "." false)
test_only_files=$(find cmd internal -name "*_test.go" 2>/dev/null | wc -l)
test_only_lines=$(find cmd internal -name "*_test.go" 2>/dev/null -exec wc -l {} + 2>/dev/null | tail -1 | awk '{print $1}')
test_only_lines=${test_only_lines:-0}

# Общий объем (Go код + C код + тесты)
total_lines=$((main_lines + c_lines + test_only_lines))
total_files=$((main_files + c_files + test_only_files))

# Основной код включая C код
main_with_c_lines=$((main_lines + c_lines))
main_with_c_files=$((main_files + c_files))

# Вывод основной статистики
echo "## 📊 Main code (without tests)"
echo ""
echo "- **Files:** $(format_number $main_with_c_files) (Go: $(format_number $main_files), C: $(format_number $c_files))"
echo "- **Lines of code:** $(format_number $main_with_c_lines) (Go: $(format_number $main_lines), C: $(format_number $c_lines))"
echo ""

# Разбивка по директориям
echo "## 📁 Directory breakdown"
echo ""
echo "| Directory | Files | Lines |"
echo "|------------|--------|-------|"
printf "| \`cmd/\` | %s | %s |\n" "$(format_number $cmd_files)" "$(format_number $cmd_lines)"
printf "| \`internal/\` | %s | %s |\n" "$(format_number $internal_files)" "$(format_number $internal_lines)"
printf "| \`runtime/native/\` (C code) | %s | %s |\n" "$(format_number $c_files)" "$(format_number $c_lines)"
echo ""

# Топ пackages
echo "## 🏆 Top 10 packages by size"
echo ""
echo "| # | Package | Lines |"
echo "|---|-------|-------|"

top_packages=$(get_top_packages 10 true)
rank=1
while IFS='|' read -r pkg lines; do
    if [ -n "$pkg" ] && [ -n "$lines" ]; then
        printf "| %d | \`%s\` | %s |\n" "$rank" "$pkg" "$(format_number $lines)"
        rank=$((rank + 1))
    fi
done < <(echo -e "$top_packages")
echo ""

# Статистика по тестам
echo "## 🧪 Test files"
echo ""
echo "- **Files:** $(format_number $test_only_files)"
echo "- **Lines of code:** $(format_number $test_only_lines)"
echo ""

# Общий объем
echo "## 📈 Total volume (code + tests)"
echo ""
echo "- **Files:** $(format_number $total_files)"
echo "- **Lines of code:** $(format_number $total_lines)"
echo ""

# Процентное соотношение
if [ $total_lines -gt 0 ]; then
    main_go_percent=$((main_lines * 100 / total_lines))
    main_c_percent=$((c_lines * 100 / total_lines))
    main_total_percent=$((main_with_c_lines * 100 / total_lines))
    test_percent=$((test_only_lines * 100 / total_lines))
    echo "## 📊 Percentage breakdown"
    echo ""
    echo "- **Main code (Go + C):** ${main_total_percent}% (Go: ${main_go_percent}%, C: ${main_c_percent}%)"
    echo "- **Tests:** ${test_percent}%"
    echo ""
fi
