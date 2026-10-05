package report

import (
	"fmt"
	"strings"

	"github.com/murluckk/self-reinvention/internal/model"
	"github.com/murluckk/self-reinvention/internal/parse"
)

// Markdown собирает выгрузку за период. Файл уходит на еженедельный разбор и
// читается в отрыве от бота, поэтому в нём есть всё: и агрегаты, и таблица по
// дням, и заметки дословно.
func Markdown(s *Stats) string {
	var b strings.Builder
	p := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }

	p("# Разбор: %s — %s", s.From, s.To)
	p("")
	p("Заполнено дней: **%d из %d**", s.FilledDays, s.TotalDays)
	p("")

	var timed, habits, limits, scales []model.Field
	for _, f := range s.Order {
		switch {
		case f.Kind == model.KindTime:
			timed = append(timed, f)
		case f.Kind == model.KindScale:
			scales = append(scales, f)
		case f.Bad:
			limits = append(limits, f)
		default:
			habits = append(habits, f)
		}
	}
	section := func(title string, fs []model.Field, line func(model.Field) string) {
		if len(fs) == 0 {
			return
		}
		p("## %s", title)
		for _, f := range fs {
			p("- %s", line(f))
		}
		p("")
	}
	section("Режим", timed, func(f model.Field) string {
		st := s.Field(f.DB)
		if st.Series.N() == 0 {
			return strings.ToLower(f.Label) + ": нет данных"
		}
		avg, spread := st.TimeAvg()
		return fmt.Sprintf("%s: в среднем **%s**, разброс **%.1f ч** (за %s)",
			strings.ToLower(f.Label), clock(avg), spread, days(st.Series.N()))
	})
	section("Привычки и занятия", habits, func(f model.Field) string {
		return fmt.Sprintf("%s: **%s**", strings.ToLower(f.Label), s.Field(f.DB).Summary())
	})
	section("Ограничения", limits, func(f model.Field) string {
		st := s.Field(f.DB)
		return fmt.Sprintf("%s: **%d из %d** отмеченных дней", strings.ToLower(f.Label), st.Yes, st.Known)
	})
	section("Состояние", scales, func(f model.Field) string {
		return fmt.Sprintf("%s: **%s**", strings.ToLower(f.Label), s.Field(f.DB).Summary())
	})

	if s.Finance {
		p("## Деньги")
		if len(s.CurOrder) == 0 {
			p("- за период записей нет")
		}
		for _, code := range s.CurOrder {
			c := s.Currencies[code]
			p("### %s", code)
			if c.Income > 0 || c.Expense > 0 {
				p("- доход: **%s**", parse.FormatAmount(c.Income))
				p("- расход: **%s**", parse.FormatAmount(c.Expense))
			}
			p("- отложено: **%s**", parse.FormatAmount(c.Saved))
			if rate, ok := c.SavingsRate(); ok {
				p("- норма сбережений: **%.0f%%**", rate*100)
			}
			p("- накоплено всего: **%s**", parse.FormatAmount(c.Capital))
		}
		p("")
	}

	p("## Стрики")
	for _, f := range s.Order {
		if f.Habit() {
			p("- %s подряд: **%d**", strings.ToLower(f.Label), s.Streaks.Of(f.DB))
		}
	}
	p("- заполненных записей подряд: **%d**", s.Streaks.Filled)
	p("")

	if len(s.Insights) > 0 {
		p("## Наблюдения за %d дней", insightWindow)
		p("")
		p("Связи, а не причины: повод присмотреться, а не вывод.")
		p("")
		for _, in := range s.Insights {
			p("- %s", in.Text)
		}
		p("")
	}

	p("## Флаги")
	if len(s.Flags) == 0 {
		p("- всё в норме")
	}
	for _, f := range s.Flags {
		p("- ⚠️ %s", f)
	}
	p("")

	p("## По дням")
	p("")
	b.WriteString(dayTable(s.Days, s.Fields))
	p("")

	p("## Заметки")
	if len(s.Notes) == 0 {
		p("")
		p("Заметок за период нет.")
	}
	var curDate string
	for _, n := range s.Notes {
		if n.Date != curDate {
			curDate = n.Date
			p("")
			p("### %s (%s)", curDate, Weekday(curDate))
		}
		prefix := n.TS.Format("15:04")
		if n.Tag != "" {
			p("- `%s` **#%s** %s", prefix, n.Tag, n.Text)
		} else {
			p("- `%s` %s", prefix, n.Text)
		}
	}
	return b.String()
}

// dayTable рисует таблицу по дням: она нужна, чтобы на разборе можно было
// глазами найти конкретный день, а не только средние.
func dayTable(days []*model.Day, set model.FieldSet) string {
	fs := set.Fields()
	head := []string{"дата", "дн"}
	for _, f := range fs {
		head = append(head, f.Label)
	}
	var b strings.Builder
	b.WriteString("| " + strings.Join(head, " | ") + " |\n")
	b.WriteString("|" + strings.Repeat("---|", len(head)) + "\n")
	for _, d := range days {
		if d.EmptyIn(set) {
			continue
		}
		row := []string{d.Date, Weekday(d.Date)}
		for i := range fs {
			f := fs[i]
			if !f.IsSet(d) {
				row = append(row, "")
				continue
			}
			v := f.FormatValue(d)
			if f.Unit != "" {
				v = strings.TrimSuffix(v, " "+f.Unit)
			}
			row = append(row, v)
		}
		b.WriteString("| " + strings.Join(row, " | ") + " |\n")
	}
	return b.String()
}

func hhmm(hours float64) string {
	h := int(hours)
	m := int((hours - float64(h)) * 60.0)
	return fmt.Sprintf("%02d:%02d", h, m)
}
