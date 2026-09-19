// Команда bot — телеграм-бот личного трекинга. Один пользователь, long polling,
// SQLite рядом на диске.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/murluckk/self-reinvention/internal/asr"
	"github.com/murluckk/self-reinvention/internal/bot"
	"github.com/murluckk/self-reinvention/internal/config"
	"github.com/murluckk/self-reinvention/internal/dashboard"
	"github.com/murluckk/self-reinvention/internal/llm"
	"github.com/murluckk/self-reinvention/internal/scheduler"
	"github.com/murluckk/self-reinvention/internal/store"
	"github.com/murluckk/self-reinvention/internal/tg"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	cfg, err := config.Load()
	if err != nil {
		log.Error("конфигурация", "err", err)
		os.Exit(1)
	}
	st, err := store.Open(cfg.DataPath)
	if err != nil {
		log.Error("хранилище", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	b := bot.New(cfg, tg.New(cfg.BotToken), st, asr.New(cfg.ASRURL), llm.New(cfg.OllamaURL, cfg.OllamaModel), log)

	sunday := cfg.WeeklyWeekday
	sch := scheduler.New(cfg, st, log,
		scheduler.Job{Name: "daily_reminder", At: cfg.DailyReminder, Run: b.RemindDay},
		scheduler.Job{Name: "weekly_report", At: cfg.WeeklyReport, Weekday: &sunday, Run: b.SendWeekly},
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		sch.Run(ctx)
	}()
	go func() {
		defer wg.Done()
		if err := b.Run(ctx); err != nil {
			log.Error("бот остановился", "err", err)
		}
	}()
	if cfg.DashboardAddr != "" {
		dash := dashboard.New(cfg, st, log)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := dash.Run(ctx); err != nil {
				log.Error("дашборд остановился", "err", err)
			}
		}()
	}

	log.Info("запустился", "owner", cfg.OwnerID, "tz", cfg.Location.String(), "data", cfg.DataPath)
	<-ctx.Done()
	log.Info("получил сигнал, останавливаюсь")

	// Long polling висит на запросе к Telegram до 30 секунд; даём ему закрыться
	// сам, но не ждём вечно.
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(35 * time.Second):
		log.Warn("не дождался завершения горутин, выхожу")
	}
	log.Info("остановился")
}
