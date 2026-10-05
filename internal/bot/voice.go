package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/murluckk/self-reinvention/internal/model"
	"github.com/murluckk/self-reinvention/internal/parse"
	"github.com/murluckk/self-reinvention/internal/report"
	"github.com/murluckk/self-reinvention/internal/tg"
)

// pending — разобранное голосовое, ждущее подтверждения. Ничего не пишем в
// базу, пока человек не нажал «Записать»: Whisper по-русски хорош, но на числах
// ошибается, а мусор в данных дороже одного нажатия.
type pending struct {
	userID  int64
	voice   *parse.Voice
	raw     string
	created time.Time
}

const pendingTTL = 6 * time.Hour

func (b *Bot) putPending(p *pending) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	id := fmt.Sprintf("%d", b.seq)
	b.pending[id] = p
	return id
}

func (b *Bot) getPending(id string) *pending {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.pending[id]
}

func (b *Bot) dropPending(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.pending, id)
}

// handleVoice ведёт голосовое по пайплайну: скачать → сконвертировать → ASR →
// разбор → подтверждение. На любом обрыве данные не теряются: сообщение
// оседает заметкой с пометкой, что распознать не удалось.
func (b *Bot) handleVoice(ctx context.Context, user *model.User, m *tg.Message, v *tg.Voice) {
	userID := user.ID
	status, err := b.tg.SendMessage(ctx, m.Chat.ID, "🎧 Слушаю…", nil)
	if err != nil {
		b.log.Error("sendMessage", "err", err)
		return
	}
	edit := func(text string, markup *tg.InlineKeyboardMarkup) {
		if err := b.tg.EditMessageText(ctx, m.Chat.ID, status.MessageID, text, markup); err != nil {
			b.log.Error("editMessageText", "err", err)
		}
	}

	text, err := b.transcribe(ctx, v)
	if err != nil {
		b.log.Error("распознавание", "err", err)
		b.alert(ctx, "asr", "Голосовое не распознано ни облаком, ни локально: "+err.Error())
		edit(b.saveUnrecognized(user, v, err), nil)
		return
	}

	date := user.Today()
	res := parse.ParseVoiceRegex(text, date)
	// Каждую расшифровку отдаём LLM; регулярки остаются быстрым фолбэком на
	// случай недоступности API и дополняют пропущенные моделью поля.
	if b.llm != nil {
		lctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		llmRes, lerr := b.llm.Parse(lctx, text, date)
		cancel()
		switch {
		case lerr != nil:
			b.log.Error("llm-парсер", "err", lerr)
			b.alert(ctx, "llm", "LLM-разбор голосового упал, работаю на регулярках: "+lerr.Error())
			if res.Recogn == 0 {
				res.Note = text // ничего не поняли — сохраним хотя бы дословно
				res.Level = "none"
			}
		case llmRes.Count() >= res.Count():
			// regex мог зацепить то, что модель пропустила, — оставляем оба слоя
			model.MergeMissing(llmRes.Day, res.Day)
			llmRes.Money = append(llmRes.Money, res.Money...)
			llmRes.Level = "llm"
			if res.Recogn > 0 {
				llmRes.Level = "llm+regex"
			}
			res = llmRes
		}
	}
	res.Day.Keep(user.Fields)
	if !user.Finance {
		res.Money = nil
	}
	res.Recogn = res.Count()
	if res.Count() == 0 && strings.TrimSpace(res.Note) == "" {
		res.Note = text
	}
	b.log.Info("голосовое разобрано", "level", res.Level, "fields", res.Count(), "chars", len(text))

	parsed, _ := json.Marshal(voiceDump(res))
	if err := b.st.SaveVoice(userID, date, text, string(parsed), res.Level); err != nil {
		b.log.Error("сохранение расшифровки", "err", err)
	}

	id := b.putPending(&pending{userID: userID, voice: res, raw: text, created: time.Now()})
	edit(confirmationText(res, false, ""), confirmKeyboard(id))
}

// transcribe скачивает голосовое, конвертирует в 16 кГц моно wav и отдаёт в ASR.
func (b *Bot) transcribe(ctx context.Context, v *tg.Voice) (string, error) {
	text, cloudErr, err := b.transcribeWithFallback(ctx, v)
	if err == nil && cloudErr != nil {
		b.alert(ctx, "asr-cloud", "Облачное распознавание недоступно, работаю на локальном Whisper: "+cloudErr.Error())
	}
	return text, err
}

