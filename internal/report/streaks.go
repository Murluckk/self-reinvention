package report

import "github.com/murluckk/self-reinvention/internal/model"

// Streaks — счётчики, которые показывает /s.
type Streaks struct {
	Algorithms   int // дней подряд с алгоритмами
	SystemDesign int // дней подряд с системным дизайном
	Workout      int // дней подряд с тренировкой
	Filled       int // дней подряд с заполненной записью
}

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

// ComputeStreaks считает стрики по всем известным дням. days должны быть
// отсортированы по дате; today задаёт точку отсчёта.
func ComputeStreaks(days []*model.Day, today string) Streaks {
	idx := make(map[string]*model.Day, len(days))
	first := ""
	for _, d := range days {
		if d.Empty() {
			continue
		}
		idx[d.Date] = d
		if first == "" || d.Date < first {
			first = d.Date
		}
	}
	s := Streaks{}
	duration := func(value func(*model.Day) *int) func(*model.Day) tri {
		return func(d *model.Day) tri {
			v := value(d)
			if v == nil {
				return triUnknown
			}
			if *v > 0 {
				return triYes
			}
			return triNo
		}
	}
	s.Algorithms = streak(idx, today, first, false, duration(func(d *model.Day) *int {
		return d.Algorithms
	}))
	s.SystemDesign = streak(idx, today, first, false, duration(func(d *model.Day) *int {
		return d.SystemDesign
	}))
	s.Workout = streak(idx, today, first, false, func(d *model.Day) tri {
		if d.Workout == nil {
			return triUnknown
		}
		if *d.Workout {
			return triYes
		}
		return triNo
	})
	s.Filled = streak(idx, today, first, false, func(d *model.Day) tri {
		if d.Empty() {
			return triNo
		}
		return triYes
	})
	return s
}
