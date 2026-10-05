// Команда bot — Telegram-бот личного трекинга с раздельными данными
// пользователей, long polling и SQLite рядом на диске.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
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
	"github.com/murluckk/self-reinvention/internal/users"
)

// version подставляется при сборке: -ldflags "-X main.version=...".
var version = "dev"

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

	reg, err := users.Load(st)
	if err != nil {
		log.Error("пользователи", "err", err)
		os.Exit(1)
	}
	// TRACKER_USERS из env переносится в базу один раз; дальше пользователи
	// живут в базе и управляются командами бота.
	seeds := make([]users.Seed, 0, len(cfg.Users))
	for _, u := range cfg.Users {
		seeds = append(seeds, users.Seed{
			ID: u.TelegramID, Name: u.Name, Preset: u.Profile, Timezone: u.Timezone,
			Login: u.DashboardUser, Password: u.DashboardPassword,
		})
	}
	if imported, err := reg.Import(seeds, cfg.OwnerID); err != nil {
		log.Error("импорт пользователей из env", "err", err)
		os.Exit(1)
	} else if len(imported) > 0 {
		log.Info("пользователи перенесены из env в базу", "names", imported)
	}
	if _, ok := reg.Get(cfg.OwnerID); !ok {
		log.Error("OWNER_ID не найден среди пользователей", "owner", cfg.OwnerID)
		os.Exit(1)
	}

	llmClient := llm.New(cfg.OllamaURL, cfg.OllamaModel)
	if cfg.LLMAPIKey != "" {
		llmClient = llm.NewOpenAI(cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.LLMModel)
	}
	localASR := asr.New(cfg.ASRURL)
	primaryASR, fallbackASR := localASR, (*asr.Client)(nil)
	if cfg.LLMAPIKey != "" {
		primaryASR = asr.NewOpenAI(
			cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.TranscribeModel, cfg.TranscribePrompt,
		)
		fallbackASR = localASR
	}
	b := bot.New(cfg, tg.New(cfg.BotToken), st, reg, primaryASR, fallbackASR, llmClient, log)
	b.Version = version

	sch := scheduler.NewDynamic(cfg, st, log, func() []scheduler.Job { return jobs(cfg, reg, b) })
	sch.OnError = b.JobFailed

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
		dash := dashboard.New(cfg, st, reg, log)
		dash.OnBlocked = func(key string) {
			b.Alert(ctx, "dashboard:"+key, "Дашборд: слишком много неудачных входов, временно блокирую "+key+".")
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := dash.Run(ctx); err != nil {
				log.Error("дашборд остановился", "err", err)
			}
		}()
	}

	log.Info("запустился", "version", version, "users", len(reg.All()), "tz", cfg.Location.String(), "data", cfg.DataPath)
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

// jobs строит расписание заново на каждом тике планировщика: новый участник
// или смена часового пояса подхватываются без перезапуска.
func jobs(cfg *config.Config, reg *users.Registry, b *bot.Bot) []scheduler.Job {
	sunday := cfg.WeeklyWeekday
	var out []scheduler.Job
	for _, user := range reg.All() {
		userID := user.ID
		jobID := strconv.FormatInt(userID, 10)
		loc := user.Location()
		out = append(out,
			scheduler.Job{
				Name: "daily_reminder:" + jobID, At: cfg.DailyReminder, Location: loc,
				Run: func(ctx context.Context) error { return b.RemindDay(ctx, userID) },
			},
			scheduler.Job{
				Name: "weekly_report:" + jobID, At: cfg.WeeklyReport, Weekday: &sunday, Location: loc,
				Run: func(ctx context.Context) error { return b.SendWeekly(ctx, userID) },
			},
		)
		if user.Finance {
			out = append(out,
				scheduler.Job{
					Name: "salary_reminder:" + jobID, At: cfg.IncomeReminder, DayOfMonth: 5, Location: loc,
					Run: func(ctx context.Context) error { return b.RemindIncome(ctx, userID, "зарплата") },
				},
				scheduler.Job{
					Name: "advance_reminder:" + jobID, At: cfg.IncomeReminder, DayOfMonth: 20, Location: loc,
					Run: func(ctx context.Context) error { return b.RemindIncome(ctx, userID, "аванс") },
				},
				scheduler.Job{
					Name: "monthly_finance:" + jobID, At: cfg.MonthlyFinance, DayOfMonth: 1, Location: loc,
					Run: func(ctx context.Context) error { return b.SendMonthlyFinance(ctx, userID) },
				},
			)
		}
	}
	return append(out, scheduler.Job{Name: "daily_backup", At: cfg.DailyBackup, Run: b.SendBackup})
}
