package bot

import (
	"context"
	"strings"
	"time"

	"github.com/murluckk/self-reinvention/internal/model"
	"github.com/murluckk/self-reinvention/internal/report"
)

// handleInsights — /i: связи между показателями за последние 90 дней.
func (b *Bot) handleInsights(ctx context.Context, user *model.User, chatID int64) {
	to := user.Today()
	st, err := b.stats(user, report.AddDays(to, -89), to)
	if err != nil {
		b.reply(ctx, chatID, "Не смог собрать данные: "+err.Error())
		return
	}
	if len(st.Insights) == 0 {
		b.reply(ctx, chatID, "🔎 Пока выводов нет: нужно хотя бы по 3 дня с разными значениями показателя и оценкой состояния. Продолжай записывать — связи появятся сами.")
		return
	}
	var sb strings.Builder
	sb.WriteString("🔎 Что заметил за 90 дней\n\n")
	for _, in := range st.Insights {
		sb.WriteString("• " + in.Text + "\n")
	}
	sb.WriteString("\nЭто связи, а не причины: повод присмотреться и проверить на себе.")
	b.reply(ctx, chatID, sb.String())
}

// handleReview — /review: AI-разбор последней недели по запросу.
func (b *Bot) handleReview(ctx context.Context, user *model.User, chatID int64) {
	if b.llm == nil {
		b.reply(ctx, chatID, "LLM не настроена на сервере.")
		return
	}
	to := user.Today()
	st, err := b.stats(user, report.AddDays(to, -6), to)
	if err != nil {
		b.reply(ctx, chatID, "Не смог собрать данные: "+err.Error())
		return
	}
	if st.FilledDays == 0 {
		b.reply(ctx, chatID, "За неделю нет записей — разбирать нечего.")
		return
	}
	status := b.send(ctx, chatID, "🤖 Думаю над неделей…", nil)
	text := b.review(ctx, st)
	if text == "" {
		text = "Не получилось собрать разбор: LLM недоступна. Цифры — в /w."
	}
	if status != nil {
		b.edit(ctx, chatID, status.MessageID, text, nil)
		return
	}
	b.reply(ctx, chatID, text)
}

const reviewPrompt = `Ты внимательный коуч по личным привычкам. Тебе дают недельную выгрузку трекера.
Пиши по-русски, на «ты», коротко, без воды и морализаторства. Опирайся только на цифры из выгрузки, ничего не выдумывай.
Структура ответа, обычным текстом без markdown-разметки:
Что получилось — 2–3 пункта с конкретными цифрами.
Что просело — 1–2 пункта; если всё ровно, так и скажи.
На следующую неделю — 1–2 конкретных маленьких шага, привязанных к данным.
Не больше 900 символов.`

// review просит LLM разобрать неделю. Заметки в модель не отправляются:
// для разбора хватает цифр, а дневниковые записи остаются на сервере.
// Пустая строка — разбора не будет, причина уже в логе и алерте.
func (b *Bot) review(ctx context.Context, st *report.Stats) string {
	if b.llm == nil || st.FilledDays == 0 {
		return ""
	}
	md := report.Markdown(st)
	if i := strings.Index(md, "\n## Заметки"); i >= 0 {
		md = md[:i]
	}
	rctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	text, err := b.llm.Complete(rctx, reviewPrompt, md)
	if err != nil {
		b.log.Warn("AI-разбор недели", "err", err)
		b.alert(ctx, "llm-review", "AI-разбор недели не собрался: "+err.Error())
		return ""
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	return "🤖 Разбор недели\n\n" + text
}
