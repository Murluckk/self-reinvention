package bot

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// alertCooldown — не чаще одного сообщения в час по одной причине: упавший
// OpenAI не должен превращаться в сотню уведомлений за вечер.
const alertCooldown = time.Hour

// health — журнал последних сбоев для /health и антиспам для алертов.
type health struct {
	mu     sync.Mutex
	recent []string
	sent   map[string]time.Time
}

func newHealth() *health { return &health{sent: map[string]time.Time{}} }

func (h *health) record(text string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.recent = append(h.recent, time.Now().Format("02.01 15:04")+" "+text)
	if len(h.recent) > 10 {
		h.recent = h.recent[len(h.recent)-10:]
	}
}

func (h *health) shouldSend(key string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if last, ok := h.sent[key]; ok && time.Since(last) < alertCooldown {
		return false
	}
	h.sent[key] = time.Now()
	return true
}

// alert записывает сбой и сообщает о нём владельцу в Telegram.
func (b *Bot) alert(ctx context.Context, key, text string) {
	b.health.record(text)
	if !b.health.shouldSend(key) {
		return
	}
	if _, err := b.tg.SendMessage(ctx, b.cfg.OwnerID, "🚨 "+text, nil); err != nil {
		b.log.Error("алерт не отправлен", "key", key, "err", err)
	}
}

// Alert — то же для внешних компонентов вроде дашборда.
func (b *Bot) Alert(ctx context.Context, key, text string) { b.alert(ctx, key, text) }

// JobFailed — обработчик ошибок планировщика.
func (b *Bot) JobFailed(ctx context.Context, job string, err error) {
	b.alert(ctx, "job:"+job, fmt.Sprintf("Задача %s не отработала: %v. Повторю на следующем тике.", job, err))
}

func (b *Bot) healthReport() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "🩺 Состояние\n\nВерсия: %s\nРаботает: %s\nУчастников: %d\n",
		b.Version, time.Since(b.started).Round(time.Minute), len(b.users.All()))
	if c, err := b.st.Counts(); err == nil {
		fmt.Fprintf(&sb, "База: %s\n", c)
	}
	if info, err := os.Stat(b.cfg.DataPath); err == nil {
		fmt.Fprintf(&sb, "Размер базы: %.1f МБ\n", float64(info.Size())/1024/1024)
	}
	if last, err := b.st.JobLastRun("daily_backup"); err == nil {
		if last == "" {
			last = "ещё не было"
		}
		fmt.Fprintf(&sb, "Последний бэкап: %s\n", last)
	}
	asrMode := "локальный Whisper"
	if b.asr != nil && b.asr.DirectAudio() {
		asrMode = "облако, фолбэк — локальный Whisper"
	}
	fmt.Fprintf(&sb, "Распознавание: %s\n", asrMode)
	b.health.mu.Lock()
	recent := append([]string(nil), b.health.recent...)
	b.health.mu.Unlock()
	if len(recent) == 0 {
		sb.WriteString("\nСбоев с момента запуска не было.")
	} else {
		sb.WriteString("\nПоследние сбои:\n")
		for i := len(recent) - 1; i >= 0; i-- {
			sb.WriteString("• " + recent[i] + "\n")
		}
	}
	return sb.String()
}
