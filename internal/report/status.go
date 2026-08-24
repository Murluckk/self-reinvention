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
	set := today.SetFields()
	if len(set) == 0 {
		p("Записи ещё нет.")
	} else {
		for _, f := range set {
			p("  %s: %s", f.Label, f.FormatValue(today))
		}
	}
	if missing := today.MissingFields(); len(missing) > 0 {
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
	if week.Sleep.N() > 0 {
		p("  сон: %.1f ч в среднем", week.Sleep.Avg())
	}
	p("  тренировок: %d, английский: %.0f мин", week.Workouts, week.English.Sum())
	if week.Work.N() > 0 {
		p("  работа: %.1f ч, выходных: %d, смен: %d", week.Work.Sum(), week.DaysOff, week.Shifts)
	}
	if week.CleanKnown > 0 {
		p("  чисто: %d из %d", week.CleanDays, week.CleanKnown)
	}

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

	p("")
	p("🔥 Стрики")
	p("  чисто: %d дн.", week.Streaks.Clean)
	p("  английский: %d дн.", week.Streaks.English)
	p("  без полного выходного: %d дн.", week.Streaks.NoDayOff)
	p("  полных выходных за 30 дней: %d", week.Streaks.FullDaysOff30)
	p("  записей подряд: %d дн.", week.Streaks.Filled)

	if len(week.Flags) > 0 {
		p("")
		p("⚠️ Флаги")
		for _, f := range week.Flags {
			p("  • %s", f)
		}
	}
	return b.String()
}
