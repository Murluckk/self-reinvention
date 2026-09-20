package report

import (
	"fmt"
	"strings"

	"github.com/murluckk/self-reinvention/internal/config"
	"github.com/murluckk/self-reinvention/internal/model"
	"github.com/murluckk/self-reinvention/internal/parse"
)

// Status — короткая сводка для /s: что уже записано сегодня, чего не хватает,
// итоги недели, капитал и стрики.
func Status(today *model.Day, week *Stats) string {
	var b strings.Builder
	p := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }

	p("📅 Сегодня %s (%s)", today.Date, Weekday(today.Date))
	set := today.SetFieldsFor(week.Profile)
	if len(set) == 0 {
		p("Записи ещё нет.")
	} else {
		for _, f := range set {
			p("  %s: %s", f.Label, f.FormatValue(today))
		}
	}
	if missing := today.MissingFieldsFor(week.Profile); len(missing) > 0 {
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
	if week.Profile == config.ProfileSveta {
		p("  прогулок: %d, учёба: %d дн., полезное: %d дн.", week.Walks, week.StudyDays, week.UsefulDays)
		p("  сладкое: %d/%d, алкоголь: %d/%d", week.SweetDays, week.SweetKnown, week.AlcoholDays, week.AlcoholKnown)
	} else {
		p("  алгоритмы: %.0f мин, системный дизайн: %.0f мин", week.Algorithms.Sum(), week.SystemDesign.Sum())
	}
	p("  тренировок: %d", week.Workouts)
	if week.Mood.N() > 0 {
		p("  состояние: %.1f/10", week.Mood.Avg())
	}

	if week.Profile != config.ProfileSveta {
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
	if week.Profile == config.ProfileSveta {
		p("  учёба: %d дн.", week.Streaks.Study)
		p("  прогулки: %d дн.", week.Streaks.Walk)
		p("  тренировки: %d дн.", week.Streaks.Workout)
		p("  полезное: %d дн.", week.Streaks.Useful)
	} else {
		p("  алгоритмы: %d дн.", week.Streaks.Algorithms)
		p("  системный дизайн: %d дн.", week.Streaks.SystemDesign)
		p("  тренировки: %d дн.", week.Streaks.Workout)
	}
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
