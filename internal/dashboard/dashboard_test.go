package dashboard

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/murluckk/self-reinvention/internal/config"
	"github.com/murluckk/self-reinvention/internal/model"
	"github.com/murluckk/self-reinvention/internal/store"
)

func TestDashboardRequiresPasswordAndRendersData(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "tracker.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cfg := &config.Config{
		Location: time.UTC,
		Users: []config.User{
			{TelegramID: 1001, DashboardUser: "owner", DashboardPassword: "secret"},
			{TelegramID: 1002, DashboardUser: "friend", DashboardPassword: "other-secret"},
		},
		MinFullDaysOff30: 4, MaxStreakNoDayOff: 12, MaxEnglishSkipsWeek: 1,
		MinSleepAvg: 7, MaxWakeSpreadH: 1.5, MinSavingsRate: .55,
	}
	sleep, english, focus, mood, energy := 7.5, 40, 8, 9, 7
	clean := true
	if err := st.UpsertDay(1001, &model.Day{
		Date: cfg.Today(), Sleep: &sleep, English: &english,
		Focus: &focus, Mood: &mood, Energy: &energy, Clean: &clean,
	}); err != nil {
		t.Fatal(err)
	}
	otherSleep := 9.0
	if err := st.UpsertDay(1002, &model.Day{Date: cfg.Today(), Sleep: &otherSleep}); err != nil {
		t.Fatal(err)
	}
	dash := New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/?days=7", nil)
	res := httptest.NewRecorder()
	dash.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("без пароля статус %d", res.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/?days=7", nil)
	req.SetBasicAuth("owner", "secret")
	res = httptest.NewRecorder()
	dash.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("с паролем статус %d: %s", res.Code, res.Body.String())
	}
	body := res.Body.String()
	for _, want := range []string{"Личный трекер", "7.5 ч", "8.0/10", "9.0/10", "7.0/10"} {
		if !strings.Contains(body, want) {
			t.Errorf("страница не содержит %q", want)
		}
	}
	for _, unwanted := range []string{"Калории", "Белок"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("страница содержит удалённое поле %q", unwanted)
		}
	}
	if strings.Contains(body, "9.0 ч") {
		t.Fatal("в дашборд первого пользователя попали чужие данные")
	}

	req = httptest.NewRequest(http.MethodGet, "/?days=7", nil)
	req.SetBasicAuth("friend", "other-secret")
	res = httptest.NewRecorder()
	dash.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "9.0 ч") {
		t.Fatalf("дашборд второго пользователя: %d %s", res.Code, res.Body.String())
	}
}

func TestHealthDoesNotRequirePassword(t *testing.T) {
	cfg := &config.Config{DashboardUser: "owner", DashboardPassword: "secret"}
	dash := New(cfg, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	res := httptest.NewRecorder()
	dash.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK || res.Body.String() != "ok\n" {
		t.Fatalf("health: %d %q", res.Code, res.Body.String())
	}
}
