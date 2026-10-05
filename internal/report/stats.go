package report

import (
	"fmt"
	"sort"

	"github.com/murluckk/self-reinvention/internal/config"
	"github.com/murluckk/self-reinvention/internal/model"
)

// Series — набор чисел с ленивой статистикой; nil-значения в него просто не
// попадают, поэтому «нет данных» не превращается в ноль.
type Series struct {
	Values []float64
	Dates  []string
}

// Add добавляет значение.
func (s *Series) Add(date string, v float64) {
	s.Values = append(s.Values, v)
	s.Dates = append(s.Dates, date)
}

// N — количество значений.
func (s *Series) N() int { return len(s.Values) }

// Sum — сумма.
func (s *Series) Sum() float64 {
	var t float64
	for _, v := range s.Values {
		t += v
	}
	return t
}

// Avg — среднее; 0 при пустой серии.
func (s *Series) Avg() float64 {
	if s.N() == 0 {
		return 0
	}
	return s.Sum() / float64(s.N())
}

// Min возвращает минимум и дату, когда он случился.
func (s *Series) Min() (float64, string) {
	if s.N() == 0 {
		return 0, ""
	}
	i := 0
	for j, v := range s.Values {
		if v < s.Values[i] {
			i = j
		}
	}
	return s.Values[i], s.Dates[i]
}

// Max возвращает максимум и дату.
func (s *Series) Max() (float64, string) {
	if s.N() == 0 {
		return 0, ""
	}
	i := 0
	for j, v := range s.Values {
		if v > s.Values[i] {
			i = j
		}
	}
	return s.Values[i], s.Dates[i]
}

// Spread — разброс между максимумом и минимумом.
func (s *Series) Spread() float64 {
	if s.N() == 0 {
		return 0
	}
	mn, _ := s.Min()
	mx, _ := s.Max()
	return mx - mn
}

// Delta — разница между последним и первым значением.
func (s *Series) Delta() float64 {
	if s.N() < 2 {
		return 0
	}
	return s.Values[s.N()-1] - s.Values[0]
}

// HalfDelta — динамика внутри периода: среднее второй половины минус среднее
// первой. Показывает направление, а не шум одного дня.
func (s *Series) HalfDelta() float64 {
	if s.N() < 4 {
		return 0
	}
	h := s.N() / 2
	var a, b float64
	for _, v := range s.Values[:h] {
		a += v
	}
	for _, v := range s.Values[h:] {
		b += v
	}
	return b/float64(s.N()-h) - a/float64(h)
}

// CurrencyStats — деньги в одной валюте.
type CurrencyStats struct {
	Income  float64
	Expense float64
	Saved   float64
	Capital float64 // накопительный итог отложенного за всё время
}

// SavingsRate — доля дохода, ушедшая в накопления.
func (c *CurrencyStats) SavingsRate() (float64, bool) {
	if c.Income <= 0 {
		return 0, false
	}
	return c.Saved / c.Income, true
}

// FieldStat — агрегат по одному показателю за период.
type FieldStat struct {
	Field  model.Field
	Series Series // числовые значения: минуты, оценки, часы суток
	Yes    int    // дней «да»: занимался, отметил, заполнил
	Known  int    // дней, когда показатель вообще отмечен
}

// Stats — всё, что бот знает о периоде.
type Stats struct {
	From, To   string
	Fields     model.FieldSet
	Finance    bool
	TotalDays  int
	FilledDays int

	Order   []model.Field // показатели пользователя в порядке каталога
	ByField map[string]*FieldStat

	Currencies map[string]*CurrencyStats
	CurOrder   []string

	Streaks  Streaks
	Days     []*model.Day
	Notes    []*model.Note
	Flags    []string
	Insights []Insight
}

// Field возвращает агрегат по колонке; для чужой колонки — пустой.
func (s *Stats) Field(col string) *FieldStat {
	if fs, ok := s.ByField[col]; ok {
		return fs
	}
	f, _ := model.FieldByColumn(col)
	if f == nil {
		return &FieldStat{}
	}
	return &FieldStat{Field: *f}
}

// Has сообщает, отслеживает ли пользователь показатель.
func (s *Stats) Has(col string) bool { _, ok := s.ByField[col]; return ok }

// Input — исходные данные для отчёта.
type Input struct {
	From, To string
	Days     []*model.Day   // записи за период
	DaysAll  []*model.Day   // записи за длинное окно, для стриков и наблюдений
	Money    []*model.Money // деньги за период
	MoneyAll []*model.Money // все деньги по To включительно
	Notes    []*model.Note  // заметки за период
	Today    string         // точка отсчёта стриков
	Cfg      *config.Config // пороги для флагов
	Fields   model.FieldSet // показатели пользователя; nil — все
	Finance  bool           // показывать деньги
}

