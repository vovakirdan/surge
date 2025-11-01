// Package format contains lightweight formatting passes that operate on the AST
// to produce stable textual output without rebuilding full pretty-printers.
//
// Назначение: ранние форматтеры (normalize-команды) поверх уже разобранного AST.
// Не делает: полноценного pretty-print всего файла, генерации кода или IO.
// Зависимости: internal/ast, internal/source.
package format
