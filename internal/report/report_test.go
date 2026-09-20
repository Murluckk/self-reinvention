package report

import (
	"strings"
	"testing"

	"github.com/murluckk/self-reinvention/internal/config"
	"github.com/murluckk/self-reinvention/internal/model"
)

func ip(v int) *int       { return &v }
func bp(v bool) *bool     { return &v }
func sp(v string) *string { return &v }

func trackerDays() []*model.Day {
	return []*model.Day{
		{Date: "2026-08-18", Wake: sp("07:00"), Bed: sp("23:30"), Algorithms: ip(40), SystemDesign: ip(30), Workout: bp(true), Mood: ip(8)},
		{Date: "2026-08-19", Wake: sp("07:30"), Bed: sp("23:45"), Algorithms: ip(30), SystemDesign: ip(45), Workout: bp(true), Mood: ip(6)},
		{Date: "2026-08-20", Wake: sp("08:00"), Bed: sp("00:15"), Algorithms: ip(0), SystemDesign: ip(0), Workout: bp(false), Mood: ip(5)},
		{Date: "2026-08-21", Wake: sp("07:15"), Bed: sp("23:20"), Algorithms: ip(45), SystemDesign: ip(30), Workout: bp(true), Mood: ip(8)},
		{Date: "2026-08-22", Wake: sp("09:30"), Bed: sp("00:30"), Algorithms: ip(20), SystemDesign: ip(0), Workout: bp(false), Mood: ip(9)},
		{Date: "2026-08-23", Wake: sp("07:45"), Bed: sp("23:50"), Algorithms: ip(60), SystemDesign: ip(40), Workout: bp(true), Mood: ip(8)},
		{Date: "2026-08-24", Wake: sp("07:00"), Bed: sp("23:10"), Algorithms: ip(35), SystemDesign: ip(30), Workout: bp(true), Mood: ip(7)},
	}
}

func buildTrackerStats() *Stats {
	days := trackerDays()
	return Build(Input{
		From: "2026-08-18", To: "2026-08-24", Today: "2026-08-24",
		Days: days, DaysAll: days,
		Money: []*model.Money{
			{Date: "2026-08-19", Kind: model.MoneyIncome, Amount: 250000, Currency: "RUB", Category: "зарплата"},
			{Date: "2026-08-19", Kind: model.MoneyExpense, Amount: 1200, Currency: "RUB", Category: "еда"},
			{Date: "2026-08-19", Kind: model.MoneySaving, Amount: 150000, Currency: "RUB", Category: "накопления"},
		},
		MoneyAll: []*model.Money{
			{Date: "2026-08-19", Kind: model.MoneyIncome, Amount: 250000, Currency: "RUB"},
			{Date: "2026-08-19", Kind: model.MoneyExpense, Amount: 1200, Currency: "RUB"},
			{Date: "2026-08-19", Kind: model.MoneySaving, Amount: 150000, Currency: "RUB"},
		},
		Notes: []*model.Note{{Date: "2026-08-19", Tag: "идея", Text: "заниматься утром"}},
		Cfg:   &config.Config{MaxWakeSpreadH: 1.5, MinSavingsRate: .55},
	})
}

func TestBuildTrackerStats(t *testing.T) {
	st := buildTrackerStats()
	if st.FilledDays != 7 || st.Workouts != 5 {
		t.Fatalf("заполнено=%d тренировки=%d", st.FilledDays, st.Workouts)
	}
	if st.Algorithms.Sum() != 230 || st.AlgorithmDays != 6 {
		t.Fatalf("алгоритмы: sum=%.0f days=%d", st.Algorithms.Sum(), st.AlgorithmDays)
	}
	if st.SystemDesign.Sum() != 175 || st.SystemDesignDays != 5 {
		t.Fatalf("системный дизайн: sum=%.0f days=%d", st.SystemDesign.Sum(), st.SystemDesignDays)
	}
	if st.Mood.Avg() != 51.0/7 {
		t.Fatalf("состояние: %.2f", st.Mood.Avg())
	}
	if st.Streaks.Algorithms != 4 || st.Streaks.Workout != 2 || st.Streaks.Filled != 7 {
		t.Fatalf("стрики: %+v", st.Streaks)
	}
	rub := st.Currencies["RUB"]
	if rub == nil || rub.Income != 250000 || rub.Expense != 1200 || rub.Saved != 150000 || rub.Capital != 150000 {
		t.Fatalf("финансы: %+v", rub)
	}
}

func TestMarkdownUsesSimplifiedSchema(t *testing.T) {
	md := Markdown(buildTrackerStats())
	for _, want := range []string{"## Режим", "## Развитие", "Алгоритмы", "Системный дизайн", "## Тренировки", "## Деньги", "заниматься утром"} {
		if !strings.Contains(md, want) {
			t.Errorf("нет %q:\n%s", want, md)
		}
	}
	for _, removed := range []string{"## Английский", "## Вес", "## Чистые дни"} {
		if strings.Contains(md, removed) {
			t.Errorf("остался удалённый раздел %q", removed)
		}
	}
}

func TestStatusContainsCapitalAndLearning(t *testing.T) {
	st := buildTrackerStats()
	text := Status(st.Days[len(st.Days)-1], st)
	for _, want := range []string{"Капитал", "алгоритмы", "системный дизайн", "Состояние"} {
		if !strings.Contains(text, want) {
			t.Errorf("нет %q:\n%s", want, text)
		}
	}
}
