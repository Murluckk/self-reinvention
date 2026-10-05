package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/murluckk/self-reinvention/internal/auth"
	"github.com/murluckk/self-reinvention/internal/model"
	"github.com/murluckk/self-reinvention/internal/report"
	"github.com/murluckk/self-reinvention/internal/tg"
)

const inviteTTL = 7 * 24 * time.Hour

// tryJoin пускает нового человека по ссылке-приглашению /start inv_<token>.
// Возвращает true, если сообщение было попыткой входа и уже обработано.
func (b *Bot) tryJoin(ctx context.Context, m *tg.Message) bool {
	if m.From == nil {
		return false
	}
	if _, ok := b.users.Get(m.From.ID); ok {
		return false
	}
	cmd, args := splitCommand(strings.TrimSpace(m.Text))
	if !strings.HasPrefix(m.Text, "/") || cmd != "start" || !strings.HasPrefix(args, "inv_") {
		return false
	}
	presetName, ok, err := b.st.UseInvite(strings.TrimPrefix(args, "inv_"), m.From.ID)
	if err != nil {
		b.log.Error("приглашение", "err", err)
		b.reply(ctx, m.Chat.ID, "Что-то сломалось, попробуй позже.")
		return true
	}
	if !ok {
		b.log.Info("недействительное приглашение", "user_id", m.From.ID)
		b.reply(ctx, m.Chat.ID, "Ссылка недействительна: она одноразовая и живёт 7 дней. Попроси новую.")
		return true
	}
	preset, found := model.PresetByName(presetName)
	if !found {
		preset, _ = model.PresetByName("basic")
	}
	name := strings.TrimSpace(m.From.FirstName)
	if name == "" {
		name = m.From.Username
	}
	if name == "" {
		name = "Гость"
	}
	password := auth.Generate()
	hash, err := auth.Hash(password)
	if err != nil {
		b.reply(ctx, m.Chat.ID, "Не смог создать пароль, попробуй позже.")
		return true
	}
	user := &model.User{
		ID: m.From.ID, Name: name, Fields: preset.Fields, Finance: preset.Finance,
		Timezone: preset.Timezone, Login: b.users.FreeLogin(m.From.Username, m.From.ID), PasswordHash: hash,
	}
	if err := b.users.Save(user); err != nil {
		b.log.Error("регистрация по приглашению", "err", err)
		b.reply(ctx, m.Chat.ID, "Не смог тебя зарегистрировать: "+err.Error())
		return true
	}
	b.log.Info("новый пользователь", "user_id", user.ID, "preset", preset.Name)
	b.reply(ctx, m.Chat.ID, welcomeText(user, b.cfg.DailyReminder, b.dashboardAccess(user, password)))
	_ = b.Notify(ctx, b.cfg.OwnerID, fmt.Sprintf("👋 По приглашению зашёл новый участник: %s (@%s, id %d), профиль %s.",
		user.Name, m.From.Username, user.ID, preset.Name))
	return true
}

func welcomeText(u *model.User, reminder, access string) string {
	labels := make([]string, 0, len(u.Fields))
	for _, f := range u.Fields.Fields() {
		labels = append(labels, strings.ToLower(f.Label))
	}
	return fmt.Sprintf(`👋 Привет, %s! Ты в трекере.

Отслеживаю: %s. Поменять — /fields.
Часовой пояс: %s. Поменять — /tz.

Каждый вечер в %s спрошу кнопками, как прошёл день. Можно и самому: /c, /d или голосовым.

%s

/help — все команды.`, u.Name, strings.Join(labels, ", "), u.Timezone, reminder, access)
}

// dashboardAccess печатает адрес дашборда и данные для входа.
func (b *Bot) dashboardAccess(u *model.User, password string) string {
	if b.cfg.DashboardAddr == "" {
		return ""
	}
	url := b.cfg.DashboardURL
	if url == "" {
		url = "(адрес дашборда не настроен)"
	}
	return fmt.Sprintf("Дашборд: %s\nЛогин: %s\nПароль: %s\nСохрани пароль и удали это сообщение. Новый — /password.", url, u.Login, password)
}

// handleFields — /fields: включить или выключить показатели кнопками.
func (b *Bot) handleFields(ctx context.Context, user *model.User, chatID int64) {
	b.send(ctx, chatID, fieldsText(user), fieldsKeyboard(user))
}

func fieldsText(u *model.User) string {
	return fmt.Sprintf("Что отслеживать? Нажимай, чтобы включить или выключить.\nСейчас показателей: %d%s.",
		len(u.Fields), map[bool]string{true: ", финансы включены", false: ""}[u.Finance])
}

