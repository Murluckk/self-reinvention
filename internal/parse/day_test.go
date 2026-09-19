package parse

import (
	"testing"

	"github.com/murluckk/self-reinvention/internal/model"
)

const today = "2026-08-24"

func mustFloat(t *testing.T, p *float64, want float64) {
	t.Helper()
	if p == nil {
		t.Fatalf("поле не заполнено, ожидалось %v", want)
	}
	if *p != want {
		t.Fatalf("получено %v, ожидалось %v", *p, want)
	}
}

func mustInt(t *testing.T, p *int, want int) {
	t.Helper()
	if p == nil {
		t.Fatalf("поле не заполнено, ожидалось %v", want)
	}
	if *p != want {
		t.Fatalf("получено %v, ожидалось %v", *p, want)
	}
}

func mustStr(t *testing.T, p *string, want string) {
	t.Helper()
	if p == nil {
		t.Fatalf("поле не заполнено, ожидалось %q", want)
	}
	if *p != want {
		t.Fatalf("получено %q, ожидалось %q", *p, want)
	}
}

func mustBool(t *testing.T, p *bool, want bool) {
	t.Helper()
	if p == nil {
		t.Fatalf("поле не заполнено, ожидалось %v", want)
	}
	if *p != want {
		t.Fatalf("получено %v, ожидалось %v", *p, want)
	}
}

func TestParseDayEqualsSyntax(t *testing.T) {
	res := ParseDay("сон=7.5 трен=зал англ=40 чисто=да", today)
	if len(res.Errors) != 0 {
		t.Fatalf("неожиданные ошибки: %v", res.Errors)
	}
	if res.Day.Date != today {
		t.Fatalf("дата %q, ожидалась %q", res.Day.Date, today)
	}
	mustFloat(t, res.Day.Sleep, 7.5)
	mustStr(t, res.Day.Workout, "зал")
	mustInt(t, res.Day.English, 40)
	mustBool(t, res.Day.Clean, true)
}

func TestParseDaySpaceSyntaxAndUnits(t *testing.T) {
	res := ParseDay("сон 7,5ч вес 73,4кг англ 40мин", today)
	if len(res.Errors) != 0 {
		t.Fatalf("неожиданные ошибки: %v", res.Errors)
	}
	mustFloat(t, res.Day.Sleep, 7.5)
	mustFloat(t, res.Day.Weight, 73.4)
	mustInt(t, res.Day.English, 40)
}

func TestParseDayBareBoolFlags(t *testing.T) {
	res := ParseDay("чисто готовил выходной", today)
	if len(res.Errors) != 0 {
		t.Fatalf("неожиданные ошибки: %v", res.Errors)
	}
	mustBool(t, res.Day.Clean, true)
	mustBool(t, res.Day.Cooked, true)
	mustBool(t, res.Day.DayOff, true)
}

func TestParseDayBoolForms(t *testing.T) {
	cases := map[string]bool{
		"чисто 1": true, "чисто 0": false,
		"чисто да": true, "чисто нет": false,
		"чисто +": true, "чисто -": false,
		"чисто y": true, "чисто n": false,
		"чисто true": true, "чисто false": false,
	}
	for in, want := range cases {
		res := ParseDay(in, today)
		if len(res.Errors) != 0 {
			t.Fatalf("%q: неожиданные ошибки: %v", in, res.Errors)
		}
		mustBool(t, res.Day.Clean, want)
	}
}

func TestParseDayExplicitDate(t *testing.T) {
	res := ParseDay("2026-08-20 сон 6", today)
	if res.Day.Date != "2026-08-20" {
		t.Fatalf("дата %q, ожидалась 2026-08-20", res.Day.Date)
	}
	mustFloat(t, res.Day.Sleep, 6)
}

func TestParseDayNoteTakesRest(t *testing.T) {
	res := ParseDay("сон 7 note было тяжело, но норм сон 3", today)
	if len(res.Errors) != 0 {
		t.Fatalf("неожиданные ошибки: %v", res.Errors)
	}
	mustFloat(t, res.Day.Sleep, 7)
	mustStr(t, res.Day.Note, "было тяжело, но норм сон 3")
}

func TestParseDayTimeNormalization(t *testing.T) {
	res := ParseDay("подъем 7.30 отбой 2315", today)
	if len(res.Errors) != 0 {
		t.Fatalf("неожиданные ошибки: %v", res.Errors)
	}
	mustStr(t, res.Day.Wake, "07:30")
	mustStr(t, res.Day.Bed, "23:15")
}

func TestParseDayReportsErrors(t *testing.T) {
	res := ParseDay("сон абв фигня 5 фокус 11", today)
	if len(res.Errors) != 3 {
		t.Fatalf("ожидал три претензии, получил %v", res.Errors)
	}
	if res.Day.Sleep != nil {
		t.Fatal("испорченное значение не должно записываться")
	}
}

func TestParseDayEmpty(t *testing.T) {
	res := ParseDay("", today)
	if len(res.Errors) == 0 {
		t.Fatal("пустая команда должна давать понятную ошибку")
	}
}

func TestParseDayEnglishKeys(t *testing.T) {
	res := ParseDay("sleep 8 english 30 clean yes weight 72", today)
	if len(res.Errors) != 0 {
		t.Fatalf("неожиданные ошибки: %v", res.Errors)
	}
	mustFloat(t, res.Day.Sleep, 8)
	mustInt(t, res.Day.English, 30)
	mustBool(t, res.Day.Clean, true)
	mustFloat(t, res.Day.Weight, 72)
}

func TestMergeKeepsExistingValues(t *testing.T) {
	dst := ParseDay("сон 7 англ 40", today).Day
	src := ParseDay("англ 60 вес 73", today).Day
	model.Merge(dst, src)
	mustFloat(t, dst.Sleep, 7)
	mustInt(t, dst.English, 60)
	mustFloat(t, dst.Weight, 73)
}

func TestScaleValidation(t *testing.T) {
	res := ParseDay("фокус 10 настроение 11 энергия 0", today)
	mustInt(t, res.Day.Focus, 10)
	if res.Day.Mood != nil {
		t.Fatal("11 вне шкалы 1..10 записываться не должен")
	}
	if res.Day.Energy != nil {
		t.Fatal("0 вне шкалы 1..10 записываться не должен")
	}
	if len(res.Errors) != 2 {
		t.Fatalf("ожидал две претензии, получил %v", res.Errors)
	}
}
