package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/murluckk/self-reinvention/internal/model"
	"github.com/murluckk/self-reinvention/internal/parse"
	"github.com/murluckk/self-reinvention/internal/report"
	"github.com/murluckk/self-reinvention/internal/tg"
)

// commandMenu — меню команд в Telegram. Админские команды в меню не попадают:
// остальным они всё равно недоступны.
var commandMenu = []tg.Command{
	{Command: "c", Description: "вечерний опрос кнопками"},
	{Command: "d", Description: "запись дня: /d подъем 7:30 трен состояние 8"},
	{Command: "s", Description: "статус: сегодня, неделя, стрики"},
	{Command: "w", Description: "выгрузка за n дней в markdown"},
	{Command: "i", Description: "наблюдения: что влияет на состояние"},
	{Command: "review", Description: "AI-разбор последней недели"},
	{Command: "m", Description: "деньги: /m -1200 еда"},
	{Command: "undo", Description: "удалить последнюю заметку или трату"},
	{Command: "fields", Description: "что отслеживать"},
	{Command: "tz", Description: "часовой пояс"},
	{Command: "friends", Description: "серии друзей"},
	{Command: "share", Description: "делиться сериями с друзьями"},
	{Command: "password", Description: "новый пароль от дашборда"},
	{Command: "help", Description: "шпаргалка"},
}

func (b *Bot) handleMessage(ctx context.Context, user *model.User, m *tg.Message) {
	if m.Voice != nil || m.Audio != nil {
		v := m.Voice
		if v == nil {
			v = m.Audio
		}
		b.handleVoice(ctx, user, m, v)
		return
	}
	text := strings.TrimSpace(m.Text)
	if text == "" {
		text = strings.TrimSpace(m.Caption)
	}
	if text == "" {
		b.reply(ctx, m.Chat.ID, "Пустое сообщение — нечего записывать.")
		return
	}
	if !strings.HasPrefix(text, "/") {
		// Текст во время правки голосовой записи или вечернего опроса — это
		// ответ на заданный вопрос, а не заметка.
		if b.answerEdit(ctx, user, m.Chat.ID, text) || b.answerCheckin(ctx, user, m.Chat.ID, text) {
			return
		}
		b.handleNote(ctx, user, m.Chat.ID, text)
		return
	}
	cmd, args := splitCommand(text)
	chat := m.Chat.ID
	switch cmd {
	case "start", "help":
		b.reply(ctx, chat, helpText(user, b.isAdmin(user)))
	case "d":
		b.handleDay(ctx, user, chat, args)
	case "c", "checkin":
		b.handleCheckinCommand(ctx, user, chat, args)
	case "m":
		if !user.Finance {
			b.reply(ctx, chat, "Финансы у тебя выключены. Включить можно в /fields.")
			return
		}
		b.handleMoney(ctx, user, chat, args)
	case "s":
		b.handleStatus(ctx, user, chat)
	case "w":
		b.handleWeekly(ctx, user, chat, args)
	case "i", "insights":
		b.handleInsights(ctx, user, chat)
	case "review":
		b.handleReview(ctx, user, chat)
	case "undo":
		b.handleUndo(ctx, user, chat)
	case "fields":
		b.handleFields(ctx, user, chat)
	case "tz":
		b.handleTimezone(ctx, user, chat, args)
	case "password":
		b.handlePassword(ctx, user, chat)
	case "share":
		b.handleShare(ctx, user, chat)
	case "friends", "f":
		b.handleFriends(ctx, user, chat)
	case "invite", "users", "remove", "health":
		if !b.isAdmin(user) {
			b.reply(ctx, chat, fmt.Sprintf("Не знаю команду /%s. /help покажет, что я умею.", cmd))
			return
		}
		b.handleAdmin(ctx, user, chat, cmd, args)
	default:
		b.reply(ctx, chat, fmt.Sprintf("Не знаю команду /%s. /help покажет, что я умею.", cmd))
	}
}