func fieldsKeyboard(u *model.User) *tg.InlineKeyboardMarkup {
	kb := &tg.InlineKeyboardMarkup{}
	var row []tg.InlineKeyboardButton
	for _, f := range model.Fields() {
		if f.DB == "note" {
			continue
		}
		mark := "▫️ "
		if u.Fields.Has(f.DB) {
			mark = "✅ "
		}
		row = append(row, tg.Button(mark+f.Label, "f:"+f.DB))
		if len(row) == 2 {
			kb.InlineKeyboard = append(kb.InlineKeyboard, row)
			row = nil
		}
	}
	if len(row) > 0 {
		kb.InlineKeyboard = append(kb.InlineKeyboard, row)
	}
	finance := "▫️ Финансы (/m)"
	if u.Finance {
		finance = "✅ Финансы (/m)"
	}
	kb.InlineKeyboard = append(kb.InlineKeyboard,
		[]tg.InlineKeyboardButton{tg.Button(finance, "f:$finance")},
		[]tg.InlineKeyboardButton{tg.Button("Готово", "f:!")},
	)
	return kb
}

func (b *Bot) handleFieldsCallback(ctx context.Context, user *model.User, cb *tg.CallbackQuery) {
	col := strings.TrimPrefix(cb.Data, "f:")
	if cb.Message == nil {
		_ = b.tg.AnswerCallbackQuery(ctx, cb.ID, "")
		return
	}
	switch col {
	case "!":
		_ = b.tg.AnswerCallbackQuery(ctx, cb.ID, "Сохранено")
		b.edit(ctx, cb.Message.Chat.ID, cb.Message.MessageID, fieldsText(user)+"\n\n/help покажет ключи для /d.", nil)
		return
	case "$finance":
		user.Finance = !user.Finance
	default:
		if _, ok := model.FieldByColumn(col); !ok {
			_ = b.tg.AnswerCallbackQuery(ctx, cb.ID, "")
			return
		}
		user.Fields = user.Fields.Toggle(col)
	}
	if err := b.users.Save(user); err != nil {
		_ = b.tg.AnswerCallbackQuery(ctx, cb.ID, "Не сохранилось: "+err.Error())
		return
	}
	_ = b.tg.AnswerCallbackQuery(ctx, cb.ID, "")
	b.edit(ctx, cb.Message.Chat.ID, cb.Message.MessageID, fieldsText(user), fieldsKeyboard(user))
}

// timezones — частые пояса для кнопок /tz; любой другой можно написать
// текстом в формате IANA или смещением от UTC.
var timezones = []struct{ label, zone string }{
	{"Калининград", "Europe/Kaliningrad"}, {"Москва", "Europe/Moscow"},
	{"Самара", "Europe/Samara"}, {"Екатеринбург", "Asia/Yekaterinburg"},
	{"Омск", "Asia/Omsk"}, {"Новосибирск", "Asia/Novosibirsk"},
	{"Красноярск", "Asia/Krasnoyarsk"}, {"Иркутск", "Asia/Irkutsk"},
	{"Якутск", "Asia/Yakutsk"}, {"Владивосток", "Asia/Vladivostok"},
	{"Магадан", "Asia/Magadan"}, {"Камчатка", "Asia/Kamchatka"},
}

func (b *Bot) handleTimezone(ctx context.Context, user *model.User, chatID int64, args string) {
	if args = strings.TrimSpace(args); args != "" {
		zone, err := parseZone(args)
		if err != nil {
			b.reply(ctx, chatID, "⚠️ "+err.Error())
			return
		}
		b.reply(ctx, chatID, b.setTimezone(user, zone))
		return
	}
	kb := &tg.InlineKeyboardMarkup{}
	for i := 0; i < len(timezones); i += 3 {
		var row []tg.InlineKeyboardButton
		for _, z := range timezones[i:min(i+3, len(timezones))] {
			row = append(row, tg.Button(z.label, "tz:"+z.zone))
		}
		kb.InlineKeyboard = append(kb.InlineKeyboard, row)
	}
	b.send(ctx, chatID, fmt.Sprintf("Сейчас: %s, у тебя %s.\nВыбери город или напиши /tz Europe/Berlin либо /tz +3.",
		user.Timezone, user.Now().Format("15:04")), kb)
}

func (b *Bot) handleTimezoneCallback(ctx context.Context, user *model.User, cb *tg.CallbackQuery) {
	zone := strings.TrimPrefix(cb.Data, "tz:")
	_ = b.tg.AnswerCallbackQuery(ctx, cb.ID, "")
	if cb.Message != nil {
		b.edit(ctx, cb.Message.Chat.ID, cb.Message.MessageID, b.setTimezone(user, zone), nil)
	}
}

