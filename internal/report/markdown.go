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

	p("## Режим")
	if s.Wake.N() > 0 {
		mn, _ := s.Wake.Min()
		mx, _ := s.Wake.Max()
		p("- подъём: с %s до %s, разброс **%.1f ч**", hhmm(mn), hhmm(mx), s.Wake.Spread())
	} else {
		p("- подъём: нет данных")
	}
	if s.Bed.N() > 0 {
		p("- последнее засыпание: **%s** (за %s)", hhmm(s.Bed.Values[s.Bed.N()-1]), days(s.Bed.N()))
	} else {
		p("- засыпание: нет данных")
	}
	p("")

	p("## Тренировки")
	p("- всего: **%d**", s.Workouts)
	p("")

	p("## Развитие")
	p("- алгоритмы: **%.0f мин**, %s", s.Algorithms.Sum(), days(s.AlgorithmDays))
	p("- системный дизайн: **%.0f мин**, %s", s.SystemDesign.Sum(), days(s.SystemDesignDays))
	p("")

	p("## Состояние")
	if s.Mood.N() == 0 {
		p("- нет данных")
	} else if s.Mood.N() < 4 {
		p("- среднее **%.1f/10** (за %s)", s.Mood.Avg(), days(s.Mood.N()))
	} else {
		p("- среднее **%.1f/10**, динамика %+.1f", s.Mood.Avg(), s.Mood.HalfDelta())
	}
	p("")

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

	p("## Стрики")
	p("- алгоритмы подряд: **%d**", s.Streaks.Algorithms)
	p("- системный дизайн подряд: **%d**", s.Streaks.SystemDesign)
	p("- тренировки подряд: **%d**", s.Streaks.Workout)
	p("- заполненных записей подряд: **%d**", s.Streaks.Filled)
	p("")

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
	b.WriteString(dayTable(s.Days))
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
func dayTable(days []*model.Day) string {
	cols := []string{"wake", "bed", "algorithms", "system_design", "workout", "mood"}
	var fs []model.Field
	head := []string{"дата", "дн"}
	for _, c := range cols {
		if f, ok := model.FieldByColumn(c); ok {
			fs = append(fs, *f)
			head = append(head, f.Label)
		}
	}
	var b strings.Builder
	b.WriteString("| " + strings.Join(head, " | ") + " |\n")
	b.WriteString("|" + strings.Repeat("---|", len(head)) + "\n")
	for _, d := range days {
		if d.Empty() {
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
