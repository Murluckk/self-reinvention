// Package llm — второй уровень разбора свободной речи: локальная модель через
// Ollama. Схема ответа генерируется из структуры model.Day, поэтому она физически
// не может разъехаться с моделью данных: добавили поле — модель сразу знает о нём.
package llm

import (
	"fmt"
	"strings"

	"github.com/murluckk/self-reinvention/internal/model"
)

func jsonType(k model.Kind) string {
	switch k {
	case model.KindFloat:
		return "number"
	case model.KindInt, model.KindScale, model.KindDuration:
		return "integer"
	case model.KindBool:
		return "boolean"
	default:
		return "string"
	}
}

// Schema печатает описание ожидаемого JSON в виде, который хорошо читают
// инструктивные модели: список полей с типами и пояснениями.
func Schema() string {
	var b strings.Builder
	b.WriteString("{\n")
	b.WriteString("  \"day\": {            // запись дня, включай ТОЛЬКО упомянутые поля\n")
	for _, f := range model.Fields() {
		extra := ""
		switch f.Kind {
		case model.KindScale:
			extra = ", целое 1..10"
		case model.KindTime:
			extra = ", строка \"HH:MM\""
		}
		if f.Unit != "" {
			extra += ", единица: " + f.Unit
		}
		fmt.Fprintf(&b, "    %q: %s,   // %s%s\n", f.DB, jsonType(f.Kind), f.Desc, extra)
	}
	b.WriteString("  },\n")
	b.WriteString(`  "money": [          // деньги, если упомянуты; иначе не включай
    {"kind": "income|expense|saving", "amount": number, "currency": "RUB|USD|EUR", "category": string, "comment": string}
  ],
  "note": string      // всё, что не разложилось по полям, дословно; иначе не включай
}`)
	return b.String()
}

// SystemPrompt — инструкция парсеру. Требования жёсткие намеренно: нам нужен
// машинно-читаемый ответ, а не рассуждения.
func SystemPrompt() string {
	return `Ты парсер дневниковых записей. На вход — расшифровка русской речи о прожитом дне.
Верни РОВНО ОДИН JSON-объект по схеме ниже. Без markdown, без пояснений, без текста вокруг.

Схема:
` + Schema() + `

Правила:
- Поля, которых нет во фразе, НЕ включай в вывод вообще. Не подставляй нули, пустые строки и null.
- Числа возвращай числами, а не строками. Слова-числительные переводи в цифры ("семь с половиной" -> 7.5).
- Время суток — строка "HH:MM" в 24-часовом формате.
- algorithms и system_design: длительность занятия в минутах; если явно не занимался — 0.
- workout=true, если была любая тренировка; если явно не было — false.
- mood: эмоциональное состояние от 1 до 10.
- Отрицания учитывай: "не занимался алгоритмами" -> algorithms=0, "без тренировки" -> workout=false.
- Если во фразе есть кусок, не относящийся ни к одному полю, положи его в note.`
}
