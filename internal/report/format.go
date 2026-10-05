package report

import (
	"fmt"
	"strings"

	"github.com/murluckk/self-reinvention/internal/model"
)

// Summary — агрегат показателя за период одной фразой. Общий формат для /s,
// выгрузки и дашборда: новый показатель сразу появляется везде.
func (fs *FieldStat) Summary() string {
	f := fs.Field
	if fs.Known == 0 {
		return "нет данных"
	}
	switch f.Kind {
	case model.KindTime:
		avg, spread := fs.TimeAvg()
		return fmt.Sprintf("в среднем %s, разброс %.1f ч", clock(avg), spread)
	case model.KindDuration:
		return fmt.Sprintf("%.0f мин, %s", fs.Series.Sum(), days(fs.Yes))
	case model.KindScale:
		if fs.Series.N() >= 4 {
			return fmt.Sprintf("%.1f/10, динамика %+.1f", fs.Series.Avg(), fs.Series.HalfDelta())
		}
		return fmt.Sprintf("%.1f/10", fs.Series.Avg())
	case model.KindInt, model.KindFloat:
		return fmt.Sprintf("в среднем %s", trimFloat(fs.Series.Avg()))
	case model.KindBool:
		return fmt.Sprintf("%d из %d", fs.Yes, fs.Known)
	case model.KindString:
		return days(fs.Yes)
	}
	return ""
}

// Value — короткое значение для карточки дашборда.
func (fs *FieldStat) Value() string {
	f := fs.Field
	if fs.Known == 0 {
		return "—"
	}
	switch f.Kind {
	case model.KindTime:
		avg, _ := fs.TimeAvg()
		return clock(avg)
	case model.KindDuration:
		return fmt.Sprintf("%.0f мин", fs.Series.Sum())
	case model.KindScale:
		return fmt.Sprintf("%.1f/10", fs.Series.Avg())
	case model.KindInt, model.KindFloat:
		return trimFloat(fs.Series.Avg())
	case model.KindBool:
		return fmt.Sprintf("%d дн.", fs.Yes)
	case model.KindString:
		return fmt.Sprintf("%d дн.", fs.Yes)
	}
	return ""
}

// Hint — подпись под значением карточки.
func (fs *FieldStat) Hint() string {
	if fs.Known == 0 {
		return "нет данных"
	}
	switch fs.Field.Kind {
	case model.KindTime:
		_, spread := fs.TimeAvg()
		return fmt.Sprintf("в среднем · разброс %.1f ч", spread)
	case model.KindDuration:
		return days(fs.Yes)
	case model.KindBool:
		return fmt.Sprintf("из %d отмеченных", fs.Known)
	}
	return "за период"
}

// TimeAvg — среднее время суток и разброс в часах. Для вечерних показателей
// 00:30 считается позже 23:30, а не на 23 часа раньше.
func (fs *FieldStat) TimeAvg() (avg, spread float64) {
	vals := fs.Series.Values
	if len(vals) == 0 {
		return 0, 0
	}
	late := 0
	for _, h := range vals {
		if h >= 18 || h < 5 {
			late++
		}
	}
	shift := late*2 > len(vals)
	var s Series
	for i, h := range vals {
		if shift && h < 12 {
			h += 24
		}
		s.Add(fs.Series.Dates[i], h)
	}
	return s.Avg(), s.Spread()
}

func trimFloat(v float64) string {
	return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.2f", v), "0"), ".")
}
