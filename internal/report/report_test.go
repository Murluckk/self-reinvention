package report

import (
	"strings"
	"testing"

	"github.com/murluckk/self-reinvention/internal/config"
	"github.com/murluckk/self-reinvention/internal/model"
)

func f(v float64) *float64 { return &v }
func i(v int) *int         { return &v }
func s(v string) *string   { return &v }
func bp(v bool) *bool      { return &v }

func testCfg() *config.Config {
	return &config.Config{
		MinFullDaysOff30:    4,
		MaxStreakNoDayOff:   12,
		MaxEnglishSkipsWeek: 1,
		MinSleepAvg:         7.0,
		MaxWakeSpreadH:      1.5,
		MinProteinG:         110,
		MinSavingsRate:      0.55,
	}
}

// week строит семь дней 2026-08-18..2026-08-24 с предсказуемыми значениями.
func week() []*model.Day {
	return []*model.Day{
		{Date: "2026-08-18", Sleep: f(7), Wake: s("07:00"), Workout: s("зал"), English: i(40), Kcal: i(2100), Protein: i(120), Work: f(8), Clean: bp(true), Weight: f(74), Telegram: i(3), Focus: i(4), Mood: i(4), Energy: i(4), Cooked: bp(true)},
		{Date: "2026-08-19", Sleep: f(6), Wake: s("07:30"), Workout: s("бег"), English: i(30), Kcal: i(1900), Protein: i(100), Work: f(9), Clean: bp(true), Telegram: i(5), Focus: i(3), Mood: i(3), Energy: i(3)},
		{Date: "2026-08-20", Sleep: f(8), Wake: s("08:00"), Workout: s("нет"), English: i(0), Kcal: i(2300), Protein: i(90), Work: f(7), Clean: bp(false), Telegram: i(8), Focus: i(2), Mood: i(2), Energy: i(2)},
		{Date: "2026-08-21", Sleep: f(7.5), Wake: s("07:15"), Workout: s("зал"), English: i(45), Protein: i(130), Work: f(8), Clean: bp(true), Shift: bp(true), Focus: i(4), Mood: i(4), Energy: i(5), Cooked: bp(true)},
		{Date: "2026-08-22", Sleep: f(9), Wake: s("09:30"), English: i(20), Work: f(0), DayOff: bp(true), Clean: bp(true), Focus: i(5), Mood: i(5), Energy: i(5)},
		{Date: "2026-08-23", Sleep: f(7), Wake: s("07:45"), Workout: s("улица"), English: i(60), Protein: i(140), Work: f(4), Clean: bp(true), Weight: f(73.2), Cooked: bp(true)},
		{Date: "2026-08-24", Sleep: f(6.5), Wake: s("07:00"), Workout: s("зал"), English: i(35), Work: f(8), Clean: bp(true), Telegram: i(2)},
	}
}

func build(t *testing.T, days []*model.Day, money []*model.Money, notes []*model.Note) *Stats {
	t.Helper()
	return Build(Input{
		From: "2026-08-18", To: "2026-08-24",
		Days: days, DaysAll: days,
		Money: money, MoneyAll: money,
		Notes: notes,
		Today: "2026-08-24",
		Cfg:   testCfg(),
	})
}

func TestAggregateSleepAndWake(t *testing.T) {
	st := build(t, week(), nil, nil)
	if st.FilledDays != 7 || st.TotalDays != 7 {
		t.Fatalf("заполнено %d из %d", st.FilledDays, st.TotalDays)
	}
	if got := st.Sleep.Avg(); got < 7.28 || got > 7.30 {
		t.Fatalf("средний сон %.3f", got)
	}
	if mn, d := st.Sleep.Min(); mn != 6 || d != "2026-08-19" {
		t.Fatalf("минимум сна %.1f (%s)", mn, d)
	}
	if mx, d := st.Sleep.Max(); mx != 9 || d != "2026-08-22" {
		t.Fatalf("максимум сна %.1f (%s)", mx, d)
	}
	if got := st.Wake.Spread(); got < 2.49 || got > 2.51 {
		t.Fatalf("разброс подъёма %.3f, ожидалось 2.5", got)
	}
}

func TestAggregateWorkoutsAndEnglish(t *testing.T) {
	st := build(t, week(), nil, nil)
	if st.Workouts != 5 {
		t.Fatalf("тренировок %d, ожидалось 5 (день «нет» не считается)", st.Workouts)
	}
	if st.WorkoutTypes["зал"] != 3 || st.WorkoutTypes["бег"] != 1 || st.WorkoutTypes["улица"] != 1 {
		t.Fatalf("разбивка по типам: %v", st.WorkoutTypes)
	}
	if st.English.Sum() != 230 {
		t.Fatalf("минут английского %.0f, ожидалось 230", st.English.Sum())
	}
	if st.EnglishDays != 6 {
		t.Fatalf("дней с английским %d, ожидалось 6", st.EnglishDays)
	}
	if st.EnglishSkips != 1 {
		t.Fatalf("пропусков %d, ожидался 1", st.EnglishSkips)
	}
}

