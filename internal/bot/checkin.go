package bot

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/murluckk/self-reinvention/internal/model"
	"github.com/murluckk/self-reinvention/internal/parse"
	"github.com/murluckk/self-reinvention/internal/report"
	"github.com/murluckk/self-reinvention/internal/tg"
)

// checkin — вечерний опрос: бот по одному спрашивает незаполненные поля дня,
// ответы приходят кнопками или текстом и пишутся в базу сразу, без итогового
// «сохранить» — бросить опрос на середине ничего не ломает.
type checkin struct {
	date    string
	header  string
	chatID  int64
	msgID   int64
	current string          // колонка, о которой спросили последней
	skipped map[string]bool // «пропустить» до конца опроса
	touched time.Time
}

const checkinTTL = 3 * time.Hour

var isoDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// handleCheckinCommand — /c [вчера|YYYY-MM-DD].
func (b *Bot) handleCheckinCommand(ctx context.Context, user *model.User, chatID int64, args string) {
	date := user.Today()
	switch arg := strings.ToLower(strings.TrimSpace(args)); {
	case arg == "":
	case arg == "вчера":
		date = report.AddDays(date, -1)
	case isoDate.MatchString(arg):
		date = arg
	default:
		b.reply(ctx, chatID, "⚠️ /c понимает дату YYYY-MM-DD или «вчера», например /c вчера")
		return
	}
	if err := b.startCheckin(ctx, user, chatID, date, "📝 Итоги дня"); err != nil {
		b.reply(ctx, chatID, "Не смог начать опрос: "+err.Error())
	}
}

func (b *Bot) startCheckin(ctx context.Context, user *model.User, chatID int64, date, header string) error {
	day, err := b.st.GetDay(user.ID, date)
	if err != nil {
		return err
	}
	if len(day.MissingIn(user.Fields)) == 0 {
		b.reply(ctx, chatID, fmt.Sprintf("✅ %s (%s) уже заполнен целиком. Поправить: /d %s ключ значение",
			date, report.Weekday(date), date))
		return nil
	}
	c := &checkin{date: date, header: header, chatID: chatID, skipped: map[string]bool{}, touched: time.Now()}
	text, kb := b.checkinView(user, c, day)
	m := b.send(ctx, chatID, text, kb)
	if m == nil {
		return fmt.Errorf("telegram не принял сообщение")
	}
	c.msgID = m.MessageID
	b.mu.Lock()
	b.checkins[user.ID] = c
	b.mu.Unlock()
	return nil
}

