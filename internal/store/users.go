package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/murluckk/self-reinvention/internal/model"
)

// ErrLoginTaken — логин дашборда уже принадлежит другому пользователю.
var ErrLoginTaken = errors.New("логин уже занят")

func (s *Store) migrateAccounts() error {
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS users (
			telegram_id INTEGER PRIMARY KEY, name TEXT NOT NULL, fields TEXT NOT NULL,
			finance INTEGER NOT NULL DEFAULT 0, timezone TEXT NOT NULL,
			login TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL,
			share INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS invites (
			token TEXT PRIMARY KEY, preset TEXT NOT NULL, created_by INTEGER NOT NULL,
			expires_at TEXT NOT NULL, used_by INTEGER)`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("миграция пользователей: %w", err)
		}
	}
	return nil
}

// Users возвращает всех пользователей в порядке регистрации.
func (s *Store) Users() ([]*model.User, error) {
	rows, err := s.db.Query(`SELECT telegram_id, name, fields, finance, timezone, login,
		password_hash, share, created_at FROM users ORDER BY created_at, telegram_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.User
	for rows.Next() {
		u := &model.User{}
		var fields, created string
		var finance, share int
		if err := rows.Scan(&u.ID, &u.Name, &fields, &finance, &u.Timezone, &u.Login,
			&u.PasswordHash, &share, &created); err != nil {
			return nil, err
		}
		u.Fields = model.ParseFieldSet(fields)
		u.Finance, u.Share = finance != 0, share != 0
		u.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, u)
	}
	return out, rows.Err()
}

// SaveUser создаёт пользователя или обновляет существующего.
func (s *Store) SaveUser(u *model.User) error {
	if u.CreatedAt.IsZero() {
		u.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.Exec(`INSERT INTO users (telegram_id, name, fields, finance, timezone, login,
			password_hash, share, created_at) VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(telegram_id) DO UPDATE SET name=excluded.name, fields=excluded.fields,
			finance=excluded.finance, timezone=excluded.timezone, login=excluded.login,
			password_hash=excluded.password_hash, share=excluded.share`,
		u.ID, u.Name, u.Fields.String(), boolInt(u.Finance), u.Timezone, u.Login,
		u.PasswordHash, boolInt(u.Share), u.CreatedAt.UTC().Format(time.RFC3339))
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed: users.login") {
		return ErrLoginTaken
	}
	return err
}

// DeleteUser убирает доступ. Записи человека остаются в базе: удалить
// доступ и стереть историю — разные решения.
func (s *Store) DeleteUser(id int64) error {
	_, err := s.db.Exec("DELETE FROM users WHERE telegram_id=?", id)
	return err
}

// CreateInvite сохраняет одноразовое приглашение.
func (s *Store) CreateInvite(token, preset string, createdBy int64, expires time.Time) error {
	_, err := s.db.Exec("INSERT INTO invites (token, preset, created_by, expires_at) VALUES (?,?,?,?)",
		token, preset, createdBy, expires.UTC().Format(time.RFC3339))
	return err
}

// UseInvite атомарно гасит приглашение и возвращает его профиль. ok=false,
// если ссылка неизвестна, просрочена или уже использована.
func (s *Store) UseInvite(token string, userID int64) (preset string, ok bool, err error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec("UPDATE invites SET used_by=? WHERE token=? AND used_by IS NULL AND expires_at>?",
		userID, token, now)
	if err != nil {
		return "", false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "", false, nil
	}
	err = s.db.QueryRow("SELECT preset FROM invites WHERE token=?", token).Scan(&preset)
	return preset, err == nil, err
}

// Counts — сколько записей в основных таблицах; нужно для проверки бэкапа и
// /health.
type Counts struct {
	Users, Days, Notes, Money int
}

func (c Counts) String() string {
	return fmt.Sprintf("пользователей %d, дней %d, заметок %d, денег %d", c.Users, c.Days, c.Notes, c.Money)
}

// Counts считает записи в базе.
func (s *Store) Counts() (Counts, error) { return counts(s.db) }

func counts(db *sql.DB) (Counts, error) {
	var c Counts
	for _, x := range []struct {
		table string
		dst   *int
	}{{"users", &c.Users}, {"days", &c.Days}, {"notes", &c.Notes}, {"money", &c.Money}} {
		if err := db.QueryRow("SELECT COUNT(*) FROM " + x.table).Scan(x.dst); err != nil {
			return c, fmt.Errorf("подсчёт %s: %w", x.table, err)
		}
	}
	return c, nil
}

// VerifyBackup открывает копию отдельно от рабочей базы и проверяет, что она
// читается: integrity_check и подсчёт записей. Бэкап, который не открывается,
// хуже отсутствующего — о нём думают, что он есть.
func VerifyBackup(path string) (Counts, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return Counts{}, err
	}
	defer db.Close()
	var check string
	if err := db.QueryRow("PRAGMA integrity_check").Scan(&check); err != nil {
		return Counts{}, fmt.Errorf("integrity_check: %w", err)
	}
	if check != "ok" {
		return Counts{}, fmt.Errorf("integrity_check: %s", check)
	}
	return counts(db)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
