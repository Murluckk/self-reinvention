package bot

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/murluckk/self-reinvention/internal/config"
	"github.com/murluckk/self-reinvention/internal/model"
	"github.com/murluckk/self-reinvention/internal/parse"
	"github.com/murluckk/self-reinvention/internal/report"
	"github.com/murluckk/self-reinvention/internal/store"
	"github.com/murluckk/self-reinvention/internal/tg"
	"github.com/murluckk/self-reinvention/internal/users"
)

// fakeTelegram записывает всё, что бот отправил, и отвечает как Bot API.
type fakeTelegram struct {
	mu    sync.Mutex
	calls []apiCall
	msgID int64
}

type apiCall struct {
	method  string
	chatID  int64
	text    string
	buttons []string // callback_data всех кнопок
}

func (f *fakeTelegram) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		ChatID      int64                    `json:"chat_id"`
		Text        string                   `json:"text"`
		ReplyMarkup *tg.InlineKeyboardMarkup `json:"reply_markup"`
	}
	body, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(body, &payload)
	method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	call := apiCall{method: method, chatID: payload.ChatID, text: payload.Text}
	if payload.ReplyMarkup != nil {
		for _, row := range payload.ReplyMarkup.InlineKeyboard {
			for _, b := range row {
				call.buttons = append(call.buttons, b.CallbackData)
			}
		}
	}
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.msgID++
	id := f.msgID
	f.mu.Unlock()
	result := map[string]any{"message_id": id, "chat": map[string]any{"id": payload.ChatID}}
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
}

// last возвращает последний вызов с текстом в указанный чат.
func (f *fakeTelegram) last(t *testing.T, chatID int64) apiCall {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.calls) - 1; i >= 0; i-- {
		c := f.calls[i]
		if c.chatID == chatID && c.text != "" {
			return c
		}
	}
	t.Fatalf("в чат %d ничего не отправлено", chatID)
	return apiCall{}
}

func (f *fakeTelegram) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

const ownerID, guestID, strangerID = 100, 200, 999

