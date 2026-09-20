package report

import (
	"fmt"
	"sort"
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

	p("## Сон")
	if s.Sleep.N() > 0 {
		mn, mnd := s.Sleep.Min()
		mx, mxd := s.Sleep.Max()
		p("- среднее: **%.1f ч** (за %s)", s.Sleep.Avg(), days(s.Sleep.N()))
		p("- минимум: %.1f ч (%s), максимум: %.1f ч (%s)", mn, mnd, mx, mxd)
	} else {
		p("- нет данных")
	}
	if s.Wake.N() > 0 {
		mn, _ := s.Wake.Min()
		mx, _ := s.Wake.Max()
		p("- подъём: с %s до %s, разброс **%.1f ч**", hhmm(mn), hhmm(mx), s.Wake.Spread())
	} else {
		p("- подъём: нет данных")
	}
	p("")

	p("## Тренировки")
	p("- всего: **%d**", s.Workouts)
	if len(s.WorkoutTypes) > 0 {
		types := make([]string, 0, len(s.WorkoutTypes))
		for k := range s.WorkoutTypes {
			types = append(types, k)
		}
		sort.Strings(types)
		for _, t := range types {
			p("- %s: %d", t, s.WorkoutTypes[t])
		}
	}
	p("")

	p("## Английский")
	p("- сумма: **%.0f мин**", s.English.Sum())
	p("- дней с занятием: %d из %d", s.EnglishDays, s.TotalDays)
	p("- пропусков (включая незаполненные дни): **%d**", s.EnglishSkips)
	p("")

	p("## Вес")
	if s.Weight.N() > 0 {
		first := s.Weight.Values[0]
		last := s.Weight.Values[s.Weight.N()-1]
		p("- %.1f → %.1f кг, дельта **%+.1f кг** (%s)", first, last, s.Weight.Delta(), measures(s.Weight.N()))
	} else {
		p("- нет данных")
	}
	p("")

	p("## Работа")
	p("- часов всего: **%.1f**", s.Work.Sum())
	p("- оплачиваемых смен в выходные: %d", s.Shifts)
	p("- полных выходных: **%d**", s.DaysOff)
	p("- максимальная серия дней без выходного: **%d**", s.MaxNoDayOff)
	p("")

	p("## Чистые дни")
	p("- чисто: **%d из %d** отмеченных дней", s.CleanDays, s.CleanKnown)
	if len(s.CleanFails) > 0 {
		p("- срывы: %s", strings.Join(s.CleanFails, ", "))
	} else {
		p("- срывов не отмечено")
	}
	p("")

	p("## Телеграм вне окон")
	if s.Telegram.N() > 0 {
		p("- сумма: **%.0f**, среднее за день: %.1f (за %s)", s.Telegram.Sum(), s.Telegram.Avg(), days(s.Telegram.N()))
	} else {
		p("- нет данных")
	}
	p("")

	p("## Состояние")
	for _, x := range []struct {
		name string
		s    *Series
	}{{"фокус", &s.Focus}, {"настроение", &s.Mood}, {"энергия", &s.Energy}} {
		if x.s.N() == 0 {
			p("- %s: нет данных", x.name)
			continue
		}
		if x.s.N() < 4 {
			p("- %s: среднее **%.1f/10** (за %s, для динамики мало данных)", x.name, x.s.Avg(), days(x.s.N()))
			continue
		}
		p("- %s: среднее **%.1f/10**, динамика %+.1f", x.name, x.s.Avg(), x.s.HalfDelta())
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
	p("- чисто подряд: **%d**", s.Streaks.Clean)
	p("- английский подряд: **%d**", s.Streaks.English)
	p("- без полного выходного подряд: **%d**", s.Streaks.NoDayOff)
	p("- полных выходных за 30 дней: **%d**", s.Streaks.FullDaysOff30)
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
	cols := []string{"sleep", "wake", "workout", "english", "work", "day_off", "clean", "telegram", "focus", "mood", "energy"}
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
