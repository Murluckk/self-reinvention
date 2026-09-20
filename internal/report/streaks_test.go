package report

import (
	"testing"

	"github.com/murluckk/self-reinvention/internal/model"
)

func TestStreaksBasics(t *testing.T) {
	st := ComputeStreaks(trackerDays(), "2026-08-24")
	if st.Algorithms != 4 || st.SystemDesign != 2 || st.Workout != 2 || st.Filled != 7 {
		t.Fatalf("стрики: %+v", st)
	}
}

func TestStreaksTodayNotYetFilled(t *testing.T) {
	st := ComputeStreaks(trackerDays(), "2026-08-25")
	if st.Algorithms != 4 || st.Filled != 7 {
		t.Fatalf("стрики: %+v", st)
	}
}

func TestStreaksGapBreaks(t *testing.T) {
	days := []*model.Day{
		{Date: "2026-08-20", Algorithms: ip(30)},
		{Date: "2026-08-22", Algorithms: ip(30)},
		{Date: "2026-08-23", Algorithms: ip(30)},
	}
	if got := ComputeStreaks(days, "2026-08-23").Algorithms; got != 2 {
		t.Fatalf("стрик %d, ожидалось 2", got)
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
