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
const userID int64 = 949465743

func testCfg() *config.Config {
	return &config.Config{Location: time.UTC, MaxWakeSpreadH: 1.5, MinSavingsRate: 0.55}
}

func TestPipelineFromCommandsToReport(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// Так это выглядит в реальном использовании: короткие строки в течение дня.
	inputs := []string{
		"2026-08-18 подъем 7:00 отбой 23:30 алго 40 системы 30 трен состояние 8",
		"2026-08-19 подъем 7:30 отбой 23:45 алго 30 системы 45 трен состояние 7",
		"2026-08-20 подъем 8:00 отбой 00:15 алго нет системы нет трен нет состояние 5",
		"2026-08-21 подъем 7:15 отбой 23:20 алго 45 системы 30 трен состояние 8",
		"2026-08-22 подъем 9:30 отбой 00:30 алго 20 системы нет трен нет состояние 9",
		"2026-08-23 подъем 7:45 отбой 23:50 алго 60 системы 40 трен состояние 8",
		"2026-08-24 подъем 7:00 отбой 23:10 алго 35 системы 30 трен состояние 7",
	}
	for _, in := range inputs {
		res := parse.ParseDay(in, today)
		if len(res.Errors) > 0 {
			t.Fatalf("%q -> %v", in, res.Errors)
		}
		if err := st.UpsertDay(userID, res.Day); err != nil {
			t.Fatal(err)
		}
	}
	// Дописывание задним числом не должно ломать уже записанное.
	res := parse.ParseDay("2026-08-19 состояние 6", today)
	if err := st.UpsertDay(userID, res.Day); err != nil {
		t.Fatal(err)
	}
	d, err := st.GetDay(userID, "2026-08-19")
	if err != nil {
		t.Fatal(err)
	}
	if d.Wake == nil || *d.Wake != "07:30" || d.Mood == nil || *d.Mood != 6 {
		t.Fatalf("дописывание затёрло данные: %+v", d)
	}

	for _, in := range []string{"+250000 зп август", "-1200 еда обед", "=150000 накопления", "=5000$ холодный кошелёк"} {
		m, err := parse.ParseMoney(in, "2026-08-19")
		if err != nil {
			t.Fatalf("%q -> %v", in, err)
		}
		m.TS = time.Now()
		if err := st.AddMoney(userID, m); err != nil {
			t.Fatal(err)
		}
	}
	tag, body := parse.ParseNote("#идея перенести тренировки на утро")
	if err := st.AddNote(userID, &model.Note{TS: time.Now(), Date: "2026-08-19", Tag: tag, Text: body}); err != nil {
		t.Fatal(err)
	}

	days, _ := st.Days(userID, "2026-08-18", today)
	money, _ := st.Money(userID, "2026-08-18", today)
	moneyAll, _ := st.MoneyUntil(userID, today)
	notes, _ := st.Notes(userID, "2026-08-18", today)
	s := report.Build(report.Input{From: "2026-08-18", To: today, Days: days, DaysAll: days,
		Money: money, MoneyAll: moneyAll, Notes: notes, Today: today, Cfg: testCfg()})

	if s.FilledDays != 7 {
		t.Fatalf("заполнено дней: %d", s.FilledDays)
	}
	if s.Workouts != 5 {
		t.Fatalf("тренировок %d, ожидалось 5", s.Workouts)
	}
	if s.Streaks.Algorithms != 4 || s.Streaks.Filled != 7 {
		t.Fatalf("стрики: %+v", s.Streaks)
	}
	if rub := s.Currencies["RUB"]; rub.Capital != 150000 || rub.Income != 250000 {
		t.Fatalf("рубли: %+v", rub)
	}

	md := report.Markdown(s)
	for _, want := range []string{
		"# Разбор: 2026-08-18 — 2026-08-24",
		"перенести тренировки на утро",
		"алгоритмы",
		"| 2026-08-24 | пн |",
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
