package parse

import (
	"testing"

	"github.com/murluckk/self-reinvention/internal/model"
)

const today = "2026-08-24"

func mustInt(t *testing.T, p *int, want int) {
	t.Helper()
	if p == nil || *p != want {
		t.Fatalf("получено %v, ожидалось %d", p, want)
	}
}

func mustStr(t *testing.T, p *string, want string) {
	t.Helper()
	if p == nil || *p != want {
		t.Fatalf("получено %v, ожидалось %q", p, want)
	}
}

func mustBool(t *testing.T, p *bool, want bool) {
	t.Helper()
	if p == nil || *p != want {
		t.Fatalf("получено %v, ожидалось %v", p, want)
	}
}

func TestParseDayAllFields(t *testing.T) {
	res := ParseDay("подъем 7.30 отбой 2315 алго 60 системы 45 трен состояние 8", today)
	if len(res.Errors) != 0 {
		t.Fatalf("неожиданные ошибки: %v", res.Errors)
	}
	mustStr(t, res.Day.Wake, "07:30")
	mustStr(t, res.Day.Bed, "23:15")
	mustInt(t, res.Day.Algorithms, 60)
	mustInt(t, res.Day.SystemDesign, 45)
	mustBool(t, res.Day.Workout, true)
	mustInt(t, res.Day.Mood, 8)
}

func TestParseDayDurationsAcceptNo(t *testing.T) {
	res := ParseDay("алго нет системы 0 трен нет", today)
	if len(res.Errors) != 0 {
		t.Fatalf("неожиданные ошибки: %v", res.Errors)
	}
	mustInt(t, res.Day.Algorithms, 0)
	mustInt(t, res.Day.SystemDesign, 0)
	mustBool(t, res.Day.Workout, false)
}

func TestParseDayBareWorkoutAliases(t *testing.T) {
	for input, want := range map[string]bool{"зал": true, "бег": true, "отдых": false} {
		res := ParseDay(input, today)
		if len(res.Errors) != 0 {
			t.Fatalf("%q: %v", input, res.Errors)
		}
		mustBool(t, res.Day.Workout, want)
	}
}

func TestParseDayDateNoteAndMerge(t *testing.T) {
	dst := ParseDay("2026-08-20 подъем 7:00 алго 30 note сложный день", today).Day
	src := ParseDay("алго 60 системы 45", today).Day
	model.Merge(dst, src)
	if dst.Date != "2026-08-20" {
		t.Fatalf("дата: %s", dst.Date)
	}
	mustStr(t, dst.Wake, "07:00")
	mustInt(t, dst.Algorithms, 60)
	mustInt(t, dst.SystemDesign, 45)
	mustStr(t, dst.Note, "сложный день")
}

func TestParseDayScaleValidation(t *testing.T) {
	res := ParseDay("состояние 11", today)
	if res.Day.Mood != nil || len(res.Errors) != 1 {
		t.Fatalf("результат: day=%+v errors=%v", res.Day, res.Errors)
	}
}

func TestParseDayRejectsDurationYesWithoutMinutes(t *testing.T) {
	res := ParseDay("алго да", today)
	if res.Day.Algorithms != nil || len(res.Errors) != 1 {
		t.Fatalf("результат: day=%+v errors=%v", res.Day, res.Errors)
	}
}

func TestParseDaySvetaProfile(t *testing.T) {
	res := ParseDayFor(
		"подъем 8:00 отбой 23:30 трен прогулка учеба состояние 8 сладкое нет алкоголь нет полезное книга по психологии",
		today, "sveta",
	)
	if len(res.Errors) != 0 {
		t.Fatalf("неожиданные ошибки: %v", res.Errors)
	}
	mustStr(t, res.Day.Wake, "08:00")
	mustBool(t, res.Day.Workout, true)
	mustBool(t, res.Day.Walk, true)
	mustBool(t, res.Day.Study, true)
	mustInt(t, res.Day.Mood, 8)
	mustBool(t, res.Day.Sweet, false)
	mustBool(t, res.Day.Alcohol, false)
	mustStr(t, res.Day.Useful, "книга по психологии")
}

func TestParseDayProfilesRejectForeignFields(t *testing.T) {
	if res := ParseDayFor("алго 30", today, "sveta"); len(res.Errors) == 0 || res.Day.Algorithms != nil {
		t.Fatalf("Свете доступны поля Паши: %+v", res)
	}
	if res := ParseDayFor("прогулка", today, "pasha"); len(res.Errors) == 0 || res.Day.Walk != nil {
		t.Fatalf("Паше доступны поля Светы: %+v", res)
	}
}

func TestParseDayUsefulBareAliases(t *testing.T) {
	for input, want := range map[string]string{"литература": "литература", "ютуб": "обучающее видео"} {
		res := ParseDayFor(input, today, "sveta")
		if len(res.Errors) != 0 {
			t.Fatalf("%q: %v", input, res.Errors)
		}
		mustStr(t, res.Day.Useful, want)
	}
}
