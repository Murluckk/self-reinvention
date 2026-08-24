package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/murluckk/self-reinvention/internal/model"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func f(v float64) *float64 { return &v }
func i(v int) *int         { return &v }
func s(v string) *string   { return &v }
func bp(v bool) *bool      { return &v }

// Главное свойство хранилища: дописывание не затирает уже записанное.
func TestUpsertDayMergesPartialWrites(t *testing.T) {
	st := open(t)
	if err := st.UpsertDay(&model.Day{Date: "2026-08-24", Sleep: f(7.5), English: i(40)}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertDay(&model.Day{Date: "2026-08-24", English: i(60), Clean: bp(true), Workout: s("зал")}); err != nil {
		t.Fatal(err)
	}
	d, err := st.GetDay("2026-08-24")
	if err != nil {
		t.Fatal(err)
	}
	if d.Sleep == nil || *d.Sleep != 7.5 {
		t.Fatalf("сон затёрся: %v", d.Sleep)
	}
	if d.English == nil || *d.English != 60 {
		t.Fatalf("английский не обновился: %v", d.English)
	}
	if d.Clean == nil || !*d.Clean {
		t.Fatalf("чисто не записалось: %v", d.Clean)
	}
	if d.Workout == nil || *d.Workout != "зал" {
		t.Fatalf("тренировка не записалась: %v", d.Workout)
	}
}

func TestGetDayMissingReturnsEmpty(t *testing.T) {
	st := open(t)
	d, err := st.GetDay("2026-01-01")
	if err != nil {
		t.Fatal(err)
	}
	if d.Date != "2026-01-01" || !d.Empty() {
		t.Fatalf("ожидалась пустая запись, получено %+v", d)
	}
}

func TestDaysRange(t *testing.T) {
	st := open(t)
	for _, date := range []string{"2026-08-17", "2026-08-18", "2026-08-24", "2026-08-25"} {
		if err := st.UpsertDay(&model.Day{Date: date, Sleep: f(7)}); err != nil {
			t.Fatal(err)
		}
	}
	days, err := st.Days("2026-08-18", "2026-08-24")
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 2 || days[0].Date != "2026-08-18" || days[1].Date != "2026-08-24" {
		t.Fatalf("выборка: %+v", days)
	}
}

func TestUndoTakesNewestEntry(t *testing.T) {
	st := open(t)
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	if err := st.AddNote(&model.Note{TS: now, Date: "2026-08-24", Text: "мысль"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddMoney(&model.Money{TS: now.Add(time.Minute), Date: "2026-08-24",
		Kind: model.MoneyExpense, Amount: 1200, Currency: "RUB", Category: "еда"}); err != nil {
		t.Fatal(err)
	}
	u, err := st.LastUndoable()
	if err != nil {
		t.Fatal(err)
	}
	if u == nil || u.Kind != "money" {
		t.Fatalf("ожидалась денежная запись, получено %+v", u)
	}
	if err := st.Delete(u.Kind, u.ID); err != nil {
		t.Fatal(err)
	}
	u, err = st.LastUndoable()
	if err != nil {
		t.Fatal(err)
	}
	if u == nil || u.Kind != "note" {
		t.Fatalf("после отмены ожидалась заметка, получено %+v", u)
	}
	if err := st.Delete(u.Kind, u.ID); err != nil {
		t.Fatal(err)
	}
	if u, _ := st.LastUndoable(); u != nil {
		t.Fatalf("ожидалось, что отменять больше нечего, получено %+v", u)
	}
}

func TestJobsRememberLastRun(t *testing.T) {
	st := open(t)
	last, err := st.JobLastRun("daily")
	if err != nil || last != "" {
		t.Fatalf("last=%q err=%v", last, err)
	}
	if err := st.SetJobLastRun("daily", "2026-08-24"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetJobLastRun("daily", "2026-08-25"); err != nil {
		t.Fatal(err)
	}
	if last, _ := st.JobLastRun("daily"); last != "2026-08-25" {
		t.Fatalf("last=%q", last)
	}
}

func TestMoneyAndNotesByPeriod(t *testing.T) {
	st := open(t)
	now := time.Now()
	_ = st.AddMoney(&model.Money{TS: now, Date: "2026-08-17", Kind: model.MoneySaving, Amount: 100, Currency: "RUB", Category: "нз"})
	_ = st.AddMoney(&model.Money{TS: now, Date: "2026-08-20", Kind: model.MoneySaving, Amount: 200, Currency: "RUB", Category: "нз"})
	_ = st.AddNote(&model.Note{TS: now, Date: "2026-08-20", Tag: "идея", Text: "мысль"})

	m, err := st.Money("2026-08-18", "2026-08-24")
	if err != nil || len(m) != 1 || m[0].Amount != 200 {
		t.Fatalf("деньги за период: %+v (%v)", m, err)
	}
	all, err := st.MoneyUntil("2026-08-24")
	if err != nil || len(all) != 2 {
		t.Fatalf("все деньги: %+v (%v)", all, err)
	}
	n, err := st.Notes("2026-08-18", "2026-08-24")
	if err != nil || len(n) != 1 || n[0].Tag != "идея" {
		t.Fatalf("заметки: %+v (%v)", n, err)
	}
}
