package report

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/murluckk/self-reinvention/internal/model"
)

// Insight — наблюдение вида «в дни с X оценка выше/ниже». Это не причинность,
// а повод присмотреться: выборки маленькие, и бот честно показывает размер.
type Insight struct {
	Text  string
	Delta float64 // разница средних оценки между группами
}

const (
	insightMinGroup = 3   // меньше трёх дней в группе — шум
	insightMinDelta = 0.7 // по шкале 1–10 меньшая разница незаметна
	insightMax      = 5
)

// FindInsights сравнивает оценки (поля-шкалы, например состояние) в днях с
// разными значениями остальных показателей. Отбой и вредные привычки влияют
// на следующий день, поэтому для них сравнивается оценка назавтра.
func FindInsights(days []*model.Day, fs model.FieldSet) []Insight {
	byDate := make(map[string]*model.Day, len(days))
	for _, d := range days {
		byDate[d.Date] = d
	}
	var out []Insight
	for _, target := range fs.Fields() {
		if target.Kind != model.KindScale {
			continue
		}
		for _, factor := range fs.Fields() {
			if factor.DB == target.DB {
				continue
			}
			if in, ok := compare(days, byDate, &target, &factor); ok {
				out = append(out, in)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return math.Abs(out[i].Delta) > math.Abs(out[j].Delta) })
	if len(out) > insightMax {
		out = out[:insightMax]
	}
	return out
}

// split делит день на группу a («да», «раньше») или b; ok=false — нет данных.
type split func(d *model.Day) (inA, ok bool)

func compare(days []*model.Day, byDate map[string]*model.Day, target, factor *model.Field) (Insight, bool) {
	var group split
	var labelA, labelB string
	nextDay := false
	switch factor.Kind {
	case model.KindBool:
		group = func(d *model.Day) (bool, bool) {
			v, ok := factor.Get(d).(*bool)
			if !ok || v == nil {
				return false, false
			}
			return *v, true
		}
		labelA, labelB = "да", "нет"
		nextDay = factor.Bad
	case model.KindDuration:
		group = func(d *model.Day) (bool, bool) {
			done := factor.Done(d)
			if done == nil {
				return false, false
			}
			return *done, true
		}
		labelA, labelB = "были", "не было"
	case model.KindTime:
		values := map[string]float64{}
		var all []float64
		evening := isEvening(days, factor)
		for _, d := range days {
			if v, ok := factor.Get(d).(*string); ok && v != nil {
				if h, ok := model.TimeToHours(*v); ok {
					if evening && h < 12 {
						h += 24
					}
					values[d.Date] = h
					all = append(all, h)
				}
			}
		}
		if len(all) < 2*insightMinGroup {
			return Insight{}, false
		}
		sort.Float64s(all)
		median := all[len(all)/2]
		group = func(d *model.Day) (bool, bool) {
			h, ok := values[d.Date]
			return h < median, ok
		}
		labelA, labelB = "до "+clock(median), "позже"
		nextDay = evening
	default:
		return Insight{}, false
	}

	var a, b Series
	for _, d := range days {
		inA, ok := group(d)
		if !ok {
			continue
		}
		scored := d
		if nextDay {
			scored = byDate[AddDays(d.Date, 1)]
		}
		if scored == nil {
			continue
		}
		v, ok := target.Get(scored).(*int)
		if !ok || v == nil {
			continue
		}
		if inA {
			a.Add(d.Date, float64(*v))
		} else {
			b.Add(d.Date, float64(*v))
		}
	}
	if a.N() < insightMinGroup || b.N() < insightMinGroup {
		return Insight{}, false
	}
	delta := a.Avg() - b.Avg()
	if math.Abs(delta) < insightMinDelta {
		return Insight{}, false
	}
	what := strings.ToLower(target.Label)
	if nextDay {
		what += " на следующий день"
	}
	return Insight{
		Delta: delta,
		Text: fmt.Sprintf("%s: %s → %s %.1f, %s → %.1f (%+.1f; %d и %d дн.)",
			factor.Label, labelA, what, a.Avg(), labelB, b.Avg(), delta, a.N(), b.N()),
	}, true
}

// isEvening узнаёт «вечерние» показатели вроде отбоя: значения около полуночи,
// где 00:30 — это поздно, а не рано.
func isEvening(days []*model.Day, f *model.Field) bool {
	late, total := 0, 0
	for _, d := range days {
		if v, ok := f.Get(d).(*string); ok && v != nil {
			if h, ok := model.TimeToHours(*v); ok {
				total++
				if h >= 18 || h < 5 {
					late++
				}
			}
		}
	}
	return total > 0 && late*2 > total
}

func clock(hours float64) string {
	hours = math.Mod(hours, 24)
	h := int(hours)
	m := int(math.Round((hours - float64(h)) * 60))
	if m == 60 {
		h, m = (h+1)%24, 0
	}
	return fmt.Sprintf("%02d:%02d", h, m)
}
