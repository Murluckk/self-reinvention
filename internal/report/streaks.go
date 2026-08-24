package report

import "github.com/murluckk/self-reinvention/internal/model"

// Streaks — счётчики, которые показывает /s.
type Streaks struct {
	Clean         int // дней подряд «чисто»
	English       int // дней подряд с английским
	NoDayOff      int // дней подряд без полного выходного
	FullDaysOff30 int // полных выходных за последние 30 дней
	Filled        int // дней подряд с заполненной записью
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

func boolTri(p *bool) tri {
	if p == nil {
		return triUnknown
	}
	if *p {
		return triYes
	}
	return triNo
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
	s.Clean = streak(idx, today, first, false, func(d *model.Day) tri { return boolTri(d.Clean) })
	s.English = streak(idx, today, first, false, func(d *model.Day) tri {
		if d.English == nil {
			return triUnknown
		}
		if *d.English > 0 {
			return triYes
		}
		return triNo
	})
	// Полный выходной обрывает серию перегруза; день без записи считаем рабочим,
	// потому что выходной человек отмечает, а обычный день может и забыть.
	s.NoDayOff = streak(idx, today, first, true, func(d *model.Day) tri {
		if d.DayOff != nil && *d.DayOff {
			return triNo
		}
		return triYes
	})
	s.Filled = streak(idx, today, first, false, func(d *model.Day) tri {
		if d.Empty() {
			return triNo
		}
		return triYes
	})
	from := AddDays(today, -29)
	for date, d := range idx {
		if date >= from && date <= today && d.DayOff != nil && *d.DayOff {
			s.FullDaysOff30++
		}
	}
	return s
}

// MaxNoDayOffStreak возвращает самую длинную серию дней подряд без полного
// выходного внутри периода — в отличие от Streaks.NoDayOff, который считает
// серию на сегодня.
func MaxNoDayOffStreak(days []*model.Day) int {
	best, cur := 0, 0
	for _, d := range days {
		if d.DayOff != nil && *d.DayOff {
			cur = 0
			continue
		}
		cur++
		if cur > best {
			best = cur
		}
	}
	return best
}