func (b *Bot) transcribeWithFallback(ctx context.Context, v *tg.Voice) (text string, cloudErr error, err error) {
	if b.asr == nil {
		return "", nil, fmt.Errorf("asr не настроен")
	}
	f, err := b.tg.GetFile(ctx, v.FileID)
	if err != nil {
		return "", nil, err
	}
	data, err := b.tg.Download(ctx, f.FilePath)
	if err != nil {
		return "", nil, err
	}
	target := b.asr
	if b.asr.DirectAudio() {
		name := cloudAudioName(f.FilePath)
		if text, err := b.asr.Transcribe(ctx, data, name); err == nil {
			return text, nil, nil
		} else {
			cloudErr = err
			b.log.Warn("облачное распознавание недоступно, пробую локальное", "err", err)
		}
		target = b.asrFallback
		if target == nil {
			return "", nil, cloudErr
		}
	}
	dir, err := os.MkdirTemp("", "voice")
	if err != nil {
		return "", cloudErr, err
	}
	defer os.RemoveAll(dir)
	in := filepath.Join(dir, "in"+ext(f.FilePath))
	out := filepath.Join(dir, "out.wav")
	if err := os.WriteFile(in, data, 0o600); err != nil {
		return "", cloudErr, err
	}
	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(cctx, "ffmpeg", "-y", "-loglevel", "error", "-i", in, "-ar", "16000", "-ac", "1", out)
	if stderr, err := cmd.CombinedOutput(); err != nil {
		if cloudErr != nil {
			return "", cloudErr, fmt.Errorf("облачный ASR: %v; локальный ffmpeg: %v: %s", cloudErr, err, strings.TrimSpace(string(stderr)))
		}
		return "", nil, fmt.Errorf("ffmpeg: %v: %s", err, strings.TrimSpace(string(stderr)))
	}
	wav, err := os.ReadFile(out)
	if err != nil {
		return "", cloudErr, err
	}
	text, err = target.Transcribe(ctx, wav, "voice.wav")
	if err != nil && cloudErr != nil {
		return "", cloudErr, fmt.Errorf("облачный ASR: %v; локальный ASR: %w", cloudErr, err)
	}
	return text, cloudErr, err
}

func cloudAudioName(path string) string {
	name := filepath.Base(path)
	if name == "." || name == "" {
		return "voice.ogg"
	}
	if e := filepath.Ext(name); strings.EqualFold(e, ".oga") {
		return strings.TrimSuffix(name, e) + ".ogg"
	}
	return name
}

func ext(path string) string {
	if e := filepath.Ext(path); e != "" {
		return e
	}
	return ".oga"
}

// saveUnrecognized — фолбэк: ни одно сообщение не должно пропасть из-за
// упавшего сервиса. Сам файл остаётся на серверах Telegram, в заметке лежит
// его id, так что расшифровать можно будет позже.
func (b *Bot) saveUnrecognized(user *model.User, v *tg.Voice, cause error) string {
	userID := user.ID
	n := &model.Note{
		TS:   user.Now(),
		Date: user.Today(),
		Tag:  "нераспознано",
		Text: fmt.Sprintf("голосовое %d сек, распознать не удалось (%v); file_id=%s", v.Duration, cause, v.FileID),
	}
	if err := b.st.AddNote(userID, n); err != nil {
		b.log.Error("сохранение нераспознанного", "err", err)
		return "⚠️ Распознать не удалось (" + cause.Error() + "), и заметку сохранить тоже не вышло: " + err.Error()
	}
	return "⚠️ Распознать не удалось: " + cause.Error() +
		"\nСохранил как заметку #нераспознано, голосовое осталось в чате — вернёмся к нему, когда сервис поднимется."
}

func confirmKeyboard(id string) *tg.InlineKeyboardMarkup {
	return &tg.InlineKeyboardMarkup{InlineKeyboard: [][]tg.InlineKeyboardButton{
		{tg.Button("✅ Записать", "v:ok:"+id), tg.Button("✖️ Отменить", "v:no:"+id)},
		{tg.Button("✏️ Исправить", "v:ed:"+id), tg.Button("📝 Показать текст", "v:raw:"+id)},
	}}
}

// confirmationText показывает разобранное по-человечески. showRaw добавляет
// сырую расшифровку — это отладка ASR: видно, где модель услышала не то.
func confirmationText(v *parse.Voice, showRaw bool, raw string) string {
	var b strings.Builder
	b.WriteString("🎤 Разобрал так:\n\n")
	if v.Day != nil && !v.Day.Empty() {
		fmt.Fprintf(&b, "%s (%s)\n%s", v.Day.Date, report.Weekday(v.Day.Date), parse.Summary(v.Day))
	}
	for _, m := range v.Money {
		b.WriteString("💰 " + parse.FormatMoney(m) + "\n")
	}
	if strings.TrimSpace(v.Note) != "" {
		b.WriteString("✍️ заметка: " + v.Note + "\n")
	}
	if v.Day.Empty() && len(v.Money) == 0 && strings.TrimSpace(v.Note) == "" {
		b.WriteString("(ничего не распознал)\n")
	}
	fmt.Fprintf(&b, "\nуровень разбора: %s", v.Level)
	if showRaw {
		b.WriteString("\n\nСырая расшифровка:\n" + raw)
	}
	return b.String()
}

