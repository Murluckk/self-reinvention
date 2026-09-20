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

// Stats — всё, что бот знает о периоде.
type Stats struct {
	From, To   string
	TotalDays  int
	FilledDays int

	Wake         Series // часы с дробной частью
	Bed          Series
	Algorithms   Series // минуты
	SystemDesign Series // минуты
	Mood         Series

	Workouts         int
	AlgorithmDays    int
	SystemDesignDays int

	Currencies map[string]*CurrencyStats
	CurOrder   []string

	Streaks Streaks
	Days    []*model.Day
	Notes   []*model.Note
	Flags   []string
}

// Input — исходные данные для отчёта.
type Input struct {
	From, To string
	Days     []*model.Day   // записи за период
	DaysAll  []*model.Day   // записи за длинное окно, для стриков
	Money    []*model.Money // деньги за период
	MoneyAll []*model.Money // все деньги по To включительно
	Notes    []*model.Note  // заметки за период
	Today    string         // точка отсчёта стриков
	Cfg      *config.Config // пороги для флагов
}

// Build считает агрегаты по периоду.
func Build(in Input) *Stats {
	s := &Stats{
		From: in.From, To: in.To,
		TotalDays:  DaysBetween(in.From, in.To),
		Currencies: map[string]*CurrencyStats{},
		Days:       in.Days,
		Notes:      in.Notes,
	}
	for _, d := range in.Days {
		if d.Empty() {
			continue
		}
		s.FilledDays++
		if d.Wake != nil {
			if h, ok := model.TimeToHours(*d.Wake); ok {
				s.Wake.Add(d.Date, h)
			}
		}
		if d.Bed != nil {
			if h, ok := model.TimeToHours(*d.Bed); ok {
				s.Bed.Add(d.Date, h)
			}
		}
		if d.Algorithms != nil {
			s.Algorithms.Add(d.Date, float64(*d.Algorithms))
			if *d.Algorithms > 0 {
				s.AlgorithmDays++
			}
		}
		if d.SystemDesign != nil {
			s.SystemDesign.Add(d.Date, float64(*d.SystemDesign))
			if *d.SystemDesign > 0 {
				s.SystemDesignDays++
			}
		}
		if d.Mood != nil {
			s.Mood.Add(d.Date, float64(*d.Mood))
		}
		if d.Workout != nil && *d.Workout {
			s.Workouts++
		}
	}
	s.Streaks = ComputeStreaks(in.DaysAll, in.Today)

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
	if s.Wake.N() > 1 && s.Wake.Spread() > cfg.MaxWakeSpreadH {
		out = append(out, fmt.Sprintf("разброс подъёма %.1f ч при пороге %.1f", s.Wake.Spread(), cfg.MaxWakeSpreadH))
	}
	if rate, ok := s.Main().SavingsRate(); ok && rate < cfg.MinSavingsRate {
		out = append(out, fmt.Sprintf("норма сбережений %.0f%% при норме ≥%.0f%%", rate*100, cfg.MinSavingsRate*100))
	}
	return out
}