// insightWindow — сколько последних дней берём для поиска связей: за неделю
// выборки слишком малы, а годовые данные смешивают разные периоды жизни.
const insightWindow = 90

// Build считает агрегаты по периоду.
func Build(in Input) *Stats {
	s := &Stats{
		From: in.From, To: in.To, Fields: in.Fields, Finance: in.Finance,
		TotalDays:  DaysBetween(in.From, in.To),
		Order:      in.Fields.Fields(),
		ByField:    map[string]*FieldStat{},
		Currencies: map[string]*CurrencyStats{},
		Days:       in.Days,
		Notes:      in.Notes,
	}
	for _, f := range s.Order {
		s.ByField[f.DB] = &FieldStat{Field: f}
	}
	for _, d := range in.Days {
		if d.EmptyIn(in.Fields) {
			continue
		}
		s.FilledDays++
		for _, f := range s.Order {
			if f.IsSet(d) {
				s.ByField[f.DB].add(d)
			}
		}
	}
	s.Streaks = ComputeStreaksIn(in.DaysAll, in.Today, in.Fields)
	s.Insights = FindInsights(lastDays(in.DaysAll, in.To, insightWindow), in.Fields)

	if in.Finance {
		for _, m := range in.Money {
			c := s.cur(m.Currency)
			switch m.Kind {
			case model.MoneyIncome:
				c.Income += m.Amount
			case model.MoneyExpense:
				c.Expense += m.Amount
			case model.MoneySaving:
				c.Saved += m.Amount
			}
		}
		for _, m := range in.MoneyAll {
			if m.Kind == model.MoneySaving {
				s.cur(m.Currency).Capital += m.Amount
			}
		}
	}
	s.CurOrder = make([]string, 0, len(s.Currencies))
	for k := range s.Currencies {
		s.CurOrder = append(s.CurOrder, k)
	}
	sort.Slice(s.CurOrder, func(i, j int) bool {
		if s.CurOrder[i] == "RUB" {
			return true
		}
		if s.CurOrder[j] == "RUB" {
			return false
		}
		return s.CurOrder[i] < s.CurOrder[j]
	})

	if in.Cfg != nil {
		s.Flags = flags(s, in.Cfg)
	}
	return s
}

func (fs *FieldStat) add(d *model.Day) {
	f := &fs.Field
	fs.Known++
	if done := f.Done(d); done != nil && *done {
		fs.Yes++
	}
	switch v := f.Get(d).(type) {
	case *int:
		fs.Series.Add(d.Date, float64(*v))
	case *float64:
		fs.Series.Add(d.Date, *v)
	case *string:
		if f.Kind == model.KindTime {
			if h, ok := model.TimeToHours(*v); ok {
				fs.Series.Add(d.Date, h)
			}
		}
	}
}

// lastDays оставляет записи за n дней по to включительно.
func lastDays(days []*model.Day, to string, n int) []*model.Day {
	from := AddDays(to, -(n - 1))
	var out []*model.Day
	for _, d := range days {
		if d.Date >= from && d.Date <= to {
			out = append(out, d)
		}
	}
	return out
}

func (s *Stats) cur(code string) *CurrencyStats {
	if code == "" {
		code = "RUB"
	}
	c, ok := s.Currencies[code]
	if !ok {
		c = &CurrencyStats{}
		s.Currencies[code] = c
	}
	return c
}

// Main возвращает статистику по основной валюте (рубли, если они вообще есть).
func (s *Stats) Main() *CurrencyStats {
	if c, ok := s.Currencies["RUB"]; ok {
		return c
	}
	for _, k := range s.CurOrder {
		return s.Currencies[k]
	}
	return &CurrencyStats{}
}

// flags — автоматические предупреждения по порогам из конфига. Смысл в том,
// чтобы не перечитывать цифры глазами каждую неделю: правила зафиксированы
// один раз и срабатывают сами.
func flags(s *Stats, cfg *config.Config) []string {
	var out []string
	if wake := s.Field("wake").Series; s.Has("wake") && wake.N() > 1 && wake.Spread() > cfg.MaxWakeSpreadH {
		out = append(out, fmt.Sprintf("разброс подъёма %.1f ч при пороге %.1f", wake.Spread(), cfg.MaxWakeSpreadH))
	}
	if s.Finance {
		if rate, ok := s.Main().SavingsRate(); ok && rate < cfg.MinSavingsRate {
			out = append(out, fmt.Sprintf("норма сбережений %.0f%% при норме ≥%.0f%%", rate*100, cfg.MinSavingsRate*100))
		}
	}
	return out
}