// checkinView рисует текущий шаг опроса: что уже записано и следующий вопрос.
func (b *Bot) checkinView(user *model.User, c *checkin, day *model.Day) (string, *tg.InlineKeyboardMarkup) {
	var next *model.Field
	left := 0
	for _, f := range day.MissingIn(user.Fields) {
		if c.skipped[f.DB] {
			continue
		}
		left++
		if next == nil {
			f := f
			next = &f
		}
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s · %s (%s)\n", c.header, c.date, report.Weekday(c.date))
	if done := day.SetFieldsIn(user.Fields); len(done) > 0 {
		sb.WriteString("\n")
		for _, f := range done {
			if f.DB != "note" {
				fmt.Fprintf(&sb, "✓ %s: %s\n", f.Label, f.FormatValue(day))
			}
		}
	}
	if next == nil {
		c.current = ""
		if len(day.MissingIn(user.Fields)) == 0 {
			sb.WriteString("\n✅ День заполнен целиком. Спасибо!")
		} else {
			sb.WriteString("\nГотово. Пропущенное можно дописать позже: /c")
		}
		return sb.String(), nil
	}
	c.current = next.DB
	fmt.Fprintf(&sb, "\n%s", fieldQuestion(next))
	if left > 1 {
		fmt.Fprintf(&sb, "\nОсталось вопросов: %d", left)
	}
	prefix := "c:" + c.date + ":" + next.DB + ":"
	return sb.String(), fieldKeyboard(next, prefix, []tg.InlineKeyboardButton{
		tg.Button("⏭ Пропустить", prefix+"-"),
		tg.Button("✖️ Хватит", prefix+"!"),
	})
}

// handleCheckinCallback обрабатывает c:<дата>:<колонка>:<значение>. Всё нужное
// для записи лежит в самой кнопке, поэтому опрос переживает рестарт бота:
// теряется только список пропущенных вопросов.
func (b *Bot) handleCheckinCallback(ctx context.Context, user *model.User, cb *tg.CallbackQuery) {
	parts := strings.SplitN(cb.Data, ":", 4)
	if len(parts) != 4 || cb.Message == nil {
		_ = b.tg.AnswerCallbackQuery(ctx, cb.ID, "")
		return
	}
	date, col, val := parts[1], parts[2], parts[3]
	f, ok := model.FieldByColumn(col)
	if !ok {
		_ = b.tg.AnswerCallbackQuery(ctx, cb.ID, "Такого поля больше нет")
		return
	}

	b.mu.Lock()
	c := b.checkins[user.ID]
	if c == nil || c.date != date || c.msgID != cb.Message.MessageID {
		c = &checkin{date: date, header: "📝 Итоги дня", skipped: map[string]bool{}}
		b.checkins[user.ID] = c
	}
	c.chatID, c.msgID, c.touched = cb.Message.Chat.ID, cb.Message.MessageID, time.Now()
	b.mu.Unlock()

	switch val {
	case "!":
		b.dropCheckin(user.ID)
		_ = b.tg.AnswerCallbackQuery(ctx, cb.ID, "Ок")
		day, _ := b.st.GetDay(user.ID, date)
		text := fmt.Sprintf("%s · %s\nОстановились. Остальное можно дописать позже: /c", c.header, date)
		if day != nil && !day.EmptyIn(user.Fields) {
			text += "\n\n" + parse.Summary(day)
		}
		b.edit(ctx, c.chatID, c.msgID, text, nil)
		return
	case "-":
		b.mu.Lock()
		c.skipped[col] = true
		b.mu.Unlock()
		_ = b.tg.AnswerCallbackQuery(ctx, cb.ID, "Пропустил")
	default:
		if err := b.writeField(user, date, f, val); err != nil {
			_ = b.tg.AnswerCallbackQuery(ctx, cb.ID, "Не записалось: "+err.Error())
			return
		}
		_ = b.tg.AnswerCallbackQuery(ctx, cb.ID, "")
	}
	b.refreshCheckin(ctx, user, c, false)
}

// answerCheckin принимает текстовый ответ на текущий вопрос опроса.
func (b *Bot) answerCheckin(ctx context.Context, user *model.User, chatID int64, text string) bool {
	b.mu.Lock()
	c := b.checkins[user.ID]
	b.mu.Unlock()
	if c == nil || c.current == "" || c.chatID != chatID {
		return false
	}
	f, ok := model.FieldByColumn(c.current)
	if !ok {
		return false
	}
	if err := b.writeField(user, c.date, f, text); err != nil {
		// Скорее всего, это не ответ, а мысль, которую человек хотел записать:
		// сохраняем заметкой, чтобы текст не потерялся.
		tag, body := parse.ParseNote(text)
		n := &model.Note{TS: user.Now(), Date: user.Today(), Tag: tag, Text: body}
		if nerr := b.st.AddNote(user.ID, n); nerr != nil {
			b.reply(ctx, chatID, fmt.Sprintf("Не понял ответ про «%s» (%v), и заметкой сохранить не вышло: %v", f.Label, err, nerr))
			return true
		}
		b.reply(ctx, chatID, fmt.Sprintf("✍️ Не похоже на ответ про «%s» (%v) — сохранил как заметку.\nОтветить можно кнопкой выше или текстом ещё раз.", f.Label, err))
		return true
	}
	c.touched = time.Now()
	// Ответ текстом ушёл ниже вопроса: старое сообщение гасим, а следующий
	// вопрос задаём новым, чтобы он оказался внизу чата.
	b.refreshCheckin(ctx, user, c, true)
	return true
}

func (b *Bot) refreshCheckin(ctx context.Context, user *model.User, c *checkin, resend bool) {
	day, err := b.st.GetDay(user.ID, c.date)
	if err != nil {
		b.reply(ctx, c.chatID, "Не смог прочитать день: "+err.Error())
		return
	}
	text, kb := b.checkinView(user, c, day)
	if resend {
		b.edit(ctx, c.chatID, c.msgID, fmt.Sprintf("%s · %s — продолжение ниже ⬇️", c.header, c.date), nil)
		if m := b.send(ctx, c.chatID, text, kb); m != nil {
			c.msgID = m.MessageID
		}
	} else {
		b.edit(ctx, c.chatID, c.msgID, text, kb)
	}
	if kb == nil {
		b.dropCheckin(user.ID)
		if len(day.MissingIn(user.Fields)) == 0 {
			b.onDayClosed(ctx, user, c.date)
		}
	}
}

func (b *Bot) dropCheckin(userID int64) {
	b.mu.Lock()
	delete(b.checkins, userID)
	b.mu.Unlock()
}

// writeField записывает одно значение дня.
func (b *Bot) writeField(user *model.User, date string, f *model.Field, val string) error {
	if !user.Fields.Has(f.DB) {
		return fmt.Errorf("поле %s у тебя выключено", f.Label)
	}
	d := &model.Day{Date: date}
	if err := parse.SetValue(d, f, val); err != nil {
		return err
	}
	return b.st.UpsertDay(user.ID, d)
}
