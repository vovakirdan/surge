package lexer

import (
	"surge/internal/token"
)

// Поддержка: 0, 123, 0b..., 0o..., 0x..., 1.0, 1e-3, 1.0e+10.
// На этом шаге **без** суффиксов (u8, f32 и т.д.) — останутся в Token.Text, но Kind ставим как IntLit/FloatLit по факту.
// Неверные формы — репорт в opts.Reporter, токен по возможности завершаем.
func (lx *Lexer) scanNumber() token.Token {
	start := lx.cursor.Mark()

	// Правила (минимум):
	//  - 0b[01_]+, 0o[0-7_]+, 0x[0-9a-fA-F_]+
	//  - десятичные: [0-9][0-9_]* (опц. .[0-9_]+) (опц. [eE][+-]?[0-9_]+)
	//  - .[0-9_]+ (если вызваны после проверки isNumberAfterDot)
	//  - Валидацию расположения '_' пока мягко: разрешаем внутри цифр; грубые ошибки репортим позже.

	kind := token.IntLit

	// ведущая точка — значит формат ".digits"
	if lx.cursor.Peek() == '.' {
		lx.cursor.Bump() // '.'
		if !isDec(lx.cursor.Peek()) {
			sp := lx.cursor.SpanFrom(start)
			lx.report("BadNumber", sp, "expected digit after '.'")
			return token.Token{Kind: token.Invalid, Span: sp, Text: string(lx.file.Content[sp.Start:sp.End])}
		}
		kind = token.FloatLit
		for isDec(lx.cursor.Peek()) || lx.cursor.Peek() == '_' {
			lx.cursor.Bump()
		}
		goto emitWithMaybeExp
	}

	// ведущий 0 и база?
	if lx.cursor.Peek() == '0' {
		lx.cursor.Bump()
		switch lx.cursor.Peek() {
		case 'b', 'B':
			lx.cursor.Bump()
			for {
				b := lx.cursor.Peek()
				if b == '0' || b == '1' || b == '_' {
					lx.cursor.Bump()
				} else { break }
			}
			goto emit
		case 'o', 'O':
			lx.cursor.Bump()
			for {
				b := lx.cursor.Peek()
				if (b >= '0' && b <= '7') || b == '_' {
					lx.cursor.Bump()
				} else { break }
			}
			goto emit
		case 'x', 'X':
			lx.cursor.Bump()
			for isHex(lx.cursor.Peek()) || lx.cursor.Peek() == '_' {
				lx.cursor.Bump()
			}
			goto emit
		default:
			// просто "0" (возможно далее десятичная дробь)
		}
	}

	// десятичная целая часть
	for isDec(lx.cursor.Peek()) || lx.cursor.Peek() == '_' {
		lx.cursor.Bump()
	}

	// дробная часть
	if lx.cursor.Peek() == '.' {
		// смотрим, есть ли цифра после точки
		b0, b1, ok := lx.cursor.Peek2()
		if ok && b0 == '.' && (b1 == '.' || b1 == '=') {
			// это '..' или '..=' — НЕ часть числа
		} else {
			lx.cursor.Bump() // '.'
			if isDec(lx.cursor.Peek()) {
				kind = token.FloatLit
				for isDec(lx.cursor.Peek()) || lx.cursor.Peek() == '_' {
					lx.cursor.Bump()
				}
			} else {
				// одиночная точка без дробной части — допустимо как float "1."
				kind = token.FloatLit
			}
		}
	}

emitWithMaybeExp:
	// экспонента
	if lx.cursor.Peek() == 'e' || lx.cursor.Peek() == 'E' {
		kind = token.FloatLit
		lx.cursor.Bump() // e/E
		if lx.cursor.Peek() == '+' || lx.cursor.Peek() == '-' {
			lx.cursor.Bump()
		}
		if !isDec(lx.cursor.Peek()) {
			sp := lx.cursor.SpanFrom(start)
			lx.report("BadNumber", sp, "expected digit after exponent")
			return token.Token{Kind: token.Invalid, Span: sp, Text: string(lx.file.Content[sp.Start:sp.End])}
		}
		for isDec(lx.cursor.Peek()) || lx.cursor.Peek() == '_' {
			lx.cursor.Bump()
		}
	}

emit:
	sp := lx.cursor.SpanFrom(start)
	return token.Token{Kind: kind, Span: sp, Text: string(lx.file.Content[sp.Start:sp.End])}
}
