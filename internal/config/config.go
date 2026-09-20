// Package config читает настройки из окружения. Секретов в репозитории нет,
// всё приходит через env — так удобнее и для systemd, и для docker-compose.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// User — один разрешённый пользователь бота и его доступ к дашборду.
type User struct {
	TelegramID        int64
	Name              string
	Profile           string
	DashboardUser     string
	DashboardPassword string
}

const (
	ProfilePasha = "pasha"
	ProfileSveta = "sveta"
)

// Config — всё, что боту нужно знать о внешнем мире, плюс пороги для флагов
// недельного отчёта.
type Config struct {
	BotToken          string
	OwnerID           int64
	Location          *time.Location
	DataPath          string
	ASRURL            string
	OllamaURL         string
	OllamaModel       string
	DashboardAddr     string
	DashboardUser     string
	DashboardPassword string
	Users             []User
	LLMAPIKey         string
	LLMBaseURL        string
	LLMModel          string
	TranscribeModel   string
	TranscribePrompt  string
	BackupDir         string
	DailyBackup       string

	// Пороги для блока «флаги» в недельной выгрузке.
	MaxWakeSpreadH float64
	MinSavingsRate float64

	// Время напоминаний в часовом поясе Location.
	DailyReminder  string // HH:MM, напоминание закрыть день
	WeeklyReport   string // HH:MM, автоматическая недельная выгрузка
	WeeklyWeekday  time.Weekday
	WeeklyReportN  int    // сколько дней попадает в автоматическую выгрузку
	IncomeReminder string // HH:MM, напоминание 5-го и 20-го
	MonthlyFinance string // HH:MM, отчёт первого числа
}

