package bot

import (
	"fmt"
	"strings"

	"github.com/murluckk/self-reinvention/internal/model"
	"github.com/murluckk/self-reinvention/internal/tg"
)

// option — кнопка с готовым значением поля.
type option struct{ label, value string }

// fieldOptions — типовые ответы на вопрос о поле. Кнопки покрывают частые
// значения, а редкие человек просто пишет текстом.
func fieldOptions(f *model.Field) [][]option {
	switch f.Kind {
	case model.KindBool:
		yes, no := "✅ да", "❌ нет"
		if f.Bad {
			yes, no = "да", "✅ нет"
		}
		return [][]option{{{yes, "1"}, {no, "0"}}}
	case model.KindScale:
		var rows [][]option
		for start := 1; start <= 10; start += 5 {
			var row []option
			for v := start; v < start+5; v++ {
				row = append(row, option{fmt.Sprint(v), fmt.Sprint(v)})
			}
			rows = append(rows, row)
		}
		return rows
	case model.KindDuration:
		return [][]option{
			{{"нет", "0"}, {"15", "15"}, {"30", "30"}, {"45", "45"}},
			{{"60", "60"}, {"90", "90"}, {"120", "120"}, {"180", "180"}},
		}
	case model.KindTime:
		times := []string{"06:00", "06:30", "07:00", "07:30", "08:00", "08:30", "09:00", "10:00"}
		if f.DB == "bed" {
			times = []string{"22:00", "22:30", "23:00", "23:30", "00:00", "00:30", "01:00", "02:00"}
		}
		return [][]option{
			{{times[0], times[0]}, {times[1], times[1]}, {times[2], times[2]}, {times[3], times[3]}},
			{{times[4], times[4]}, {times[5], times[5]}, {times[6], times[6]}, {times[7], times[7]}},
		}
	}
	return nil
}

// fieldKeyboard собирает клавиатуру ответа: callback каждой кнопки — prefix и
// значение. tail добавляет служебные ряды вроде «пропустить».
func fieldKeyboard(f *model.Field, prefix string, tail ...[]tg.InlineKeyboardButton) *tg.InlineKeyboardMarkup {
	kb := &tg.InlineKeyboardMarkup{}
	for _, row := range fieldOptions(f) {
		var buttons []tg.InlineKeyboardButton
		for _, o := range row {
			buttons = append(buttons, tg.Button(o.label, prefix+o.value))
		}
		kb.InlineKeyboard = append(kb.InlineKeyboard, buttons)
	}
	kb.InlineKeyboard = append(kb.InlineKeyboard, tail...)
	return kb
}

// fieldQuestion — вопрос о поле человеческим языком с подсказкой, как ответить.
func fieldQuestion(f *model.Field) string {
	hint := "нажми кнопку или напиши своё"
	switch f.Kind {
	case model.KindTime:
		hint += ", например 7:40"
	case model.KindDuration:
		hint = "сколько минут? Кнопка или число"
	case model.KindString:
		hint = "напиши текстом"
	case model.KindInt, model.KindFloat:
		hint = "напиши число"
	}
	return fmt.Sprintf("❓ %s — %s.\n%s", f.Label, f.Desc, capitalize(hint))
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

// fieldPicker — список полей пользователя для выбора; заполненные отмечены.
func fieldPicker(fs model.FieldSet, d *model.Day, prefix string, tail ...[]tg.InlineKeyboardButton) *tg.InlineKeyboardMarkup {
	kb := &tg.InlineKeyboardMarkup{}
	var row []tg.InlineKeyboardButton
	for _, f := range fs.Fields() {
		mark := "▫️ "
		if f.IsSet(d) {
			mark = "✓ "
		}
		row = append(row, tg.Button(mark+f.Label, prefix+f.DB))
		if len(row) == 2 {
			kb.InlineKeyboard = append(kb.InlineKeyboard, row)
			row = nil
		}
	}
	if len(row) > 0 {
		kb.InlineKeyboard = append(kb.InlineKeyboard, row)
	}
	kb.InlineKeyboard = append(kb.InlineKeyboard, tail...)
	return kb
}
