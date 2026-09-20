package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/murluckk/self-reinvention/internal/config"
	"github.com/murluckk/self-reinvention/internal/model"
	"github.com/murluckk/self-reinvention/internal/parse"
	"github.com/murluckk/self-reinvention/internal/report"
	"github.com/murluckk/self-reinvention/internal/tg"
)

var commandDescriptions = map[string]string{
	"d":    "запись дня: /d подъем 7:30 трен состояние 8",
	"m":    "деньги: /m -1200 еда",
	"s":    "статус: сегодня, неделя, стрики",
	"w":    "выгрузка за n дней в markdown",
	"undo": "удалить последнюю заметку или трату",
	"help": "шпаргалка по синтаксису",
}

func (b *Bot) handleMessage(ctx context.Context, userID int64, m *tg.Message) {
	if m.Voice != nil || m.Audio != nil {
		v := m.Voice
		if v == nil {
			v = m.Audio
		}
		b.handleVoice(ctx, userID, m, v)
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
		b.handleNote(ctx, userID, m.Chat.ID, text)
		return
	}
	cmd, args := splitCommand(text)
	switch cmd {
	case "start", "help":
		b.reply(ctx, m.Chat.ID, helpTextFor(b.profile(userID)))
	case "d":
		b.handleDay(ctx, userID, m.Chat.ID, args)
	case "m":
		if b.profile(userID) != config.ProfilePasha {
			b.reply(ctx, m.Chat.ID, "Для твоего профиля деньги сейчас не отслеживаются.")
			return
		}
		b.handleMoney(ctx, userID, m.Chat.ID, args)
	case "s":
		b.handleStatus(ctx, userID, m.Chat.ID)
	case "w":
		b.handleWeekly(ctx, userID, m.Chat.ID, args)
	case "undo":
		b.handleUndo(ctx, userID, m.Chat.ID)
	default:
		b.reply(ctx, m.Chat.ID, fmt.Sprintf("Не знаю команду /%s. /help покажет, что я умею.", cmd))
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

func (b *Bot) handleNote(ctx context.Context, userID, chatID int64, text string) {
	tag, body := parse.ParseNote(text)
	if body == "" && tag == "" {
		b.reply(ctx, chatID, "Пустая заметка.")
		return
	}
	n := &model.Note{TS: b.cfg.Now(), Date: b.cfg.Today(), Tag: tag, Text: body}
	if err := b.st.AddNote(userID, n); err != nil {
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

func (b *Bot) handleDay(ctx context.Context, userID, chatID int64, args string) {
	res := parse.ParseDayFor(args, b.cfg.Today(), b.profile(userID))
	if len(res.Errors) > 0 {
		b.reply(ctx, chatID, "⚠️ "+strings.Join(res.Errors, "\n⚠️ ")+"\n\n/help — шпаргалка")
	}
	if res.Day.Empty() {
		return
	}
	if err := b.st.UpsertDay(userID, res.Day); err != nil {
		b.log.Error("запись дня", "err", err)
		b.reply(ctx, chatID, "Не смог записать: "+err.Error())
		return
	}
	b.reply(ctx, chatID, b.dayConfirmation(userID, res.Day.Date, res.Day))
}

// dayConfirmation показывает, что записалось, и что за этот день ещё пусто —
// чтобы дописать одним сообщением, не заходя в /s.
func (b *Bot) dayConfirmation(userID int64, date string, written *model.Day) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "✅ %s (%s)\n%s", date, report.Weekday(date), parse.Summary(written))
	full, err := b.st.GetDay(userID, date)
	if err != nil {
		b.log.Error("чтение дня", "err", err)
		return sb.String()
	}
	if missing := full.MissingFieldsFor(b.profile(userID)); len(missing) > 0 {
		names := make([]string, 0, len(missing))
		for _, f := range missing {
			names = append(names, f.Keys[0])
		}
		fmt.Fprintf(&sb, "\nОсталось: %s", strings.Join(names, ", "))
	} else {
		sb.WriteString("\nДень заполнен целиком.")
	}
	return sb.String()
}

func (b *Bot) handleMoney(ctx context.Context, userID, chatID int64, args string) {
	m, err := parse.ParseMoney(args, b.cfg.Today())
	if err != nil {
		b.reply(ctx, chatID, "⚠️ "+err.Error())
		return
	}
	m.TS = b.cfg.Now()
	if err := b.st.AddMoney(userID, m); err != nil {
		b.log.Error("запись денег", "err", err)
		b.reply(ctx, chatID, "Не смог записать: "+err.Error())
		return
	}
	text := "💰 " + parse.FormatMoney(m)
	if m.Kind == model.MoneySaving {
		if all, err := b.st.MoneyUntil(userID, b.cfg.Today()); err == nil {
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

func (b *Bot) handleStatus(ctx context.Context, userID, chatID int64) {
	today := b.cfg.Today()
	day, err := b.st.GetDay(userID, today)
	if err != nil {
		b.reply(ctx, chatID, "Не смог прочитать запись: "+err.Error())
		return
	}
	st, err := b.stats(userID, report.AddDays(today, -6), today)
	if err != nil {
		b.reply(ctx, chatID, "Не смог собрать статистику: "+err.Error())
		return
	}
	b.reply(ctx, chatID, report.Status(day, st))
}

func (b *Bot) handleWeekly(ctx context.Context, userID, chatID int64, args string) {
	n := b.cfg.WeeklyReportN
	if args != "" {
		v, err := strconv.Atoi(strings.Fields(args)[0])
		if err != nil || v <= 0 || v > 366 {
			b.reply(ctx, chatID, "⚠️ /w ждёт число дней от 1 до 366, например /w 30")
			return
		}
		n = v
	}
	if err := b.sendReport(ctx, userID, chatID, n); err != nil {
		b.log.Error("выгрузка", "err", err)
		b.reply(ctx, chatID, "Не смог собрать выгрузку: "+err.Error())
	}
}

// sendReport собирает markdown за n дней и отправляет файлом.
func (b *Bot) sendReport(ctx context.Context, userID, chatID int64, n int) error {
	to := b.cfg.Today()
	from := report.AddDays(to, -(n - 1))
	st, err := b.stats(userID, from, to)
	if err != nil {
		return err
	}
	md := report.Markdown(st)
	name := fmt.Sprintf("razbor_%s_%s.md", from, to)
	caption := fmt.Sprintf("Разбор %s — %s: %d/%d дней, флагов: %d", from, to, st.FilledDays, st.TotalDays, len(st.Flags))
	return b.tg.SendDocument(ctx, chatID, name, []byte(md), caption)
}

func (b *Bot) handleUndo(ctx context.Context, userID, chatID int64) {
	u, err := b.st.LastUndoable(userID)
	if err != nil {
		b.reply(ctx, chatID, "Не смог посмотреть последние записи: "+err.Error())
		return
	}
	if u == nil {
		b.reply(ctx, chatID, "Отменять нечего: ни заметок, ни денежных записей.")
		return
	}
	if err := b.st.Delete(userID, u.Kind, u.ID); err != nil {
		b.reply(ctx, chatID, "Не смог удалить: "+err.Error())
		return
	}
	what := "заметку"
	if u.Kind == "money" {
		what = "денежную запись"
	}
	b.reply(ctx, chatID, fmt.Sprintf("🗑 Удалил %s: %s", what, u.Descr))
}

// RemindDay — напоминание закрыть день; вызывается планировщиком.
func (b *Bot) RemindDay(ctx context.Context, userID int64) error {
	date := b.cfg.Today()
	day, err := b.st.GetDay(userID, date)
	if err != nil {
		return err
	}
	missing := day.MissingFieldsFor(b.profile(userID))
	if len(missing) == 0 {
		return b.Notify(ctx, userID, "🌙 День закрыт полностью. Ничего не жду.")
	}
	names := make([]string, 0, len(missing))
	for _, f := range missing {
		names = append(names, f.Keys[0])
	}
	text := fmt.Sprintf("🌙 Пора закрыть %s (%s).\nНе хватает: %s\n\nОдной строкой: /d %s\nИли просто наговори голосовым.",
		date, report.Weekday(date), strings.Join(names, ", "), exampleFor(missing))
	return b.Notify(ctx, userID, text)
}

// exampleFor строит подсказку из первых незаполненных полей, чтобы не
// вспоминать синтаксис в полночь.
func exampleFor(missing []model.Field) string {
	var parts []string
	for i, f := range missing {
		if i >= 4 {
			break
		}
		switch f.Kind {
		case model.KindBool:
			parts = append(parts, f.Keys[0])
		case model.KindTime:
			parts = append(parts, f.Keys[0]+" 07:00")
		case model.KindString:
			parts = append(parts, f.Keys[0]+" зал")
		default:
			parts = append(parts, f.Keys[0]+" …")
		}
	}
	return strings.Join(parts, " ")
}

// SendWeekly отправляет автоматическую недельную выгрузку.
func (b *Bot) SendWeekly(ctx context.Context, userID int64) error {
	return b.sendReport(ctx, userID, userID, b.cfg.WeeklyReportN)
}
