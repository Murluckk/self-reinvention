package report

import (
	"fmt"
	"strings"

	"github.com/murluckk/self-reinvention/internal/model"
	"github.com/murluckk/self-reinvention/internal/parse"
)

// Status — короткая сводка для /s: что уже записано сегодня, чего не хватает,
// итоги недели, капитал и стрики.
func Status(today *model.Day, week *Stats) string {
	var b strings.Builder
	p := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }

	p("📅 Сегодня %s (%s)", today.Date, Weekday(today.Date))
	set := today.SetFieldsIn(week.Fields)
	if len(set) == 0 {
		p("Записи ещё нет.")
	} else {
		for _, f := range set {
			p("  %s: %s", f.Label, f.FormatValue(today))
		}
	}
	if missing := today.MissingIn(week.Fields); len(missing) > 0 {
		names := make([]string, 0, len(missing))
		for _, f := range missing {
			names = append(names, f.Keys[0])
		}
		p("Не заполнено: %s", strings.Join(names, ", "))
	} else {
		p("Заполнено всё.")
	}

	p("")
	p("🗓 Неделя %s — %s: %d/%d дней", week.From, week.To, week.FilledDays, week.TotalDays)
	for _, f := range week.Order {
		p("  %s: %s", strings.ToLower(f.Label), week.Field(f.DB).Summary())
	}

	if week.Finance {
		p("")
		p("💰 Капитал")
		if len(week.CurOrder) == 0 {
			p("  записей пока нет")
		}
		for _, code := range week.CurOrder {
			c := week.Currencies[code]
			line := fmt.Sprintf("  %s: %s накоплено", code, parse.FormatAmount(c.Capital))
			if rate, ok := c.SavingsRate(); ok {
				line += fmt.Sprintf(" (за неделю отложено %s, норма %.0f%%)", parse.FormatAmount(c.Saved), rate*100)
			} else if c.Saved > 0 {
				line += fmt.Sprintf(" (за неделю отложено %s)", parse.FormatAmount(c.Saved))
			}
			p("%s", line)
		}
	}

	p("")
	p("🔥 Стрики")
	for _, f := range week.Order {
		if f.Habit() {
			p("  %s: %d дн.", strings.ToLower(f.Label), week.Streaks.Of(f.DB))
		}
	}
	p("  записей подряд: %d дн.", week.Streaks.Filled)

	if len(week.Insights) > 0 {
		p("")
		p("🔎 Заметил: %s", week.Insights[0].Text)
	}

	if len(week.Flags) > 0 {
		p("")
		p("⚠️ Флаги")
		for _, f := range week.Flags {
			p("  • %s", f)
		}
	}
	return b.String()
}