// voiceDump — то, что кладём в базу рядом с сырой расшифровкой.
func voiceDump(v *parse.Voice) map[string]any {
	day := map[string]any{}
	for _, f := range v.Day.SetFields() {
		day[f.DB] = f.Get(v.Day)
	}
	out := map[string]any{"level": v.Level, "day": day}
	if len(v.Money) > 0 {
		out["money"] = v.Money
	}
	if v.Note != "" {
		out["note"] = v.Note
	}
	return out
}

func (b *Bot) handleCallback(ctx context.Context, user *model.User, cb *tg.CallbackQuery) {
	switch {
	case strings.HasPrefix(cb.Data, "v:"):
		b.handleVoiceCallback(ctx, user, cb)
	case strings.HasPrefix(cb.Data, "c:"):
		b.handleCheckinCallback(ctx, user, cb)
	case strings.HasPrefix(cb.Data, "f:"):
		b.handleFieldsCallback(ctx, user, cb)
	case strings.HasPrefix(cb.Data, "tz:"):
		b.handleTimezoneCallback(ctx, user, cb)
	default:
		_ = b.tg.AnswerCallbackQuery(ctx, cb.ID, "")
	}
}

// handleVoiceCallback — кнопки под карточкой голосового:
// v:<действие>:<id>[:<колонка>[:<значение>]].
func (b *Bot) handleVoiceCallback(ctx context.Context, user *model.User, cb *tg.CallbackQuery) {
	parts := strings.SplitN(cb.Data, ":", 5)
	if len(parts) < 3 {
		_ = b.tg.AnswerCallbackQuery(ctx, cb.ID, "")
		return
	}
	action, id := parts[1], parts[2]
	col, val := "", ""
	if len(parts) > 3 {
		col = parts[3]
	}
	if len(parts) > 4 {
		val = parts[4]
	}
	p := b.getPending(id)
	if p == nil || p.userID != user.ID {
		_ = b.tg.AnswerCallbackQuery(ctx, cb.ID, "Эта карточка уже неактуальна")
		if cb.Message != nil {
			b.edit(ctx, cb.Message.Chat.ID, cb.Message.MessageID, "⌛️ Карточка устарела — наговори заново.", nil)
		}
		return
	}
	chatID, msgID := user.ID, int64(0)
	if cb.Message != nil {
		chatID, msgID = cb.Message.Chat.ID, cb.Message.MessageID
	}
	card := func() { b.edit(ctx, chatID, msgID, confirmationText(p.voice, false, ""), confirmKeyboard(id)) }
	switch action {
	case "raw":
		_ = b.tg.AnswerCallbackQuery(ctx, cb.ID, "")
		b.edit(ctx, chatID, msgID, confirmationText(p.voice, true, p.raw), confirmKeyboard(id))
	case "no":
		b.dropPending(id)
		b.dropEdit(user.ID)
		_ = b.tg.AnswerCallbackQuery(ctx, cb.ID, "Отменено")
		b.edit(ctx, chatID, msgID, "✖️ Отменил, ничего не записал.\n\nРасшифровка:\n"+p.raw, nil)
	case "ok":
		b.dropPending(id)
		b.dropEdit(user.ID)
		_ = b.tg.AnswerCallbackQuery(ctx, cb.ID, "Записано")
		b.edit(ctx, chatID, msgID, b.applyVoice(ctx, p), nil)
	case "ed":
		_ = b.tg.AnswerCallbackQuery(ctx, cb.ID, "")
		b.dropEdit(user.ID)
		b.edit(ctx, chatID, msgID, confirmationText(p.voice, false, "")+"\n\n✏️ Какое поле поправить?",
			fieldPicker(user.Fields, p.voice.Day, "v:pick:"+id+":",
				[]tg.InlineKeyboardButton{tg.Button("↩️ Назад", "v:back:"+id)}))
	case "pick":
		f, ok := model.FieldByColumn(col)
		if !ok {
			_ = b.tg.AnswerCallbackQuery(ctx, cb.ID, "")
			return
		}
		_ = b.tg.AnswerCallbackQuery(ctx, cb.ID, "")
		b.mu.Lock()
		b.edits[user.ID] = &fieldEdit{pendingID: id, col: col, chatID: chatID, msgID: msgID, created: time.Now()}
		b.mu.Unlock()
		current := ""
		if f.IsSet(p.voice.Day) {
			current = "\nСейчас: " + f.FormatValue(p.voice.Day)
		}
		b.edit(ctx, chatID, msgID, fieldQuestion(f)+current,
			fieldKeyboard(f, "v:set:"+id+":"+col+":", []tg.InlineKeyboardButton{
				tg.Button("🗑 Убрать", "v:clr:"+id+":"+col), tg.Button("↩️ Назад", "v:back:"+id),
			}))
	case "set":
		f, ok := model.FieldByColumn(col)
		if !ok {
			_ = b.tg.AnswerCallbackQuery(ctx, cb.ID, "")
			return
		}
		if err := parse.SetValue(p.voice.Day, f, val); err != nil {
			_ = b.tg.AnswerCallbackQuery(ctx, cb.ID, "Не подошло: "+err.Error())
			return
		}
		b.dropEdit(user.ID)
		_ = b.tg.AnswerCallbackQuery(ctx, cb.ID, "Исправил")
		card()
	case "clr":
		if f, ok := model.FieldByColumn(col); ok {
			f.Clear(p.voice.Day)
		}
		b.dropEdit(user.ID)
		_ = b.tg.AnswerCallbackQuery(ctx, cb.ID, "Убрал")
		card()
	case "back":
		b.dropEdit(user.ID)
		_ = b.tg.AnswerCallbackQuery(ctx, cb.ID, "")
		card()
	default:
		_ = b.tg.AnswerCallbackQuery(ctx, cb.ID, "")
	}
}

