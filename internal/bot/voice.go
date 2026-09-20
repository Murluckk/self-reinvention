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

func (b *Bot) expirePending() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, p := range b.pending {
		if time.Since(p.created) > pendingTTL {
			delete(b.pending, id)
		}
	}
}

// handleVoice ведёт голосовое по пайплайну: скачать → сконвертировать → ASR →
// разбор → подтверждение. На любом обрыве данные не теряются: сообщение
// оседает заметкой с пометкой, что распознать не удалось.
func (b *Bot) handleVoice(ctx context.Context, userID int64, m *tg.Message, v *tg.Voice) {
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
		edit(b.saveUnrecognized(userID, v, err), nil)
		return
	}

	date := b.cfg.Today()
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
			if res.Recogn == 0 {
				res.Note = text // ничего не поняли — сохраним хотя бы дословно
				res.Level = "none"
			}
		case llmRes.Count() >= res.Count():
			// regex мог зацепить то, что модель пропустила, — оставляем оба слоя
			model.Merge(llmRes.Day, res.Day)
			llmRes.Money = append(llmRes.Money, res.Money...)
			llmRes.Level = "llm"
			if res.Recogn > 0 {
				llmRes.Level = "llm+regex"
			}
			res = llmRes
		}
	}
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
	if b.asr == nil {
		return "", fmt.Errorf("asr не настроен")
	}
	f, err := b.tg.GetFile(ctx, v.FileID)
	if err != nil {
		return "", err
	}
	data, err := b.tg.Download(ctx, f.FilePath)
	if err != nil {
		return "", err
	}
	target := b.asr
	var cloudErr error
	if b.asr.DirectAudio() {
		name := cloudAudioName(f.FilePath)
		if text, err := b.asr.Transcribe(ctx, data, name); err == nil {
			return text, nil
		} else {
			cloudErr = err
			b.log.Warn("облачное распознавание недоступно, пробую локальное", "err", err)
		}
		target = b.asrFallback
		if target == nil {
			return "", cloudErr
		}
	}
	dir, err := os.MkdirTemp("", "voice")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	in := filepath.Join(dir, "in"+ext(f.FilePath))
	out := filepath.Join(dir, "out.wav")
	if err := os.WriteFile(in, data, 0o600); err != nil {
		return "", err
	}
	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(cctx, "ffmpeg", "-y", "-loglevel", "error", "-i", in, "-ar", "16000", "-ac", "1", out)
	if stderr, err := cmd.CombinedOutput(); err != nil {
		if cloudErr != nil {
			return "", fmt.Errorf("облачный ASR: %v; локальный ffmpeg: %v: %s", cloudErr, err, strings.TrimSpace(string(stderr)))
		}
		return "", fmt.Errorf("ffmpeg: %v: %s", err, strings.TrimSpace(string(stderr)))
	}
	wav, err := os.ReadFile(out)
	if err != nil {
		return "", err
	}
	text, err := target.Transcribe(ctx, wav, "voice.wav")
	if err != nil && cloudErr != nil {
		return "", fmt.Errorf("облачный ASR: %v; локальный ASR: %w", cloudErr, err)
	}
	return text, err
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
func (b *Bot) saveUnrecognized(userID int64, v *tg.Voice, cause error) string {
	n := &model.Note{
		TS:   b.cfg.Now(),
		Date: b.cfg.Today(),
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
		{tg.Button("📝 Показать текст", "v:raw:"+id)},
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

func (b *Bot) handleCallback(ctx context.Context, userID int64, cb *tg.CallbackQuery) {
	parts := strings.SplitN(cb.Data, ":", 3)
	if len(parts) != 3 || parts[0] != "v" {
		_ = b.tg.AnswerCallbackQuery(ctx, cb.ID, "")
		return
	}
	action, id := parts[1], parts[2]
	p := b.getPending(id)
	if p == nil || p.userID != userID {
		_ = b.tg.AnswerCallbackQuery(ctx, cb.ID, "Эта карточка уже неактуальна")
		if cb.Message != nil {
			_ = b.tg.EditMessageText(ctx, cb.Message.Chat.ID, cb.Message.MessageID,
				"⌛️ Карточка устарела — наговори заново.", nil)
		}
		return
	}
	chatID, msgID := userID, int64(0)
	if cb.Message != nil {
		chatID, msgID = cb.Message.Chat.ID, cb.Message.MessageID
	}
	switch action {
	case "raw":
		_ = b.tg.AnswerCallbackQuery(ctx, cb.ID, "")
		_ = b.tg.EditMessageText(ctx, chatID, msgID, confirmationText(p.voice, true, p.raw), confirmKeyboard(id))
	case "no":
		b.dropPending(id)
		_ = b.tg.AnswerCallbackQuery(ctx, cb.ID, "Отменено")
		_ = b.tg.EditMessageText(ctx, chatID, msgID, "✖️ Отменил, ничего не записал.\n\nРасшифровка:\n"+p.raw, nil)
	case "ok":
		b.dropPending(id)
		_ = b.tg.AnswerCallbackQuery(ctx, cb.ID, "Записано")
		_ = b.tg.EditMessageText(ctx, chatID, msgID, b.applyVoice(p), nil)
	default:
		_ = b.tg.AnswerCallbackQuery(ctx, cb.ID, "")
	}
}

// applyVoice пишет подтверждённое в базу и возвращает текст для чата.
func (b *Bot) applyVoice(p *pending) string {
	v := p.voice
	var out []string
	if v.Day != nil && !v.Day.Empty() {
		if err := b.st.UpsertDay(p.userID, v.Day); err != nil {
			b.log.Error("запись дня из голосового", "err", err)
			out = append(out, "⚠️ запись дня не сохранилась: "+err.Error())
		} else {
			out = append(out, b.dayConfirmation(p.userID, v.Day.Date, v.Day))
		}
	}
	for _, m := range v.Money {
		m.TS = b.cfg.Now()
		if err := b.st.AddMoney(p.userID, m); err != nil {
			b.log.Error("запись денег из голосового", "err", err)
			out = append(out, "⚠️ деньги не сохранились: "+err.Error())
			continue
		}
		out = append(out, "💰 "+parse.FormatMoney(m))
	}
	if note := strings.TrimSpace(v.Note); note != "" {
		n := &model.Note{TS: b.cfg.Now(), Date: b.cfg.Today(), Text: note}
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
