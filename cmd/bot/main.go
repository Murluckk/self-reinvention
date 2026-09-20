// Команда bot — Telegram-бот личного трекинга с раздельными данными
// пользователей, long polling и SQLite рядом на диске.
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
	st, err := store.Open(cfg.DataPath, cfg.OwnerID)
	if err != nil {
		log.Error("хранилище", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	llmClient := llm.New(cfg.OllamaURL, cfg.OllamaModel)
	if cfg.LLMAPIKey != "" {
		llmClient = llm.NewOpenAI(cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.LLMModel)
	}
	b := bot.New(cfg, tg.New(cfg.BotToken), st, asr.New(cfg.ASRURL), llmClient, log)

	sunday := cfg.WeeklyWeekday
	var jobs []scheduler.Job
	for _, user := range cfg.Users {
		userID := user.TelegramID
		jobs = append(jobs,
			scheduler.Job{
				Name: "daily_reminder:" + user.DashboardUser, At: cfg.DailyReminder,
				Run: func(ctx context.Context) error { return b.RemindDay(ctx, userID) },
			},
			scheduler.Job{
				Name: "weekly_report:" + user.DashboardUser, At: cfg.WeeklyReport, Weekday: &sunday,
				Run: func(ctx context.Context) error { return b.SendWeekly(ctx, userID) },
			},
		)
	}
	jobs = append(jobs, scheduler.Job{Name: "daily_backup", At: cfg.DailyBackup, Run: b.SendBackup})
	sch := scheduler.New(cfg, st, log, jobs...)

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

	log.Info("запустился", "users", len(cfg.Users), "tz", cfg.Location.String(), "data", cfg.DataPath)
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
