// Package bot связывает всё вместе: читает обновления Telegram, разбирает
// команды, ходит в ASR и LLM, пишет в базу и отвечает пользователю.
package bot

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/murluckk/self-reinvention/internal/asr"
	"github.com/murluckk/self-reinvention/internal/config"
	"github.com/murluckk/self-reinvention/internal/llm"
	"github.com/murluckk/self-reinvention/internal/report"
	"github.com/murluckk/self-reinvention/internal/store"
	"github.com/murluckk/self-reinvention/internal/tg"
)

// Bot — главный объект приложения.
type Bot struct {
	cfg         *config.Config
	tg          *tg.Client
	st          *store.Store
	asr         *asr.Client
	asrFallback *asr.Client
	llm         *llm.Client
	log         *slog.Logger

	mu      sync.Mutex
	pending map[string]*pending
	seq     int64
}

// New собирает бота из готовых зависимостей.
func New(cfg *config.Config, client *tg.Client, st *store.Store, a, fallback *asr.Client, l *llm.Client, log *slog.Logger) *Bot {
	return &Bot{
		cfg: cfg, tg: client, st: st, asr: a, asrFallback: fallback,
		llm: l, log: log, pending: map[string]*pending{},
	}
}

// Run крутит long polling до отмены контекста.
func (b *Bot) Run(ctx context.Context) error {
	if me, err := b.tg.GetMe(ctx); err != nil {
		// Не выходим: сеть может быть недоступна в момент старта, а long polling
		// сам переживает разрывы. Но в логе видно, что токен не сработал.
		b.log.Error("не смог представиться в telegram, проверь BOT_TOKEN", "err", err)
	} else {
		b.log.Info("подключился", "bot", me.Username)
	}
	if err := b.tg.SetMyCommands(ctx, commandDescriptions); err != nil {
		b.log.Warn("не удалось выставить меню команд", "err", err)
	}
	var offset int64
	backoff := time.Second
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		updates, err := b.tg.GetUpdates(ctx, offset, 30)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, context.Canceled) {
				return nil
			}
			b.log.Error("getUpdates", "err", err)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(backoff):
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
		for _, u := range updates {
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
			b.handleUpdate(ctx, &u)
		}
		b.expirePending()
	}
}

func (b *Bot) handleUpdate(ctx context.Context, u *tg.Update) {
	defer func() {
		if r := recover(); r != nil {
			b.log.Error("паника в обработчике", "panic", r)
		}
	}()
	switch {
	case u.CallbackQuery != nil:
		if !b.owns(u.CallbackQuery.From) {
			return
		}
		b.handleCallback(ctx, u.CallbackQuery.From.ID, u.CallbackQuery)
	case u.Message != nil:
		if !b.owns(u.Message.From) {
			return
		}
		b.handleMessage(ctx, u.Message.From.ID, u.Message)
	}
}

// owns молча отсекает всех, кроме владельца: бот личный, отвечать посторонним
// он не должен даже отказом.
func (b *Bot) owns(u *tg.User) bool {
	if u == nil {
		return false
	}
	if _, ok := b.cfg.User(u.ID); !ok {
		b.log.Info("игнорирую чужое сообщение", "user_id", u.ID)
		return false
	}
	return true
}

// reply отправляет текст, логируя ошибку отправки: терять данные из-за
// упавшего ответа нельзя, но и падать бот не должен.
func (b *Bot) reply(ctx context.Context, chatID int64, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	if _, err := b.tg.SendMessage(ctx, chatID, text, nil); err != nil {
		b.log.Error("sendMessage", "err", err)
	}
}

// Notify пишет владельцу — этим пользуется планировщик.
func (b *Bot) Notify(ctx context.Context, userID int64, text string) error {
	_, err := b.tg.SendMessage(ctx, userID, text, nil)
	return err
}

func (b *Bot) profile(userID int64) string {
	if user, ok := b.cfg.User(userID); ok {
		return user.Profile
	}
	return config.ProfilePasha
}

func (b *Bot) now(userID int64) time.Time { return b.cfg.NowFor(userID) }
func (b *Bot) today(userID int64) string  { return b.cfg.TodayFor(userID) }

// stats собирает агрегаты за период; общий код для /s, /w и планировщика.
func (b *Bot) stats(userID int64, from, to string) (*report.Stats, error) {
	days, err := b.st.Days(userID, from, to)
	if err != nil {
		return nil, err
	}
	// Стрики считаем по длинному окну, иначе серия в 40 дней покажется семёркой.
	daysAll, err := b.st.Days(userID, report.AddDays(to, -400), to)
	if err != nil {
		return nil, err
	}
	money, err := b.st.Money(userID, from, to)
	if err != nil {
		return nil, err
	}
	moneyAll, err := b.st.MoneyUntil(userID, to)
	if err != nil {
		return nil, err
	}
	notes, err := b.st.Notes(userID, from, to)
	if err != nil {
		return nil, err
	}
	return report.Build(report.Input{
		From: from, To: to,
		Days: days, DaysAll: daysAll,
		Money: money, MoneyAll: moneyAll,
		Notes:   notes,
		Today:   b.today(userID),
		Cfg:     b.cfg,
		Profile: b.profile(userID),
	}), nil
}
