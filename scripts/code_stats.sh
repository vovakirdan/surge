#!/bin/bash

# Скрипт для вывода статистики по строкам кода компилятора Surge

# Цветовые коды для вывода
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
MAGENTA='\033[0;35m'
BOLD='\033[1m'
NC='\033[0m' # No Color

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
    find_cmd="$find_cmd -not -path \"./testdata/*\" -not -path \"./stdlib/*\" -not -path \"./core/*\""
    
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
echo -e "${BOLD}${CYAN}═══════════════════════════════════════════════════════════════${NC}"
echo -e "${BOLD}${CYAN}           Статистика кодовой базы компилятора Surge${NC}"
echo -e "${BOLD}${CYAN}═══════════════════════════════════════════════════════════════${NC}"
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

# Тестовые файлы
test_stats=$(get_dir_stats "." false)
test_files_total=$(echo $test_stats | awk '{print $1}')
test_lines_total=$(echo $test_stats | awk '{print $2}')

# Вычитаем основные файлы из общего количества для получения только тестов
test_only_stats=$(get_dir_stats "." false)
test_only_files=$(find cmd internal -name "*_test.go" 2>/dev/null | wc -l)
test_only_lines=$(find cmd internal -name "*_test.go" 2>/dev/null -exec wc -l {} + 2>/dev/null | tail -1 | awk '{print $1}')
test_only_lines=${test_only_lines:-0}

# Общий объем
total_lines=$((main_lines + test_only_lines))
total_files=$((main_files + test_only_files))

# Вывод основной статистики
echo -e "${BOLD}${GREEN}📊 Основной код компилятора (без тестов)${NC}"
echo -e "   Файлов: ${BOLD}$(format_number $main_files)${NC}"
echo -e "   Строк кода: ${BOLD}$(format_number $main_lines)${NC}"
echo ""

# Разбивка по директориям
echo -e "${BOLD}${BLUE}📁 Разбивка по директориям:${NC}"
printf "   %-20s %8s файлов  %12s строк\n" "cmd/" "$(format_number $cmd_files)" "$(format_number $cmd_lines)"
printf "   %-20s %8s файлов  %12s строк\n" "internal/" "$(format_number $internal_files)" "$(format_number $internal_lines)"
echo ""

# Топ пackages
echo -e "${BOLD}${MAGENTA}🏆 Топ-10 пакетов по размеру:${NC}"
printf "   %-50s %12s\n" "Пакет" "Строк"
echo "   $(printf '─%.0s' {1..65})"

top_packages=$(get_top_packages 10 true)
rank=1
while IFS='|' read -r pkg lines; do
    if [ -n "$pkg" ] && [ -n "$lines" ]; then
        printf "   %2d. %-45s %12s\n" "$rank" "$pkg" "$(format_number $lines)"
        rank=$((rank + 1))
    fi
done < <(echo -e "$top_packages")
echo ""

# Статистика по тестам
echo -e "${BOLD}${YELLOW}🧪 Тестовые файлы:${NC}"
echo -e "   Файлов: ${BOLD}$(format_number $test_only_files)${NC}"
echo -e "   Строк кода: ${BOLD}$(format_number $test_only_lines)${NC}"
echo ""

# Общий объем
echo -e "${BOLD}${CYAN}📈 Общий объем (код + тесты):${NC}"
echo -e "   Файлов: ${BOLD}$(format_number $total_files)${NC}"
echo -e "   Строк кода: ${BOLD}$(format_number $total_lines)${NC}"
echo ""

# Процентное соотношение
if [ $total_lines -gt 0 ]; then
    main_percent=$((main_lines * 100 / total_lines))
    test_percent=$((test_only_lines * 100 / total_lines))
    echo -e "${BOLD}${CYAN}📊 Процентное соотношение:${NC}"
    echo -e "   Основной код: ${GREEN}${main_percent}%${NC}"
    echo -e "   Тесты: ${YELLOW}${test_percent}%${NC}"
    echo ""
fi

# Итоговая оценка
echo -e "${BOLD}${CYAN}═══════════════════════════════════════════════════════════════${NC}"
echo -e "${BOLD}Итог:${NC} Компилятор Surge содержит примерно ${BOLD}${GREEN}$(format_number $main_lines)${NC} строк основного кода"
echo -e "       что соответствует среднему компилятору языка программирования."
echo -e "${BOLD}${CYAN}═══════════════════════════════════════════════════════════════${NC}"