func (b *Bot) setTimezone(user *model.User, zone string) string {
	old := user.Timezone
	user.Timezone = zone
	if err := b.users.Save(user); err != nil {
		return "Не смог сменить пояс: " + err.Error()
	}
	return fmt.Sprintf("🕰 Часовой пояс: %s (было %s). У тебя сейчас %s, вечерний опрос придёт в %s по этому времени.",
		zone, old, user.Now().Format("15:04"), b.cfg.DailyReminder)
}

// parseZone понимает IANA-имена и смещения вида +3, UTC+5, GMT-2.
func parseZone(s string) (string, error) {
	s = strings.TrimSpace(s)
	up := strings.ToUpper(s)
	for _, p := range []string{"UTC", "GMT"} {
		up = strings.TrimPrefix(up, p)
	}
	if up != "" && (up[0] == '+' || up[0] == '-') {
		n, err := strconv.Atoi(up)
		if err == nil && n >= -12 && n <= 14 {
			if n == 0 {
				return "UTC", nil
			}
			// В базе tz знаки у Etc/GMT перевёрнуты: UTC+3 — это Etc/GMT-3.
			return fmt.Sprintf("Etc/GMT%+d", -n), nil
		}
	}
	if _, err := time.LoadLocation(s); err != nil || s == "" || strings.EqualFold(s, "local") {
		return "", fmt.Errorf("не знаю пояс %q. Примеры: /tz Europe/Moscow, /tz Asia/Almaty, /tz +3", s)
	}
	return s, nil
}

func (b *Bot) handlePassword(ctx context.Context, user *model.User, chatID int64) {
	if b.cfg.DashboardAddr == "" {
		b.reply(ctx, chatID, "Дашборд на этом сервере выключен.")
		return
	}
	password := auth.Generate()
	hash, err := auth.Hash(password)
	if err != nil {
		b.reply(ctx, chatID, "Не смог создать пароль: "+err.Error())
		return
	}
	user.PasswordHash = hash
	if err := b.users.Save(user); err != nil {
		b.reply(ctx, chatID, "Не смог сохранить пароль: "+err.Error())
		return
	}
	b.reply(ctx, chatID, "🔑 Новый пароль, старый больше не работает.\n\n"+b.dashboardAccess(user, password))
}

func (b *Bot) handleShare(ctx context.Context, user *model.User, chatID int64) {
	user.Share = !user.Share
	if err := b.users.Save(user); err != nil {
		b.reply(ctx, chatID, "Не сохранилось: "+err.Error())
		return
	}
	if user.Share {
		b.reply(ctx, chatID, "🤝 Делишься сериями. Другие участники с включённым /share увидят, закрыт ли твой день и сколько дней подряд ты записываешь. Сами записи, заметки и деньги не видит никто.\n\nДрузья — /friends, выключить — /share.")
		return
	}
	b.reply(ctx, chatID, "🔒 Больше не делишься сериями и не видишь чужие. Включить обратно — /share.")
}

// friendView — то, что видно другу: только факт закрытия дня и серии.
type friendView struct {
	name    string
	closed  bool
	filled  int
	best    string
	bestLen int
}

func (b *Bot) friends(user *model.User) []friendView {
	var out []friendView
	for _, f := range b.users.All() {
		if f.ID == user.ID || !f.Share {
			continue
		}
		today := f.Today()
		days, err := b.st.Days(f.ID, report.AddDays(today, -400), today)
		if err != nil {
			b.log.Error("серии друга", "err", err)
			continue
		}
		v := friendView{name: f.Name}
		if len(days) > 0 && days[len(days)-1].Date == today {
			v.closed = len(days[len(days)-1].MissingIn(f.Fields)) == 0
		}
		st := report.ComputeStreaksIn(days, today, f.Fields)
		v.filled = st.Filled
		for _, field := range f.Fields.Fields() {
			if n := st.Of(field.DB); field.Habit() && n > v.bestLen {
				v.best, v.bestLen = strings.ToLower(field.Label), n
			}
		}
		out = append(out, v)
	}
	return out
}

func (b *Bot) handleFriends(ctx context.Context, user *model.User, chatID int64) {
	if !user.Share {
		b.reply(ctx, chatID, "Серии друзей видны только тем, кто делится своими. Включить — /share.")
		return
	}
	list := b.friends(user)
	if len(list) == 0 {
		b.reply(ctx, chatID, "Пока никто, кроме тебя, не включил /share.")
		return
	}
	var sb strings.Builder
	sb.WriteString("🤝 Друзья\n")
	for _, f := range list {
		state := "⏳ день ещё открыт"
		if f.closed {
			state = "✅ день закрыт"
		}
		fmt.Fprintf(&sb, "\n%s — %s\n  записей подряд: %d дн.", f.name, state, f.filled)
		if f.bestLen > 0 {
			fmt.Fprintf(&sb, ", лучшая серия: %s %d дн.", f.best, f.bestLen)
		}
	}
	b.reply(ctx, chatID, sb.String())
}