// splitCommand отрезает имя команды вместе с суффиксом @bot_name.
func splitCommand(text string) (cmd, args string) {
	text = strings.TrimPrefix(text, "/")
	if i := strings.IndexAny(text, " \n"); i >= 0 {
		cmd, args = text[:i], strings.TrimSpace(text[i+1:])
	} else {
		cmd = text
	}
	if i := strings.Index(cmd, "@"); i >= 0 {
		cmd = cmd[:i]
	}
	return strings.ToLower(cmd), args
}

func (b *Bot) handleNote(ctx context.Context, user *model.User, chatID int64, text string) {
	tag, body := parse.ParseNote(text)
	if body == "" && tag == "" {
		b.reply(ctx, chatID, "Пустая заметка.")
		return
	}
	n := &model.Note{TS: user.Now(), Date: user.Today(), Tag: tag, Text: body}
	if err := b.st.AddNote(user.ID, n); err != nil {
		b.log.Error("сохранение заметки", "err", err)
		b.reply(ctx, chatID, "Не смог записать заметку: "+err.Error())
		return
	}
	if tag != "" {
		b.reply(ctx, chatID, "✍️ записал в #"+tag)
		return
	}
	b.reply(ctx, chatID, "✍️ записал")
}

func (b *Bot) handleDay(ctx context.Context, user *model.User, chatID int64, args string) {
	res := parse.ParseDayIn(args, user.Today(), user.Fields)
	if len(res.Errors) > 0 {
		b.reply(ctx, chatID, "⚠️ "+strings.Join(res.Errors, "\n⚠️ ")+"\n\n/help — шпаргалка")
	}
	if res.Day.Empty() {
		return
	}
	if err := b.st.UpsertDay(user.ID, res.Day); err != nil {
		b.log.Error("запись дня", "err", err)
		b.reply(ctx, chatID, "Не смог записать: "+err.Error())
		return
	}
	b.reply(ctx, chatID, b.dayConfirmation(ctx, user, res.Day.Date, res.Day))
}

// dayConfirmation показывает, что записалось, и что за этот день ещё пусто —
// чтобы дописать одним сообщением, не заходя в /s.
func (b *Bot) dayConfirmation(ctx context.Context, user *model.User, date string, written *model.Day) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "✅ %s (%s)\n%s", date, report.Weekday(date), parse.Summary(written))
	full, err := b.st.GetDay(user.ID, date)
	if err != nil {
		b.log.Error("чтение дня", "err", err)
		return sb.String()
	}
	if missing := full.MissingIn(user.Fields); len(missing) > 0 {
		names := make([]string, 0, len(missing))
		for _, f := range missing {
			names = append(names, f.Keys[0])
		}
		fmt.Fprintf(&sb, "\nОсталось: %s · /c — дозаполнить кнопками", strings.Join(names, ", "))
	} else {
		sb.WriteString("\nДень заполнен целиком.")
		b.onDayClosed(ctx, user, date)
	}
	return sb.String()
}

func (b *Bot) handleMoney(ctx context.Context, user *model.User, chatID int64, args string) {
	m, err := parse.ParseMoney(args, user.Today())
	if err != nil {
		b.reply(ctx, chatID, "⚠️ "+err.Error())
		return
	}
	m.TS = user.Now()
	if err := b.st.AddMoney(user.ID, m); err != nil {
		b.log.Error("запись денег", "err", err)
		b.reply(ctx, chatID, "Не смог записать: "+err.Error())
		return
	}
	text := "💰 " + parse.FormatMoney(m)
	if m.Kind == model.MoneySaving {
		if all, err := b.st.MoneyUntil(user.ID, user.Today()); err == nil {
			var capital float64
			for _, x := range all {
				if x.Kind == model.MoneySaving && x.Currency == m.Currency {
					capital += x.Amount
				}
			}
			text += fmt.Sprintf("\nВсего накоплено: %s %s", parse.FormatAmount(capital), m.Currency)
		}
	}
	b.reply(ctx, chatID, text)
}

