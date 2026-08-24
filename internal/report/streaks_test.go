package report

import (
	"testing"

	"github.com/murluckk/self-reinvention/internal/model"
)

func TestStreaksBasics(t *testing.T) {
	st := ComputeStreaks(week(), "2026-08-24")
	// 21, 22, 23, 24 подряд чисто; 20-е — срыв
	if st.Clean != 4 {
		t.Fatalf("стрик «чисто» %d, ожидалось 4", st.Clean)
	}
	// английский > 0 идёт с 21-го по 24-е, 20-е — ноль
	if st.English != 4 {
		t.Fatalf("стрик английского %d, ожидалось 4", st.English)
	}
	// 22-е было полным выходным, значит серия перегруза — 23 и 24
	if st.NoDayOff != 2 {
		t.Fatalf("стрик без выходного %d, ожидалось 2", st.NoDayOff)
	}
	if st.FullDaysOff30 != 1 {
		t.Fatalf("полных выходных за 30 дней %d, ожидался 1", st.FullDaysOff30)
	}
	if st.Filled != 7 {
		t.Fatalf("заполненных подряд %d, ожидалось 7", st.Filled)
	}
}

// Незакрытый сегодняшний день не должен обнулять стрик: запись появляется
// вечером, а смотреть /s хочется днём.
func TestStreaksTodayNotYetFilled(t *testing.T) {
	st := ComputeStreaks(week(), "2026-08-25")
	if st.Clean != 4 {
		t.Fatalf("стрик «чисто» %d, ожидалось 4", st.Clean)
	}
	if st.Filled != 7 {
		t.Fatalf("заполненных подряд %d, ожидалось 7", st.Filled)
	}
}

// Пропущенный день в прошлом стрик обрывает.
func TestStreaksGapBreaks(t *testing.T) {
	days := []*model.Day{
		{Date: "2026-08-20", Clean: bp(true)},
		{Date: "2026-08-22", Clean: bp(true)},
		{Date: "2026-08-23", Clean: bp(true)},
	}
	if got := ComputeStreaks(days, "2026-08-23").Clean; got != 2 {
		t.Fatalf("стрик %d, ожидалось 2", got)
	}
}

func TestStreaksEmpty(t *testing.T) {
	st := ComputeStreaks(nil, "2026-08-24")
	if st != (Streaks{}) {
		t.Fatalf("на пустых данных ожидались нули, получено %+v", st)
	}
}

// Дни без записи считаются рабочими: выходной человек отмечает, обычный день
// может и забыть, и молчание не должно выглядеть как отдых.
func TestStreaksNoDayOffCountsUnknownDays(t *testing.T) {
	days := []*model.Day{
		{Date: "2026-08-18", DayOff: bp(true)},
		{Date: "2026-08-22", Work: f(8)},
	}
	if got := ComputeStreaks(days, "2026-08-24").NoDayOff; got != 6 {
		t.Fatalf("стрик без выходного %d, ожидалось 6 (19-24)", got)
	}
}

func TestDateHelpers(t *testing.T) {
	if got := AddDays("2026-08-24", -6); got != "2026-08-18" {
		t.Fatalf("AddDays -> %q", got)
	}
	if got := DaysBetween("2026-08-18", "2026-08-24"); got != 7 {
		t.Fatalf("DaysBetween -> %d", got)
	}
	if got := Weekday("2026-08-24"); got != "пн" {
		t.Fatalf("Weekday -> %q", got)
	}
}
