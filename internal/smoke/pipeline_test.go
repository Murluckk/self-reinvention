// Package smoke — сквозной тест: ввод команд, запись в базу, сборка отчёта.
// Проверяет стыки между пакетами, которые модульные тесты не видят.
package smoke

import (
	"strings"
	"testing"
	"time"

	"path/filepath"

	"github.com/murluckk/self-reinvention/internal/config"
	"github.com/murluckk/self-reinvention/internal/model"
	"github.com/murluckk/self-reinvention/internal/parse"
	"github.com/murluckk/self-reinvention/internal/report"
	"github.com/murluckk/self-reinvention/internal/store"
)

const today = "2026-08-24"

func testCfg() *config.Config {
	return &config.Config{Location: time.UTC, MinFullDaysOff30: 4, MaxStreakNoDayOff: 12,
		MaxEnglishSkipsWeek: 1, MinSleepAvg: 7, MaxWakeSpreadH: 1.5, MinSavingsRate: 0.55}
}

func TestPipelineFromCommandsToReport(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// Так это выглядит в реальном использовании: короткие строки в течение дня.
	inputs := []string{
		"2026-08-18 сон 7 подъем 7:00 зал англ 40 работа 8 чисто готовил фокус 8 вес 74",
		"2026-08-19 сон 6.5 подъем 7:30 бег англ 30 работа 9 чисто тг 5",
		"2026-08-20 сон 8 подъем 8:00 трен нет англ 0 работа 7 чисто нет тг 8 note сорвался после созвона",
		"2026-08-21 сон 7.5 подъем 7:15 зал англ 45 работа 8 смена чисто",
		"2026-08-22 сон 9 подъем 9:30 выходной англ 20 чисто работа 0",
		"2026-08-23 сон 7 подъем 7:45 улица англ 60 работа 4 чисто вес 73.2 готовил",
		"2026-08-24 сон 6.5 подъем 7:00 зал англ 35 работа 8 чисто тг 2",
	}
	for _, in := range inputs {
		res := parse.ParseDay(in, today)
		if len(res.Errors) > 0 {
			t.Fatalf("%q -> %v", in, res.Errors)
		}
		if err := st.UpsertDay(res.Day); err != nil {
			t.Fatal(err)
		}
	}
	// Дописывание задним числом не должно ломать уже записанное.
	res := parse.ParseDay("2026-08-19 фокус 6", today)
	if err := st.UpsertDay(res.Day); err != nil {
		t.Fatal(err)
	}
	d, err := st.GetDay("2026-08-19")
	if err != nil {
		t.Fatal(err)
	}
	if d.Sleep == nil || *d.Sleep != 6.5 || d.Focus == nil || *d.Focus != 6 {
		t.Fatalf("дописывание затёрло данные: %+v", d)
	}

	for _, in := range []string{"+250000 зп август", "-1200 еда обед", "=150000 накопления", "=5000$ холодный кошелёк"} {
		m, err := parse.ParseMoney(in, "2026-08-19")
		if err != nil {
			t.Fatalf("%q -> %v", in, err)
		}
		m.TS = time.Now()
		if err := st.AddMoney(m); err != nil {
			t.Fatal(err)
		}
	}
	tag, body := parse.ParseNote("#идея перенести тренировки на утро")
	if err := st.AddNote(&model.Note{TS: time.Now(), Date: "2026-08-19", Tag: tag, Text: body}); err != nil {
		t.Fatal(err)
	}

	days, _ := st.Days("2026-08-18", today)
	money, _ := st.Money("2026-08-18", today)
	moneyAll, _ := st.MoneyUntil(today)
	notes, _ := st.Notes("2026-08-18", today)
	s := report.Build(report.Input{From: "2026-08-18", To: today, Days: days, DaysAll: days,
		Money: money, MoneyAll: moneyAll, Notes: notes, Today: today, Cfg: testCfg()})

	if s.FilledDays != 7 {
		t.Fatalf("заполнено дней: %d", s.FilledDays)
	}
	if s.Workouts != 5 {
		t.Fatalf("тренировок %d, ожидалось 5", s.Workouts)
	}
	if s.Streaks.Clean != 4 || s.Streaks.Filled != 7 {
		t.Fatalf("стрики: %+v", s.Streaks)
	}
	if rub := s.Currencies["RUB"]; rub.Capital != 150000 || rub.Income != 250000 {
		t.Fatalf("рубли: %+v", rub)
	}

	md := report.Markdown(s)
	for _, want := range []string{
		"# Разбор: 2026-08-18 — 2026-08-24",
		"перенести тренировки на утро",
		"срывы: 2026-08-20",
		"| 2026-08-24 | пн |",
		"⚠️",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("в выгрузке нет %q:\n%s", want, md)
		}
	}

	status := report.Status(d, s)
	if !strings.Contains(status, "Стрики") || !strings.Contains(status, "Капитал") {
		t.Fatalf("статус неполон:\n%s", status)
	}
}