func TestAggregateWorkAndClean(t *testing.T) {
	st := build(t, week(), nil, nil)
	if st.Work.Sum() != 44 {
		t.Fatalf("часов работы %.1f, ожидалось 44", st.Work.Sum())
	}
	if st.Shifts != 1 || st.DaysOff != 1 {
		t.Fatalf("смен %d, выходных %d", st.Shifts, st.DaysOff)
	}
	// 18-21 подряд без выходного, 22-е — полный выходной, дальше 23-24
	if st.MaxNoDayOff != 4 {
		t.Fatalf("максимальная серия без выходного %d, ожидалось 4", st.MaxNoDayOff)
	}
	if st.CleanDays != 6 || st.CleanKnown != 7 {
		t.Fatalf("чистых %d из %d", st.CleanDays, st.CleanKnown)
	}
	if len(st.CleanFails) != 1 || st.CleanFails[0] != "2026-08-20" {
		t.Fatalf("срывы: %v", st.CleanFails)
	}
}

func TestAggregateWeightDelta(t *testing.T) {
	st := build(t, week(), nil, nil)
	if got := st.Weight.Delta(); got < -0.81 || got > -0.79 {
		t.Fatalf("дельта веса %.2f, ожидалось -0.8", got)
	}
}

func TestAggregateNutrition(t *testing.T) {
	st := build(t, week(), nil, nil)
	if got := st.Kcal.Avg(); got != 2100 {
		t.Fatalf("средние ккал %.0f", got)
	}
	if got := st.Protein.Avg(); got != 116 {
		t.Fatalf("средний белок %.0f", got)
	}
	if st.CookedDays != 3 {
		t.Fatalf("дней готовки %d", st.CookedDays)
	}
}

func TestAggregateMoney(t *testing.T) {
	money := []*model.Money{
		{Date: "2026-08-18", Kind: model.MoneyIncome, Amount: 250000, Currency: "RUB", Category: "зп"},
		{Date: "2026-08-19", Kind: model.MoneyExpense, Amount: 1200, Currency: "RUB", Category: "еда"},
		{Date: "2026-08-19", Kind: model.MoneySaving, Amount: 150000, Currency: "RUB", Category: "накопления"},
		{Date: "2026-08-20", Kind: model.MoneySaving, Amount: 5000, Currency: "USD", Category: "кошелёк"},
	}
	st := build(t, week(), money, nil)
	rub := st.Currencies["RUB"]
	if rub.Income != 250000 || rub.Expense != 1200 || rub.Saved != 150000 || rub.Capital != 150000 {
		t.Fatalf("рубли: %+v", rub)
	}
	rate, ok := rub.SavingsRate()
	if !ok || rate < 0.59 || rate > 0.61 {
		t.Fatalf("норма сбережений %.3f", rate)
	}
	if st.Currencies["USD"].Capital != 5000 {
		t.Fatalf("доллары: %+v", st.Currencies["USD"])
	}
	if st.CurOrder[0] != "RUB" {
		t.Fatalf("порядок валют %v, рубли должны идти первыми", st.CurOrder)
	}
}

func TestFlags(t *testing.T) {
	st := build(t, week(), nil, nil)
	joined := strings.Join(st.Flags, "\n")
	for _, want := range []string{"полных выходных за 30 дней", "разброс подъёма", "чистых дней"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("ожидал флаг %q, получил:\n%s", want, joined)
		}
	}
	// сон 7.29 и белок 116 в норме — флагов по ним быть не должно
	for _, unwanted := range []string{"средний сон", "средний белок", "пропусков английского"} {
		if strings.Contains(joined, unwanted) {
			t.Fatalf("лишний флаг %q:\n%s", unwanted, joined)
		}
	}
}

func TestFlagsSavingsRate(t *testing.T) {
	money := []*model.Money{
		{Date: "2026-08-18", Kind: model.MoneyIncome, Amount: 100000, Currency: "RUB", Category: "зп"},
		{Date: "2026-08-19", Kind: model.MoneySaving, Amount: 10000, Currency: "RUB", Category: "накопления"},
	}
	st := build(t, week(), money, nil)
	if !strings.Contains(strings.Join(st.Flags, "\n"), "норма сбережений 10%") {
		t.Fatalf("ожидал флаг по норме сбережений, получил %v", st.Flags)
	}
}

func TestMarkdownIsSelfContained(t *testing.T) {
	notes := []*model.Note{
		{Date: "2026-08-19", Tag: "идея", Text: "перенести тренировки на утро"},
		{Date: "2026-08-20", Text: "тяжёлый день"},
	}
	md := Markdown(build(t, week(), nil, notes))
	for _, want := range []string{
		"# Разбор: 2026-08-18 — 2026-08-24",
		"## Сон", "## Тренировки", "## Английский", "## Питание", "## Вес",
		"## Работа", "## Чистые дни", "## Телеграм вне окон", "## Состояние",
		"## Деньги", "## Стрики", "## Флаги", "## По дням", "## Заметки",
		"перенести тренировки на утро", "тяжёлый день", "#идея",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("в выгрузке нет %q", want)
		}
	}
}