// Load собирает конфиг из окружения и проверяет обязательные поля.
func Load() (*Config, error) {
	c := &Config{
		BotToken:          os.Getenv("BOT_TOKEN"),
		DataPath:          env("DATA_PATH", "./data/tracker.db"),
		ASRURL:            env("ASR_URL", "http://127.0.0.1:8081/transcribe"),
		OllamaURL:         env("OLLAMA_URL", "http://127.0.0.1:11434"),
		OllamaModel:       env("OLLAMA_MODEL", "qwen2.5:7b-instruct"),
		DashboardAddr:     os.Getenv("DASHBOARD_ADDR"),
		DashboardUser:     env("DASHBOARD_USER", "tracker"),
		DashboardPassword: os.Getenv("DASHBOARD_PASSWORD"),
		LLMAPIKey:         os.Getenv("LLM_API_KEY"),
		LLMBaseURL:        env("LLM_BASE_URL", "https://api.openai.com/v1"),
		LLMModel:          env("LLM_MODEL", "gpt-5.4-mini"),
		TranscribeModel:   env("TRANSCRIBE_MODEL", "gpt-transcribe"),
		TranscribePrompt: env("TRANSCRIBE_PROMPT",
			"Русский личный дневник. Точно записывай числа и время цифрами. "+
				"Термины: подъём, отбой, алгоритмы, системный дизайн, тренировка, "+
				"прогулка, учёба, литература, обучающее видео, полезное занятие, "+
				"сладкое, алкоголь, эмоциональное состояние, траты, аванс, зарплата, накопления."),
		BackupDir:      env("BACKUP_DIR", "./data/backups"),
		DailyBackup:    env("DAILY_BACKUP", "04:00"),
		MaxWakeSpreadH: envFloat("MAX_WAKE_SPREAD_H", 1.5),
		MinSavingsRate: envFloat("MIN_SAVINGS_RATE", 0.55),
		DailyReminder:  env("DAILY_REMINDER", "23:00"),
		WeeklyReport:   env("WEEKLY_REPORT", "12:40"),
		WeeklyWeekday:  time.Sunday,
		WeeklyReportN:  envInt("WEEKLY_REPORT_DAYS", 7),
		IncomeReminder: env("INCOME_REMINDER", "10:00"),
		MonthlyFinance: env("MONTHLY_FINANCE", "10:15"),
	}
	if c.BotToken == "" {
		return nil, fmt.Errorf("BOT_TOKEN не задан")
	}
	owner := os.Getenv("OWNER_ID")
	if owner == "" {
		return nil, fmt.Errorf("OWNER_ID не задан")
	}
	id, err := strconv.ParseInt(owner, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("OWNER_ID должен быть числом: %w", err)
	}
	c.OwnerID = id
	c.Users, err = parseUsers(os.Getenv("TRACKER_USERS"), User{
		TelegramID: id, Name: c.DashboardUser, Profile: ProfilePasha,
		DashboardUser: c.DashboardUser, DashboardPassword: c.DashboardPassword,
	})
	if err != nil {
		return nil, err
	}
	if c.DashboardAddr != "" {
		for _, u := range c.Users {
			if u.DashboardUser == "" || u.DashboardPassword == "" {
				return nil, fmt.Errorf("у пользователя %d не заданы логин/пароль дашборда", u.TelegramID)
			}
		}
	}

	tz := env("TZ", "UTC")
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("не знаю часовой пояс %q: %w", tz, err)
	}
	c.Location = loc
	return c, nil
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envFloat(k string, def float64) float64 {
	if v := os.Getenv(k); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

// Now возвращает текущее время в часовом поясе пользователя.
func (c *Config) Now() time.Time { return time.Now().In(c.Location) }

// Today возвращает сегодняшнюю дату в формате YYYY-MM-DD.
func (c *Config) Today() string { return c.Now().Format("2006-01-02") }

// User возвращает настройки разрешённого Telegram-пользователя.
func (c *Config) User(id int64) (User, bool) {
	for _, u := range c.Users {
		if u.TelegramID == id {
			return u, true
		}
	}
	return User{}, false
}

// parseUsers понимает TRACKER_USERS=id:имя:профиль:login:password,... Старые
// форматы без профиля и имени тоже принимаются.
// Если переменная не задана, сохраняется обратная совместимость с OWNER_ID.
func parseUsers(raw string, fallback User) ([]User, error) {
	if strings.TrimSpace(raw) == "" {
		if fallback.Profile == "" {
			fallback.Profile = ProfilePasha
		}
		return []User{fallback}, nil
	}
	var out []User
	seenIDs := map[int64]bool{}
	seenLogins := map[string]bool{}
	for _, item := range strings.Split(raw, ",") {
		parts := strings.SplitN(strings.TrimSpace(item), ":", 5)
		if len(parts) < 3 || len(parts) > 5 {
			return nil, fmt.Errorf("TRACKER_USERS: ожидалось id:имя:профиль:login:password, получено %q", item)
		}
		id, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("TRACKER_USERS: плохой telegram id %q", parts[0])
		}
		name, profile, login, password := parts[1], "", parts[1], parts[2]
		switch len(parts) {
		case 4:
			name, login, password = parts[1], parts[2], parts[3]
		case 5:
			name, profile, login, password = parts[1], parts[2], parts[3], parts[4]
		}
		name, login = strings.TrimSpace(name), strings.TrimSpace(login)
		profile = strings.ToLower(strings.TrimSpace(profile))
		if profile == "" {
			switch strings.ToLower(name) {
			case "света", "sveta":
				profile = ProfileSveta
			default:
				profile = ProfilePasha
			}
		}
		if profile != ProfilePasha && profile != ProfileSveta {
			return nil, fmt.Errorf("TRACKER_USERS: неизвестный профиль %q для %d", profile, id)
		}
		if name == "" || login == "" || password == "" {
			return nil, fmt.Errorf("TRACKER_USERS: пустое имя, логин или пароль для %d", id)
		}
		if seenIDs[id] || seenLogins[login] {
			return nil, fmt.Errorf("TRACKER_USERS: повтор id или логина для %d", id)
		}
		seenIDs[id], seenLogins[login] = true, true
		out = append(out, User{
			TelegramID: id, Name: name, Profile: profile,
			DashboardUser: login, DashboardPassword: password,
		})
	}
	return out, nil
}
