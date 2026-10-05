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

	"github.com/murluckk/self-reinvention/internal/auth"
	"github.com/murluckk/self-reinvention/internal/config"
	"github.com/murluckk/self-reinvention/internal/model"
	"github.com/murluckk/self-reinvention/internal/store"
	"github.com/murluckk/self-reinvention/internal/users"
)

func TestDashboardRequiresPasswordAndRendersData(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "tracker.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cfg := &config.Config{Location: time.UTC, MaxWakeSpreadH: 1.5, MinSavingsRate: .55}
	reg := registry(t, st,
		users.Seed{ID: 1001, Name: "Паша", Preset: "pasha", Timezone: "UTC", Login: "owner", Password: "secret"},
		users.Seed{ID: 1002, Name: "Света", Preset: "sveta", Timezone: "UTC", Login: "friend", Password: "other-secret"},
	)
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
	dash := New(cfg, st, reg, slog.New(slog.NewTextHandler(io.Discard, nil)))

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
		!strings.Contains(res.Body.String(), "Прогулка") ||
		!strings.Contains(res.Body.String(), "Сладкое") ||
		!strings.Contains(res.Body.String(), "Алкоголь") ||
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
	dash := New(&config.Config{}, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	res := httptest.NewRecorder()
	dash.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK || res.Body.String() != "ok\n" {
		t.Fatalf("health: %d %q", res.Code, res.Body.String())
	}
}

func registry(t *testing.T, st *store.Store, seeds ...users.Seed) *users.Registry {
	t.Helper()
	reg, err := users.Load(st)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Import(seeds, seeds[0].ID); err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestDashboardBlocksPasswordGuessing(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "tracker.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	reg := registry(t, st, users.Seed{ID: 1, Name: "Паша", Preset: "pasha", Timezone: "UTC", Login: "owner", Password: "secret"})
	dash := New(&config.Config{Location: time.UTC}, st, reg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	var blocked []string
	dash.OnBlocked = func(key string) { blocked = append(blocked, key) }
	h := dash.Handler()
	try := func(ip, password string) int {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Forwarded-For", ip)
		req.SetBasicAuth("owner", password)
		res := httptest.NewRecorder()
		h.ServeHTTP(res, req)
		return res.Code
	}
	for i := 0; i < 10; i++ {
		if code := try("203.0.113.7", "wrong"); code != http.StatusUnauthorized {
			t.Fatalf("попытка %d: статус %d", i, code)
		}
	}
	if code := try("203.0.113.7", "secret"); code != http.StatusTooManyRequests {
		t.Fatalf("после 10 ошибок IP должен быть заблокирован даже с верным паролем, статус %d", code)
	}
	if len(blocked) == 0 || !strings.Contains(blocked[0], "203.0.113.7") {
		t.Fatalf("владельца не предупредили о переборе: %v", blocked)
	}
	if code := try("198.51.100.1", "secret"); code != http.StatusOK {
		t.Fatalf("другой IP с верным паролем: статус %d", code)
	}
}

func TestDashboardAcceptsNewPasswordImmediately(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "tracker.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	reg := registry(t, st, users.Seed{ID: 1, Name: "Паша", Preset: "pasha", Timezone: "UTC", Login: "owner", Password: "old"})
	dash := New(&config.Config{Location: time.UTC}, st, reg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, ok := dash.check("owner", "old"); !ok {
		t.Fatal("старый пароль не принят")
	}
	u, _ := reg.Get(1)
	u.PasswordHash, _ = auth.Hash("new")
	if err := reg.Save(u); err != nil {
		t.Fatal(err)
	}
	if _, ok := dash.check("owner", "old"); ok {
		t.Fatal("после смены пароля старый всё ещё работает из кэша")
	}
	if _, ok := dash.check("owner", "new"); !ok {
		t.Fatal("новый пароль не принят")
	}
}
