package report

import (
	"github.com/murluckk/self-reinvention/internal/model"
)

// Streaks — серии, которые показывают /s, выгрузка и дашборд.
type Streaks struct {
	ByField map[string]int // дней подряд по каждой привычке
	Filled  int            // дней подряд с заполненной записью
}

// Of возвращает серию по колонке.
func (s Streaks) Of(col string) int { return s.ByField[col] }

// tri — троичный ответ предиката: да, нет и «нет данных».
type tri int

const (
	triUnknown tri = iota
	triYes
	triNo
)

// streak считает, сколько дней подряд, заканчивая сегодняшним, выполняется
// условие. Сегодняшний день особый: он ещё не закрыт, поэтому отсутствие
// данных за сегодня не обрывает серию — иначе стрик обнулялся бы каждое утро.
func streak(idx map[string]*model.Day, today, first string, unknownContinues bool, pred func(*model.Day) tri) int {
	if len(idx) == 0 || first == "" {
		return 0
	}
	n := 0
	cur := today
	for cur >= first {
		var v tri
		if d, ok := idx[cur]; ok {
			v = pred(d)
		}
		switch {
		case v == triYes:
			n++
		case v == triNo:
			return n
		case unknownContinues:
			n++
		case cur == today:
			// сегодня ещё не закрыт — идём дальше, не засчитывая день
		default:
			return n
		}
		cur = PrevDate(cur)
	}
	return n
}

// ComputeStreaks считает стрики по всем полям. days должны быть
// отсортированы по дате; today задаёт точку отсчёта.
func ComputeStreaks(days []*model.Day, today string) Streaks {
	return ComputeStreaksIn(days, today, nil)
}

// ComputeStreaksIn считает серии только по показателям пользователя.
func ComputeStreaksIn(days []*model.Day, today string, fs model.FieldSet) Streaks {
	idx := make(map[string]*model.Day, len(days))
	first := ""
	for _, d := range days {
		if d.EmptyIn(fs) {
			continue
		}
		idx[d.Date] = d
		if first == "" || d.Date < first {
			first = d.Date
		}
	}
	s := Streaks{ByField: map[string]int{}}
	for _, f := range fs.Fields() {
		if !f.Habit() {
			continue
		}
		s.ByField[f.DB] = streak(idx, today, first, false, func(d *model.Day) tri {
			done := f.Done(d)
			switch {
			case done == nil:
				return triUnknown
			case *done:
				return triYes
			default:
				return triNo
			}
		})
	}
	s.Filled = streak(idx, today, first, false, func(d *model.Day) tri {
		if d.EmptyIn(fs) {
			return triNo
		}
		return triYes
	})
	return s
}
