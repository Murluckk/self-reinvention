// Package bot связывает всё вместе: читает обновления Telegram, разбирает
// команды, ходит в ASR и LLM, пишет в базу и отвечает пользователю.
package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/murluckk/self-reinvention/internal/asr"
	"github.com/murluckk/self-reinvention/internal/config"
	"github.com/murluckk/self-reinvention/internal/llm"
	"github.com/murluckk/self-reinvention/internal/model"
	"github.com/murluckk/self-reinvention/internal/report"
	"github.com/murluckk/self-reinvention/internal/store"
	"github.com/murluckk/self-reinvention/internal/tg"
	"github.com/murluckk/self-reinvention/internal/users"
)

// Bot — главный объект приложения.
type Bot struct {
	cfg         *config.Config
	tg          *tg.Client
	st          *store.Store
	users       *users.Registry
	asr         *asr.Client
	asrFallback *asr.Client
	llm         *llm.Client
	log         *slog.Logger

	// Version — версия сборки для /health.
	Version  string
	started  time.Time
	username string

	mu       sync.Mutex
	pending  map[string]*pending
	seq      int64
	checkins map[int64]*checkin
	edits    map[int64]*fieldEdit

	health *health
}

// New собирает бота из готовых зависимостей.
func New(cfg *config.Config, client *tg.Client, st *store.Store, reg *users.Registry, a, fallback *asr.Client, l *llm.Client, log *slog.Logger) *Bot {
	return &Bot{
		cfg: cfg, tg: client, st: st, users: reg, asr: a, asrFallback: fallback,
		llm: l, log: log, started: time.Now(),
		pending:  map[string]*pending{},
		checkins: map[int64]*checkin{},
		edits:    map[int64]*fieldEdit{},
		health:   newHealth(),
	}
}

// Run крутит long polling до отмены контекста.
func (b *Bot) Run(ctx context.Context) error {
	if me, err := b.tg.GetMe(ctx); err != nil {
		// Не выходим: сеть может быть недоступна в момент старта, а long polling
		// сам переживает разрывы. Но в логе видно, что токен не сработал.
		b.log.Error("не смог представиться в telegram, проверь BOT_TOKEN", "err", err)
	} else {
		b.username = me.Username
		b.log.Info("подключился", "bot", me.Username)
	}
	if err := b.tg.SetMyCommands(ctx, commandMenu); err != nil {
		b.log.Warn("не удалось выставить меню команд", "err", err)
	}
	var offset int64
	backoff := time.Second
	var failingSince time.Time
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
			if failingSince.IsZero() {
				failingSince = time.Now()
			}
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
		if !failingSince.IsZero() && time.Since(failingSince) > 5*time.Minute {
			// Пока Telegram недоступен, писать в него бессмысленно; сообщаем,
			// когда связь вернулась, чтобы было видно, сколько бот был глухим.
			b.alert(ctx, "telegram", fmt.Sprintf("Telegram был недоступен %s, связь восстановилась.",
				time.Since(failingSince).Round(time.Minute)))
		}
		failingSince = time.Time{}
		backoff = time.Second
		for _, u := range updates {
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
			b.handleUpdate(ctx, &u)
		}
		b.expireSessions()
	}
}

func (b *Bot) handleUpdate(ctx context.Context, u *tg.Update) {
	defer func() {
		if r := recover(); r != nil {
			b.log.Error("паника в обработчике", "panic", r)
			b.alert(ctx, "panic", fmt.Sprintf("Паника в обработчике: %v", r))
		}
	}()
	switch {
	case u.CallbackQuery != nil:
		user, ok := b.member(u.CallbackQuery.From)
		if !ok {
			return
		}
		b.handleCallback(ctx, user, u.CallbackQuery)
	case u.Message != nil:
		if b.tryJoin(ctx, u.Message) {
			return
		}
		user, ok := b.member(u.Message.From)
		if !ok {
			return
		}
		b.handleMessage(ctx, user, u.Message)
	}
}

// member молча отсекает всех, кого нет в реестре: бот личный, отвечать
// посторонним он не должен даже отказом. Войти можно только по приглашению.
func (b *Bot) member(u *tg.User) (*model.User, bool) {
	if u == nil {
		return nil, false
	}
	user, ok := b.users.Get(u.ID)
	if !ok {
		b.log.Info("игнорирую чужое сообщение", "user_id", u.ID)
		return nil, false
	}
	return user, true
}

func (b *Bot) isAdmin(user *model.User) bool { return user.ID == b.cfg.OwnerID }

// reply отправляет текст, логируя ошибку отправки: терять данные из-за
// упавшего ответа нельзя, но и падать бот не должен.
func (b *Bot) reply(ctx context.Context, chatID int64, text string) {
	b.send(ctx, chatID, text, nil)
}

func (b *Bot) send(ctx context.Context, chatID int64, text string, markup *tg.InlineKeyboardMarkup) *tg.Message {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	var opt *tg.SendOptions
	if markup != nil {
		opt = &tg.SendOptions{Markup: markup}
	}
	m, err := b.tg.SendMessage(ctx, chatID, text, opt)
	if err != nil {
		b.log.Error("sendMessage", "err", err)
		return nil
	}
	return m
}

func (b *Bot) edit(ctx context.Context, chatID, msgID int64, text string, markup *tg.InlineKeyboardMarkup) {
	if err := b.tg.EditMessageText(ctx, chatID, msgID, text, markup); err != nil &&
		!strings.Contains(err.Error(), "message is not modified") {
		b.log.Error("editMessageText", "err", err)
	}
}

// Notify пишет пользователю — этим пользуется планировщик.
func (b *Bot) Notify(ctx context.Context, userID int64, text string) error {
	_, err := b.tg.SendMessage(ctx, userID, text, nil)
	return err
}

// user перечитывает пользователя из реестра: настройки могли поменяться,
// пока шёл длинный запрос к ASR или LLM.
func (b *Bot) user(id int64) (*model.User, bool) { return b.users.Get(id) }

// stats собирает агрегаты за период; общий код для /s, /w и планировщика.
func (b *Bot) stats(user *model.User, from, to string) (*report.Stats, error) {
	days, err := b.st.Days(user.ID, from, to)
	if err != nil {
		return nil, err
	}
	// Стрики считаем по длинному окну, иначе серия в 40 дней покажется семёркой.
	daysAll, err := b.st.Days(user.ID, report.AddDays(to, -400), to)
	if err != nil {
		return nil, err
	}
	money, err := b.st.Money(user.ID, from, to)
	if err != nil {
		return nil, err
	}
	moneyAll, err := b.st.MoneyUntil(user.ID, to)
	if err != nil {
		return nil, err
	}
	notes, err := b.st.Notes(user.ID, from, to)
	if err != nil {
		return nil, err
	}
	return report.Build(report.Input{
		From: from, To: to,
		Days: days, DaysAll: daysAll,
		Money: money, MoneyAll: moneyAll,
		Notes:   notes,
		Today:   user.Today(),
		Cfg:     b.cfg,
		Fields:  user.Fields,
		Finance: user.Finance,
	}), nil
}

func (b *Bot) expireSessions() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, p := range b.pending {
		if time.Since(p.created) > pendingTTL {
			delete(b.pending, id)
		}
	}
	for id, c := range b.checkins {
		if time.Since(c.touched) > checkinTTL {
			delete(b.checkins, id)
		}
	}
	for id, e := range b.edits {
		if time.Since(e.created) > pendingTTL {
			delete(b.edits, id)
		}
	}
}
