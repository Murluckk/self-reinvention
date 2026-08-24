package scheduler

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/murluckk/self-reinvention/internal/config"
	"github.com/murluckk/self-reinvention/internal/store"
)

func setup(t *testing.T) (*config.Config, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return &config.Config{Location: time.UTC}, st
}

// Рестарт бота не должен приводить к повторной отправке напоминания.
func TestJobRunsOncePerDay(t *testing.T) {
	cfg, st := setup(t)
	runs := 0
	s := New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)),
		Job{Name: "daily", At: "00:00", Run: func(context.Context) error { runs++; return nil }})
	s.tick(context.Background())
	s.tick(context.Background())
	s.tick(context.Background())
	if runs != 1 {
		t.Fatalf("задача отработала %d раз, ожидался 1", runs)
	}
}

// До назначенного времени задача не срабатывает.
func TestJobWaitsForItsTime(t *testing.T) {
	cfg, st := setup(t)
	runs := 0
	s := New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)),
		Job{Name: "daily", At: "23:59", Run: func(context.Context) error { runs++; return nil }})
	s.tick(context.Background())
	if now := time.Now().UTC(); now.Hour() == 23 && now.Minute() == 59 {
		t.Skip("тест запущен ровно в 23:59 UTC")
	}
	if runs != 0 {
		t.Fatalf("задача отработала раньше времени")
	}
}

// Ошибка задачи не должна помечать её выполненной: повторим на следующем тике.
func TestFailedJobRetries(t *testing.T) {
	cfg, st := setup(t)
	runs := 0
	s := New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)),
		Job{Name: "daily", At: "00:00", Run: func(context.Context) error {
			runs++
			if runs == 1 {
				return io.ErrUnexpectedEOF
			}
			return nil
		}})
	s.tick(context.Background())
	s.tick(context.Background())
	s.tick(context.Background())
	if runs != 2 {
		t.Fatalf("задача отработала %d раз, ожидалось 2 (одна ошибка + повтор)", runs)
	}
}

// Недельная выгрузка привязана к дню недели.
func TestWeekdayFilter(t *testing.T) {
	cfg, st := setup(t)
	runs := 0
	wrong := time.Now().UTC().Weekday() + 1
	if wrong > time.Saturday {
		wrong = time.Sunday
	}
	s := New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)),
		Job{Name: "weekly", At: "00:00", Weekday: &wrong, Run: func(context.Context) error { runs++; return nil }})
	s.tick(context.Background())
	if runs != 0 {
		t.Fatal("задача сработала не в свой день недели")
	}
}

func TestMinutes(t *testing.T) {
	if v, err := minutes("23:00"); err != nil || v != 1380 {
		t.Fatalf("%d %v", v, err)
	}
	for _, bad := range []string{"", "25:00", "12:99", "12", "abc"} {
		if _, err := minutes(bad); err == nil {
			t.Fatalf("ожидалась ошибка на %q", bad)
		}
	}
}
