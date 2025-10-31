
// Package fuzztests houses Go fuzz harnesses that exercise the early Surge
// compilation pipeline (source -> lexer -> parser). Its goal is to smoke test
// robustness and guard against panics or allocator explosions on arbitrary
// inputs.
//
// Назначение: запускать fuzz-обработчики, которые загружают байты в FileSet и
// прогоняют их через лексер/парсер.
//
// Не делает: генерацию корпусов, запись файлов, выполнение CLI.
//
// Зависимости: internal/source, internal/lexer, internal/parser, internal/diag,
// internal/ast.

package fuzztests