// fieldEdit — ожидание текстового значения для поля голосовой карточки.
type fieldEdit struct {
	pendingID string
	col       string
	chatID    int64
	msgID     int64
	created   time.Time
}

func (b *Bot) dropEdit(userID int64) {
	b.mu.Lock()
	delete(b.edits, userID)
	b.mu.Unlock()
}

// answerEdit принимает значение поля, введённое текстом во время правки.
func (b *Bot) answerEdit(ctx context.Context, user *model.User, chatID int64, text string) bool {
	b.mu.Lock()
	e := b.edits[user.ID]
	b.mu.Unlock()
	if e == nil || e.chatID != chatID {
		return false
	}
	p := b.getPending(e.pendingID)
	f, ok := model.FieldByColumn(e.col)
	if p == nil || !ok {
		b.dropEdit(user.ID)
		return false
	}
	if err := parse.SetValue(p.voice.Day, f, text); err != nil {
		b.reply(ctx, chatID, fmt.Sprintf("Не понял значение для «%s»: %v. Попробуй ещё раз или нажми «Назад».", f.Label, err))
		return true
	}
	b.dropEdit(user.ID)
	b.edit(ctx, e.chatID, e.msgID, "✏️ Исправлено, карточка ниже ⬇️", nil)
	if m := b.send(ctx, chatID, confirmationText(p.voice, false, ""), confirmKeyboard(e.pendingID)); m == nil {
		b.reply(ctx, chatID, "Не смог показать карточку заново — наговори голосовое ещё раз.")
	}
	return true
}

// applyVoice пишет подтверждённое в базу и возвращает текст для чата.
func (b *Bot) applyVoice(ctx context.Context, p *pending) string {
	v := p.voice
	user, ok := b.user(p.userID)
	if !ok {
		return "Доступ к трекеру закрыт — ничего не записал."
	}
	var out []string
	if v.Day != nil && !v.Day.Empty() {
		if err := b.st.UpsertDay(p.userID, v.Day); err != nil {
			b.log.Error("запись дня из голосового", "err", err)
			out = append(out, "⚠️ запись дня не сохранилась: "+err.Error())
		} else {
			out = append(out, b.dayConfirmation(ctx, user, v.Day.Date, v.Day))
		}
	}
	for _, m := range v.Money {
		m.TS = user.Now()
		if err := b.st.AddMoney(p.userID, m); err != nil {
			b.log.Error("запись денег из голосового", "err", err)
			out = append(out, "⚠️ деньги не сохранились: "+err.Error())
			continue
		}
		out = append(out, "💰 "+parse.FormatMoney(m))
	}
	if note := strings.TrimSpace(v.Note); note != "" {
		n := &model.Note{TS: user.Now(), Date: user.Today(), Text: note}
		if err := b.st.AddNote(p.userID, n); err != nil {
			b.log.Error("запись заметки из голосового", "err", err)
			out = append(out, "⚠️ заметка не сохранилась: "+err.Error())
		} else {
			out = append(out, "✍️ "+note)
		}
	}
	if len(out) == 0 {
		return "Нечего было записывать."
	}
	return strings.Join(out, "\n")
}
