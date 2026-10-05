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
		Notes:   []*model.Note{{Date: "2026-08-19", Tag: "идея", Text: "заниматься утром"}},
		Cfg:     &config.Config{MaxWakeSpreadH: 1.5, MinSavingsRate: .55},
		Fields:  preset("pasha"),
		Finance: true,
	})
}

func preset(name string) model.FieldSet {
	p, _ := model.PresetByName(name)
	return p.Fields
}

func TestBuildTrackerStats(t *testing.T) {
	st := buildTrackerStats()
	if st.FilledDays != 7 || st.Field("workout").Yes != 5 {
		t.Fatalf("заполнено=%d тренировки=%d", st.FilledDays, st.Field("workout").Yes)
	}
	if a := st.Field("algorithms"); a.Series.Sum() != 230 || a.Yes != 6 {
		t.Fatalf("алгоритмы: sum=%.0f days=%d", a.Series.Sum(), a.Yes)
	}
	if sd := st.Field("system_design"); sd.Series.Sum() != 175 || sd.Yes != 5 {
		t.Fatalf("системный дизайн: sum=%.0f days=%d", sd.Series.Sum(), sd.Yes)
	}
	if st.Field("mood").Series.Avg() != 51.0/7 {
		t.Fatalf("состояние: %.2f", st.Field("mood").Series.Avg())
	}
	if st.Streaks.Of("algorithms") != 4 || st.Streaks.Of("workout") != 2 || st.Streaks.Filled != 7 {
		t.Fatalf("стрики: %+v", st.Streaks)
	}
	rub := st.Currencies["RUB"]
	if rub == nil || rub.Income != 250000 || rub.Expense != 1200 || rub.Saved != 150000 || rub.Capital != 150000 {
		t.Fatalf("финансы: %+v", rub)
	}
}

func TestMarkdownUsesSimplifiedSchema(t *testing.T) {
	md := Markdown(buildTrackerStats())
	for _, want := range []string{"## Режим", "## Привычки и занятия", "алгоритмы", "системный дизайн", "тренировка", "## Деньги", "заниматься утром", "| Алгоритмы |"} {
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
	for _, want := range []string{"Капитал", "алгоритмы", "системный дизайн", "состояние"} {
		if !strings.Contains(text, want) {
			t.Errorf("нет %q:\n%s", want, text)
		}
	}
}

func TestSvetaProfileReport(t *testing.T) {
	days := []*model.Day{
		{Date: "2026-08-22", Wake: sp("08:00"), Workout: bp(true), Walk: bp(true), Study: bp(true), Useful: sp("литература"), Mood: ip(8), Sweet: bp(false), Alcohol: bp(false)},
		{Date: "2026-08-23", Wake: sp("08:30"), Workout: bp(false), Walk: bp(true), Study: bp(true), Useful: sp("обучающее видео"), Mood: ip(7), Sweet: bp(true), Alcohol: bp(false)},
		{Date: "2026-08-24", Wake: sp("08:15"), Workout: bp(true), Walk: bp(true), Study: bp(false), Useful: sp("книга"), Mood: ip(9), Sweet: bp(false), Alcohol: bp(true)},
	}
	st := Build(Input{
		From: "2026-08-22", To: "2026-08-24", Today: "2026-08-24",
		Days: days, DaysAll: days, Fields: preset("sveta"),
		Cfg: &config.Config{MaxWakeSpreadH: 1.5, MinSavingsRate: .55},
	})
	if st.FilledDays != 3 || st.Field("walk").Yes != 3 || st.Field("study").Yes != 2 || st.Field("useful").Yes != 3 ||
		st.Field("sweet").Yes != 1 || st.Field("sweet").Known != 3 || st.Field("alcohol").Yes != 1 || st.Field("alcohol").Known != 3 {
		t.Fatalf("статистика Светы: %+v", st)
	}
	if st.Streaks.Of("walk") != 3 || st.Streaks.Of("study") != 0 || st.Streaks.Of("useful") != 3 {
		t.Fatalf("стрики Светы: %+v", st.Streaks)
	}
	md := Markdown(st)
	for _, want := range []string{"## Привычки и занятия", "прогулка", "учёба", "## Ограничения", "сладкое", "алкоголь", "Полезное занятие"} {
		if !strings.Contains(md, want) {
			t.Errorf("нет %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "## Деньги") || strings.Contains(md, "алгоритмы") {
		t.Fatalf("в отчёт Светы попал профиль Паши:\n%s", md)
	}
	status := Status(days[len(days)-1], st)
	if strings.Contains(status, "Капитал") || !strings.Contains(status, "прогулка: 3 из 3") {
		t.Fatalf("неверный статус Светы:\n%s", status)
	}
}

func TestInsightsFindWorkoutAndNextDayEffects(t *testing.T) {
	var days []*model.Day
	// Чередуем: в дни с тренировкой состояние 8, без неё 5; алкоголь портит
	// следующий день.
	for i := 0; i < 12; i++ {
		date := AddDays("2026-08-01", i)
		workout := i%2 == 0
		mood := 5
		if workout {
			mood = 8
		}
		days = append(days, &model.Day{Date: date, Workout: bp(workout), Mood: ip(mood)})
	}
	found := FindInsights(days, preset("pasha"))
	if len(found) == 0 || !strings.Contains(found[0].Text, "Тренировка: да → состояние 8.0, нет → 5.0") {
		t.Fatalf("нет наблюдения про тренировку: %+v", found)
	}

	var next []*model.Day
	for i := 0; i < 12; i++ {
		alcohol := i%3 == 0
		next = append(next, &model.Day{Date: AddDays("2026-08-01", i), Alcohol: bp(alcohol), Mood: ip(7)})
	}
	for i := 1; i < 12; i++ {
		if *next[i-1].Alcohol {
			next[i].Mood = ip(4)
		}
	}
	found = FindInsights(next, preset("sveta"))
	if len(found) == 0 || !strings.Contains(found[0].Text, "состояние на следующий день") {
		t.Fatalf("нет наблюдения про следующий день: %+v", found)
	}
}

func TestInsightsNeedEnoughData(t *testing.T) {
	days := []*model.Day{
		{Date: "2026-08-01", Workout: bp(true), Mood: ip(9)},
		{Date: "2026-08-02", Workout: bp(false), Mood: ip(3)},
	}
	if found := FindInsights(days, preset("pasha")); len(found) != 0 {
		t.Fatalf("на двух днях не должно быть выводов: %+v", found)
	}
}

func TestBedTimeAverageCrossesMidnight(t *testing.T) {
	st := Build(Input{
		From: "2026-08-01", To: "2026-08-02", Today: "2026-08-02", Fields: preset("basic"),
		Days: []*model.Day{
			{Date: "2026-08-01", Bed: sp("23:30")},
			{Date: "2026-08-02", Bed: sp("00:30")},
		},
	})
	if got := st.Field("bed").Value(); got != "00:00" {
		t.Fatalf("среднее засыпание %q, ожидалось 00:00", got)
	}
}
