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
			{TelegramID: 1001, Name: "Паша", Profile: config.ProfilePasha, DashboardUser: "owner", DashboardPassword: "secret"},
			{TelegramID: 1002, Name: "Света", Profile: config.ProfileSveta, DashboardUser: "friend", DashboardPassword: "other-secret"},
		},
		MaxWakeSpreadH: 1.5, MinSavingsRate: .55,
	}
	wake, algorithms, systems, mood := "07:30", 40, 45, 9
	workout := true
	if err := st.UpsertDay(1001, &model.Day{
		Date: cfg.Today(), Wake: &wake, Algorithms: &algorithms,
		SystemDesign: &systems, Mood: &mood, Workout: &workout,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddMoney(1001, &model.Money{
		Date: cfg.Today(), TS: time.Now(), Kind: model.MoneyExpense,
		Amount: 1200, Currency: "RUB", Category: "еда", Comment: "обед",
	}); err != nil {
		t.Fatal(err)
	}
	otherWake, useful := "09:00", "книга по психологии"
	walk, study, sweet, alcohol := true, true, false, false
	if err := st.UpsertDay(1002, &model.Day{
		Date: cfg.Today(), Wake: &otherWake, Walk: &walk, Study: &study,
		Useful: &useful, Sweet: &sweet, Alcohol: &alcohol,
	}); err != nil {
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
	for _, want := range []string{"Паша", "07:30", "40 мин", "45 мин", "9.0/10", "1 200 ₽"} {
		if !strings.Contains(body, want) {
			t.Errorf("страница не содержит %q", want)
		}
	}
	for _, unwanted := range []string{"Калории", "Белок", "Английский"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("страница содержит удалённое поле %q", unwanted)
		}
	}
	if strings.Contains(body, "09:00") {
		t.Fatal("в дашборд первого пользователя попали чужие данные")
	}

	req = httptest.NewRequest(http.MethodGet, "/?days=7", nil)
	req.SetBasicAuth("friend", "other-secret")
	res = httptest.NewRecorder()
	dash.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "Света") ||
		!strings.Contains(res.Body.String(), "09:00") ||
		!strings.Contains(res.Body.String(), "Прогулки") ||
		!strings.Contains(res.Body.String(), "книга по психологии") {
		t.Fatalf("дашборд второго пользователя: %d %s", res.Code, res.Body.String())
	}
	if strings.Contains(res.Body.String(), "Алгоритмы") || strings.Contains(res.Body.String(), "Системный дизайн") ||
		strings.Contains(res.Body.String(), "Деньги") {
		t.Fatal("в профиль Светы попали поля или финансы Паши")
	}

	req = httptest.NewRequest(http.MethodGet, "/?days=7&date="+cfg.Today(), nil)
	req.SetBasicAuth("owner", "secret")
	res = httptest.NewRecorder()
	dash.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "Траты за") ||
		!strings.Contains(res.Body.String(), "еда") || !strings.Contains(res.Body.String(), "обед") {
		t.Fatalf("детали расходов: %d %s", res.Code, res.Body.String())
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
