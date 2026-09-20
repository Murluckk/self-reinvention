package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/murluckk/self-reinvention/internal/model"
)

const testUserID int64 = 1001

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func i(v int) *int       { return &v }
func s(v string) *string { return &v }
func bp(v bool) *bool    { return &v }

// Главное свойство хранилища: дописывание не затирает уже записанное.
func TestUpsertDayMergesPartialWrites(t *testing.T) {
	st := open(t)
	if err := st.UpsertDay(testUserID, &model.Day{Date: "2026-08-24", Wake: s("07:30"), Algorithms: i(40)}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertDay(testUserID, &model.Day{Date: "2026-08-24", Algorithms: i(60), Mood: i(8), Workout: bp(true)}); err != nil {
		t.Fatal(err)
	}
	d, err := st.GetDay(testUserID, "2026-08-24")
	if err != nil {
		t.Fatal(err)
	}
	if d.Wake == nil || *d.Wake != "07:30" {
		t.Fatalf("подъём затёрся: %v", d.Wake)
	}
	if d.Algorithms == nil || *d.Algorithms != 60 {
		t.Fatalf("алгоритмы не обновились: %v", d.Algorithms)
	}
	if d.Mood == nil || *d.Mood != 8 {
		t.Fatalf("состояние не записалось: %v", d.Mood)
	}
	if d.Workout == nil || !*d.Workout {
		t.Fatalf("тренировка не записалась: %v", d.Workout)
	}
}

func TestGetDayMissingReturnsEmpty(t *testing.T) {
	st := open(t)
	d, err := st.GetDay(testUserID, "2026-01-01")
	if err != nil {
		t.Fatal(err)
	}
	if d.Date != "2026-01-01" || !d.Empty() {
		t.Fatalf("ожидалась пустая запись, получено %+v", d)
	}
}

func TestUsersHaveIndependentDays(t *testing.T) {
	st := open(t)
	algorithmsA, algorithmsB := 30, 90
	if err := st.UpsertDay(1, &model.Day{Date: "2026-08-24", Algorithms: &algorithmsA}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertDay(2, &model.Day{Date: "2026-08-24", Algorithms: &algorithmsB}); err != nil {
		t.Fatal(err)
	}
	a, _ := st.GetDay(1, "2026-08-24")
	b, _ := st.GetDay(2, "2026-08-24")
	if a.Algorithms == nil || *a.Algorithms != 30 || b.Algorithms == nil || *b.Algorithms != 90 {
		t.Fatalf("данные смешались: a=%+v b=%+v", a, b)
	}
}

func TestMigrationDiscardsLegacyDayData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE days (
		date TEXT PRIMARY KEY, updated_at TEXT, focus INTEGER, mood INTEGER, energy INTEGER);
		INSERT INTO days (date, focus, mood, energy) VALUES ('2026-08-24', 3, 4, 5)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	for attempt := 0; attempt < 2; attempt++ {
		st, err := Open(path, testUserID)
		if err != nil {
			t.Fatal(err)
		}
		d, err := st.GetDay(testUserID, "2026-08-24")
		if err != nil {
			t.Fatal(err)
		}
		if !d.Empty() {
			t.Fatalf("попытка %d: старые данные не отброшены: %+v", attempt+1, d)
		}
		if err := st.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBackupCreatesReadableDatabase(t *testing.T) {
	st := open(t)
	if err := st.UpsertDay(testUserID, &model.Day{Date: "2026-08-24", Wake: s("07:30")}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "backup", "tracker.db")
	if err := st.Backup(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 || info.Mode().Perm() != 0o600 {
		t.Fatalf("backup info=%v err=%v", info, err)
	}
	copy, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer copy.Close()
	d, err := copy.GetDay(testUserID, "2026-08-24")
	if err != nil || d.Wake == nil || *d.Wake != "07:30" {
		t.Fatalf("backup не читается: day=%+v err=%v", d, err)
	}
}

func TestDaysRange(t *testing.T) {
	st := open(t)
	for _, date := range []string{"2026-08-17", "2026-08-18", "2026-08-24", "2026-08-25"} {
		if err := st.UpsertDay(testUserID, &model.Day{Date: date, Wake: s("07:00")}); err != nil {
			t.Fatal(err)
		}
	}
	days, err := st.Days(testUserID, "2026-08-18", "2026-08-24")
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
	if err := st.AddNote(testUserID, &model.Note{TS: now, Date: "2026-08-24", Text: "мысль"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddMoney(testUserID, &model.Money{TS: now.Add(time.Minute), Date: "2026-08-24",
		Kind: model.MoneyExpense, Amount: 1200, Currency: "RUB", Category: "еда"}); err != nil {
		t.Fatal(err)
	}
	u, err := st.LastUndoable(testUserID)
	if err != nil {
		t.Fatal(err)
	}
	if u == nil || u.Kind != "money" {
		t.Fatalf("ожидалась денежная запись, получено %+v", u)
	}
	if err := st.Delete(testUserID, u.Kind, u.ID); err != nil {
		t.Fatal(err)
	}
	u, err = st.LastUndoable(testUserID)
	if err != nil {
		t.Fatal(err)
	}
	if u == nil || u.Kind != "note" {
		t.Fatalf("после отмены ожидалась заметка, получено %+v", u)
	}
	if err := st.Delete(testUserID, u.Kind, u.ID); err != nil {
		t.Fatal(err)
	}
	if u, _ := st.LastUndoable(testUserID); u != nil {
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
	_ = st.AddMoney(testUserID, &model.Money{TS: now, Date: "2026-08-17", Kind: model.MoneySaving, Amount: 100, Currency: "RUB", Category: "нз"})
	_ = st.AddMoney(testUserID, &model.Money{TS: now, Date: "2026-08-20", Kind: model.MoneySaving, Amount: 200, Currency: "RUB", Category: "нз"})
	_ = st.AddNote(testUserID, &model.Note{TS: now, Date: "2026-08-20", Tag: "идея", Text: "мысль"})

	m, err := st.Money(testUserID, "2026-08-18", "2026-08-24")
	if err != nil || len(m) != 1 || m[0].Amount != 200 {
		t.Fatalf("деньги за период: %+v (%v)", m, err)
	}
	all, err := st.MoneyUntil(testUserID, "2026-08-24")
	if err != nil || len(all) != 2 {
		t.Fatalf("все деньги: %+v (%v)", all, err)
	}
	n, err := st.Notes(testUserID, "2026-08-18", "2026-08-24")
	if err != nil || len(n) != 1 || n[0].Tag != "идея" {
		t.Fatalf("заметки: %+v (%v)", n, err)
	}
}
