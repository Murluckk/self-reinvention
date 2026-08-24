package parse

import (
	"strings"
	"unicode"
)

// ParseNote вытаскивает из заметки ведущий тег `#тег` и возвращает его вместе с
// остальным текстом. Тег необязателен: смысл заметок в нулевом трении.
func ParseNote(text string) (tag, body string) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "#") {
		return "", text
	}
	rest := text[1:]
	end := strings.IndexFunc(rest, func(r rune) bool {
		return unicode.IsSpace(r)
	})
	if end < 0 {
		return strings.ToLower(rest), ""
	}
	return strings.ToLower(rest[:end]), strings.TrimSpace(rest[end:])
}