type harness struct {
	t   *testing.T
	b   *Bot
	tg  *fakeTelegram
	st  *store.Store
	reg *users.Registry
	ctx context.Context
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	fake := &fakeTelegram{}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	reg, err := users.Load(st)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Import([]users.Seed{{ID: ownerID, Name: "Паша", Preset: "pasha", Timezone: "UTC", Login: "owner", Password: "x"}}, ownerID); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{OwnerID: ownerID, Location: time.UTC, DailyReminder: "22:00",
		DashboardAddr: "127.0.0.1:0", DashboardURL: "https://dash.example", WeeklyReportN: 7}
	b := New(cfg, tg.NewWithBase("T", srv.URL), st, reg, nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return &harness{t: t, b: b, tg: fake, st: st, reg: reg, ctx: context.Background()}
}

func (h *harness) say(from int64, text string) apiCall {
	h.t.Helper()
	h.b.handleUpdate(h.ctx, &tg.Update{Message: &tg.Message{
		MessageID: 1, From: &tg.User{ID: from, FirstName: "Тимур", Username: "timur_k"},
		Chat: tg.Chat{ID: from}, Text: text,
	}})
	return h.tg.last(h.t, from)
}

func (h *harness) press(from int64, data string, msgID int64) apiCall {
	h.t.Helper()
	h.b.handleUpdate(h.ctx, &tg.Update{CallbackQuery: &tg.CallbackQuery{
		ID: "cb", From: &tg.User{ID: from}, Data: data,
		Message: &tg.Message{MessageID: msgID, Chat: tg.Chat{ID: from}},
	}})
	return h.tg.last(h.t, from)
}

var inviteRe = regexp.MustCompile(`inv_[A-Za-z0-9_-]+`)

func TestInviteCheckinAndFriendsFlow(t *testing.T) {
	h := newHarness(t)

	// Посторонним бот не отвечает вовсе.
	before := h.tg.count()
	h.b.handleUpdate(h.ctx, &tg.Update{Message: &tg.Message{From: &tg.User{ID: strangerID}, Chat: tg.Chat{ID: strangerID}, Text: "/help"}})
	if h.tg.count() != before {
		t.Fatal("бот ответил постороннему")
	}

	// Владелец приглашает, гость заходит по ссылке.
	invite := h.say(ownerID, "/invite")
	token := inviteRe.FindString(invite.text)
	if token == "" {
		t.Fatalf("нет ссылки в приглашении: %s", invite.text)
	}
	welcome := h.say(guestID, "/start "+token)
	for _, want := range []string{"Привет, Тимур", "Логин: timur_k", "Пароль: ", "https://dash.example"} {
		if !strings.Contains(welcome.text, want) {
			t.Fatalf("в приветствии нет %q:\n%s", want, welcome.text)
		}
	}
	guest, ok := h.reg.Get(guestID)
	if !ok || guest.Finance || guest.Fields.Has("algorithms") {
		t.Fatalf("гость создан неверно: %+v", guest)
	}
	if !strings.Contains(h.tg.last(t, ownerID).text, "Тимур") {
		t.Fatal("владельца не предупредили о новом участнике")
	}
	// Вторая попытка по той же ссылке не проходит.
	if again := h.say(strangerID, "/start "+token); !strings.Contains(again.text, "недействительна") {
		t.Fatalf("ссылка сработала дважды: %s", again.text)
	}

	// Гость включает прогулку в /fields.
	fields := h.say(guestID, "/fields")
	if !contains(fields.buttons, "f:walk") {
		t.Fatalf("в /fields нет прогулки: %v", fields.buttons)
	}
	h.press(guestID, "f:walk", 50)
	if g, _ := h.reg.Get(guestID); !g.Fields.Has("walk") {
		t.Fatal("прогулка не включилась")
	}

	// Оба делятся сериями.
	h.say(ownerID, "/share")
	h.say(guestID, "/share")

	// Вечерний опрос: подъём текстом, остальное кнопками.
	today := guest.Today()
	q := h.say(guestID, "/c")
	if !strings.Contains(q.text, "Подъём") || !contains(q.buttons, "c:"+today+":wake:07:00") {
		t.Fatalf("первый вопрос не про подъём: %s %v", q.text, q.buttons)
	}
	q = h.say(guestID, "7:45")
	if !strings.Contains(q.text, "✓ Подъём: 07:45") || !strings.Contains(q.text, "Заснул") {
		t.Fatalf("текстовый ответ не принят: %s", q.text)
	}
	msg := h.tg.msgID
	q = h.press(guestID, "c:"+today+":bed:23:30", msg)
	q = h.press(guestID, "c:"+today+":workout:-", msg) // пропустить
	if !strings.Contains(q.text, "Прогулка") {
		t.Fatalf("после пропуска ждали прогулку: %s", q.text)
	}
	q = h.press(guestID, "c:"+today+":walk:1", msg)
	q = h.press(guestID, "c:"+today+":mood:8", msg)
	if !strings.Contains(q.text, "Пропущенное можно дописать") {
		t.Fatalf("опрос не закончился после пропуска: %s", q.text)
	}
	day, _ := h.st.GetDay(guestID, today)
	if day.Wake == nil || *day.Wake != "07:45" || day.Bed == nil || day.Walk == nil || !*day.Walk ||
		day.Mood == nil || *day.Mood != 8 || day.Workout != nil {
		t.Fatalf("в базу записалось не то: %+v", day)
	}

	// Добиваем тренировку через /c — день закрыт, друг получает уведомление.
	q = h.say(guestID, "/c")
	q = h.press(guestID, "c:"+today+":workout:1", h.tg.msgID)
	if !strings.Contains(q.text, "День заполнен целиком") {
		t.Fatalf("день не закрылся: %s", q.text)
	}
	if note := h.tg.last(t, ownerID); !strings.Contains(note.text, "Тимур: день закрыт") {
		t.Fatalf("владелец не узнал, что друг закрыл день: %s", note.text)
	}
	friends := h.say(ownerID, "/friends")
	if !strings.Contains(friends.text, "Тимур — ✅ день закрыт") {
		t.Fatalf("/friends: %s", friends.text)
	}
	// Данные друга недоступны: в /friends нет ни времени, ни оценок.
	if strings.Contains(friends.text, "07:45") || strings.Contains(friends.text, "8/10") {
		t.Fatalf("в /friends утекли записи: %s", friends.text)
	}
}

func TestReminderStartsCheckinAndAdminOnlyCommands(t *testing.T) {
	h := newHarness(t)
	if err := h.b.RemindDay(h.ctx, ownerID); err != nil {
		t.Fatal(err)
	}
	q := h.tg.last(t, ownerID)
	if !strings.Contains(q.text, "Пора подвести итоги") || len(q.buttons) == 0 {
		t.Fatalf("напоминание без кнопок: %s", q.text)
	}

	token := inviteRe.FindString(h.say(ownerID, "/invite sveta").text)
	h.say(guestID, "/start "+token)
	if g, _ := h.reg.Get(guestID); !g.Fields.Has("sweet") {
		t.Fatalf("профиль sveta из приглашения не применился: %+v", g)
	}
	if reply := h.say(guestID, "/users"); strings.Contains(reply.text, "Участники") {
		t.Fatal("гостю доступна админская команда")
	}
	if reply := h.say(ownerID, "/remove 200"); !strings.Contains(reply.text, "больше не имеет доступа") {
		t.Fatalf("/remove: %s", reply.text)
	}
	before := h.tg.count()
	h.b.handleUpdate(h.ctx, &tg.Update{Message: &tg.Message{From: &tg.User{ID: guestID}, Chat: tg.Chat{ID: guestID}, Text: "/s"}})
	if h.tg.count() != before {
		t.Fatal("удалённый участник всё ещё получает ответы")
	}
}

func TestVoiceCardFieldEdit(t *testing.T) {
	h := newHarness(t)
	owner, _ := h.reg.Get(ownerID)
	mood := 5
	id := h.b.putPending(&pending{userID: ownerID, created: time.Now(), raw: "встал в семь",
		voice: voiceWith(&model.Day{Date: owner.Today(), Mood: &mood})})

	picker := h.press(ownerID, "v:ed:"+id, 70)
	if !contains(picker.buttons, "v:pick:"+id+":mood") {
		t.Fatalf("нет выбора поля: %v", picker.buttons)
	}
	question := h.press(ownerID, "v:pick:"+id+":wake", 70)
	if !strings.Contains(question.text, "Подъём") {
		t.Fatalf("вопрос не про подъём: %s", question.text)
	}
	card := h.say(ownerID, "6:50") // значение текстом
	if !strings.Contains(card.text, "Подъём: 06:50") || !contains(card.buttons, "v:ok:"+id) {
		t.Fatalf("карточка не обновилась: %s", card.text)
	}
	h.press(ownerID, "v:set:"+id+":mood:9", h.tg.msgID)
	h.press(ownerID, "v:ok:"+id, h.tg.msgID)
	day, _ := h.st.GetDay(ownerID, owner.Today())
	if day.Wake == nil || *day.Wake != "06:50" || day.Mood == nil || *day.Mood != 9 {
		t.Fatalf("правки не записались: %+v", day)
	}
	// Заметок не появилось: текст «6:50» ушёл в поле, а не в дневник.
	if notes, _ := h.st.Notes(ownerID, report.AddDays(owner.Today(), -1), owner.Today()); len(notes) != 0 {
		t.Fatalf("ответ на вопрос сохранился заметкой: %+v", notes[0])
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func voiceWith(d *model.Day) *parse.Voice { return &parse.Voice{Day: d, Level: "llm"} }

func TestCheckinKeepsNonAnswerAsNote(t *testing.T) {
	h := newHarness(t)
	h.say(ownerID, "/c")
	reply := h.say(ownerID, "надо купить кроссовки")
	if !strings.Contains(reply.text, "сохранил как заметку") {
		t.Fatalf("текст не сохранён: %s", reply.text)
	}
	owner, _ := h.reg.Get(ownerID)
	notes, _ := h.st.Notes(ownerID, owner.Today(), owner.Today())
	if len(notes) != 1 || notes[0].Text != "надо купить кроссовки" {
		t.Fatalf("заметка: %+v", notes)
	}
	if day, _ := h.st.GetDay(ownerID, owner.Today()); !day.EmptyIn(owner.Fields) {
		t.Fatalf("в день попал мусор: %+v", day)
	}
}