// closedFriends — строка для вечернего напоминания: кто уже закрыл день.
func (b *Bot) closedFriends(user *model.User) string {
	if !user.Share {
		return ""
	}
	var names []string
	for _, f := range b.friends(user) {
		if f.closed {
			names = append(names, f.name)
		}
	}
	if len(names) == 0 {
		return ""
	}
	return "🤝 Уже закрыли день: " + strings.Join(names, ", ")
}

// onDayClosed сообщает друзьям, что человек закрыл сегодняшний день. Один раз
// в день: повторные правки дня не должны спамить.
func (b *Bot) onDayClosed(ctx context.Context, user *model.User, date string) {
	if !user.Share || date != user.Today() {
		return
	}
	key := "closed_notice:" + strconv.FormatInt(user.ID, 10)
	if last, err := b.st.JobLastRun(key); err != nil || last == date {
		return
	}
	if err := b.st.SetJobLastRun(key, date); err != nil {
		b.log.Error("отметка закрытия дня", "err", err)
		return
	}
	days, err := b.st.Days(user.ID, report.AddDays(date, -400), date)
	if err != nil {
		return
	}
	filled := report.ComputeStreaksIn(days, date, user.Fields).Filled
	for _, f := range b.users.All() {
		if f.ID == user.ID || !f.Share {
			continue
		}
		if err := b.Notify(ctx, f.ID, fmt.Sprintf("🤝 %s: день закрыт ✅ Записей подряд — %d дн.", user.Name, filled)); err != nil {
			b.log.Warn("уведомление друга", "err", err)
		}
	}
}

// handleAdmin — команды владельца: /invite, /users, /remove, /health.
func (b *Bot) handleAdmin(ctx context.Context, admin *model.User, chatID int64, cmd, args string) {
	switch cmd {
	case "invite":
		presetName := strings.ToLower(strings.TrimSpace(args))
		if presetName == "" {
			presetName = "basic"
		}
		preset, ok := model.PresetByName(presetName)
		if !ok {
			names := make([]string, 0, len(model.Presets))
			for _, p := range model.Presets {
				names = append(names, p.Name+" — "+p.Title)
			}
			b.reply(ctx, chatID, "Не знаю профиль. Есть:\n"+strings.Join(names, "\n"))
			return
		}
		token := auth.Token()
		if err := b.st.CreateInvite(token, preset.Name, admin.ID, time.Now().Add(inviteTTL)); err != nil {
			b.reply(ctx, chatID, "Не смог создать приглашение: "+err.Error())
			return
		}
		link := "/start inv_" + token
		if b.username != "" {
			link = fmt.Sprintf("https://t.me/%s?start=inv_%s", b.username, token)
		}
		b.reply(ctx, chatID, fmt.Sprintf("🎟 Приглашение (одноразовое, 7 дней), профиль %s — %s:\n\n%s\n\nПерешли ссылку человеку: он нажмёт Start и сразу получит доступ и пароль от дашборда. Поля потом настроит в /fields.",
			preset.Name, preset.Title, link))
	case "users":
		var sb strings.Builder
		sb.WriteString("👥 Участники\n")
		for _, u := range b.users.All() {
			flags := ""
			if u.Finance {
				flags += " 💰"
			}
			if u.Share {
				flags += " 🤝"
			}
			fmt.Fprintf(&sb, "\n%s%s — id %d, логин %s\n  %s, показателей: %d", u.Name, flags, u.ID, u.Login, u.Timezone, len(u.Fields))
		}
		sb.WriteString("\n\nЗакрыть доступ: /remove <id>")
		b.reply(ctx, chatID, sb.String())
	case "remove":
		id, err := strconv.ParseInt(strings.TrimSpace(args), 10, 64)
		if err != nil {
			b.reply(ctx, chatID, "⚠️ /remove ждёт telegram id, список — /users")
			return
		}
		if id == admin.ID {
			b.reply(ctx, chatID, "Себя удалить нельзя.")
			return
		}
		u, ok := b.users.Get(id)
		if !ok {
			b.reply(ctx, chatID, "Такого участника нет.")
			return
		}
		if err := b.users.Remove(id); err != nil {
			b.reply(ctx, chatID, "Не смог удалить: "+err.Error())
			return
		}
		b.reply(ctx, chatID, fmt.Sprintf("🚪 %s больше не имеет доступа. Записи остались в базе.", u.Name))
	case "health":
		b.reply(ctx, chatID, b.healthReport())
	}
}