func (b *Bot) handleStatus(ctx context.Context, user *model.User, chatID int64) {
	today := user.Today()
	day, err := b.st.GetDay(user.ID, today)
	if err != nil {
		b.reply(ctx, chatID, "Не смог прочитать запись: "+err.Error())
		return
	}
	st, err := b.stats(user, report.AddDays(today, -6), today)
	if err != nil {
		b.reply(ctx, chatID, "Не смог собрать статистику: "+err.Error())
		return
	}
	b.reply(ctx, chatID, report.Status(day, st))
}

func (b *Bot) handleWeekly(ctx context.Context, user *model.User, chatID int64, args string) {
	n := b.cfg.WeeklyReportN
	if args != "" {
		v, err := strconv.Atoi(strings.Fields(args)[0])
		if err != nil || v <= 0 || v > 366 {
			b.reply(ctx, chatID, "⚠️ /w ждёт число дней от 1 до 366, например /w 30")
			return
		}
		n = v
	}
	if _, err := b.sendReport(ctx, user, chatID, n); err != nil {
		b.log.Error("выгрузка", "err", err)
		b.reply(ctx, chatID, "Не смог собрать выгрузку: "+err.Error())
	}
}

// sendReport собирает markdown за n дней и отправляет файлом.
func (b *Bot) sendReport(ctx context.Context, user *model.User, chatID int64, n int) (*report.Stats, error) {
	to := user.Today()
	from := report.AddDays(to, -(n - 1))
	st, err := b.stats(user, from, to)
	if err != nil {
		return nil, err
	}
	md := report.Markdown(st)
	name := fmt.Sprintf("razbor_%s_%s.md", from, to)
	caption := fmt.Sprintf("Разбор %s — %s: %d/%d дней, флагов: %d", from, to, st.FilledDays, st.TotalDays, len(st.Flags))
	return st, b.tg.SendDocument(ctx, chatID, name, []byte(md), caption)
}

func (b *Bot) handleUndo(ctx context.Context, user *model.User, chatID int64) {
	u, err := b.st.LastUndoable(user.ID)
	if err != nil {
		b.reply(ctx, chatID, "Не смог посмотреть последние записи: "+err.Error())
		return
	}
	if u == nil {
		b.reply(ctx, chatID, "Отменять нечего: ни заметок, ни денежных записей.")
		return
	}
	if err := b.st.Delete(user.ID, u.Kind, u.ID); err != nil {
		b.reply(ctx, chatID, "Не смог удалить: "+err.Error())
		return
	}
	what := "заметку"
	if u.Kind == "money" {
		what = "денежную запись"
	}
	b.reply(ctx, chatID, fmt.Sprintf("🗑 Удалил %s: %s", what, u.Descr))
}

// RemindDay — вечернее напоминание; вызывается планировщиком. Вместо текста
// с синтаксисом сразу начинает опрос кнопками по незаполненным полям.
func (b *Bot) RemindDay(ctx context.Context, userID int64) error {
	user, ok := b.user(userID)
	if !ok {
		return nil
	}
	date := user.Today()
	day, err := b.st.GetDay(user.ID, date)
	if err != nil {
		return err
	}
	if len(day.MissingIn(user.Fields)) == 0 {
		return b.Notify(ctx, user.ID, "🌙 День закрыт полностью. Ничего не жду.")
	}
	header := "🌙 Пора подвести итоги дня. Можно кнопками, можно одним голосовым."
	if friends := b.closedFriends(user); friends != "" {
		header += "\n" + friends
	}
	return b.startCheckin(ctx, user, user.ID, date, header)
}

// SendWeekly отправляет автоматическую недельную выгрузку и AI-разбор.
func (b *Bot) SendWeekly(ctx context.Context, userID int64) error {
	user, ok := b.user(userID)
	if !ok {
		return nil
	}
	st, err := b.sendReport(ctx, user, user.ID, b.cfg.WeeklyReportN)
	if err != nil {
		return err
	}
	// Выгрузка уже ушла: ошибка разбора не должна заставить планировщик
	// повторить задачу и прислать файл второй раз.
	if review := b.review(ctx, st); review != "" {
		if err := b.Notify(ctx, user.ID, review); err != nil {
			b.log.Warn("отправка AI-разбора", "user_id", user.ID, "err", err)
		}
	}
	return nil
}
