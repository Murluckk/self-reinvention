// Package store — хранилище на SQLite (modernc.org/sqlite, чистый Go, без cgo).
//
// Почему SQLite, а не JSON-файл: почти каждая операция здесь — это выборка по
// диапазону дат, upsert одной колонки в существующей строке или агрегат за 30
// дней. На JSON-файле всё это превращается в «прочитать целиком, изменить в
// памяти, записать целиком», а стрики и недельные срезы приходится считать
// руками. SQLite даёт диапазоны, ON CONFLICT DO UPDATE и атомарность из
// коробки, стоит одну зависимость без cgo и остаётся одним файлом, который
// можно скопировать с VPS как бэкап.
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/murluckk/self-reinvention/internal/model"

	_ "modernc.org/sqlite"
)

// Store — соединение с базой.
type Store struct {
	db *sql.DB
}

// Open открывает базу и приводит схему в актуальное состояние.
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("каталог для базы: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	// Пишет один пользователь, конкуррентности нет — один коннект избавляет от
	// database is locked.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close закрывает соединение.
func (s *Store) Close() error { return s.db.Close() }

func sqlType(k model.Kind) string {
	switch k {
	case model.KindFloat:
		return "REAL"
	case model.KindInt, model.KindScale, model.KindBool:
		return "INTEGER"
	default:
		return "TEXT"
	}
}

// migrate создаёт таблицы и добавляет колонки под поля, которых ещё нет в базе.
// Схема таблицы дней выводится из структуры model.Day, поэтому новое поле в
// структуре само приезжает в базу при следующем запуске.
func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS days (date TEXT PRIMARY KEY, updated_at TEXT)`,
		`CREATE TABLE IF NOT EXISTS notes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts TEXT NOT NULL, date TEXT NOT NULL, tag TEXT NOT NULL DEFAULT '', text TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS money (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts TEXT NOT NULL, date TEXT NOT NULL, kind TEXT NOT NULL,
			amount REAL NOT NULL, currency TEXT NOT NULL DEFAULT 'RUB',
			category TEXT NOT NULL DEFAULT '', comment TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS voice (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts TEXT NOT NULL, date TEXT NOT NULL, raw TEXT NOT NULL,
			parsed TEXT NOT NULL DEFAULT '', level TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS jobs (name TEXT PRIMARY KEY, last_run TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			name TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS notes_date ON notes(date)`,
		`CREATE INDEX IF NOT EXISTS money_date ON money(date)`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("миграция: %w", err)
		}
	}
	have, err := s.columns("days")
	if err != nil {
		return err
	}
	for _, f := range model.Fields() {
		if have[f.DB] {
			continue
		}
		q := fmt.Sprintf("ALTER TABLE days ADD COLUMN %s %s", f.DB, sqlType(f.Kind))
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("добавление колонки %s: %w", f.DB, err)
		}
	}
	return s.migrateRatingsToTen()
}

// migrateRatingsToTen один раз переводит исторические оценки 1..5 в 2..10.
// Маркер нужен обязательно: без него каждый рестарт повторно умножал бы данные.
func (s *Store) migrateRatingsToTen() error {
	const name = "ratings_1_to_10"
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var applied int
	if err := tx.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE name=?", name).Scan(&applied); err != nil {
		return err
	}
	if applied > 0 {
		return tx.Commit()
	}
	if _, err := tx.Exec(`
		UPDATE days SET
			focus = CASE WHEN focus BETWEEN 1 AND 5 THEN focus * 2 ELSE focus END,
			mood = CASE WHEN mood BETWEEN 1 AND 5 THEN mood * 2 ELSE mood END,
			energy = CASE WHEN energy BETWEEN 1 AND 5 THEN energy * 2 ELSE energy END`); err != nil {
		return fmt.Errorf("миграция оценок на шкалу 1-10: %w", err)
	}
	if _, err := tx.Exec(
		"INSERT INTO schema_migrations (name, applied_at) VALUES (?,?)",
		name, time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) columns(table string) (map[string]bool, error) {
	rows, err := s.db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}

// UpsertDay записывает заполненные поля d в запись за d.Date, не трогая те,
// что в d равны nil. Это и есть дописывание задним числом.
func (s *Store) UpsertDay(d *model.Day) error {
	if d.Date == "" {
		return fmt.Errorf("не указана дата записи")
	}
	set := d.SetFields()
	cols := []string{"date", "updated_at"}
	vals := []any{d.Date, time.Now().UTC().Format(time.RFC3339)}
	for _, f := range set {
		cols = append(cols, f.DB)
		vals = append(vals, dbValue(&f, d))
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(cols)), ",")
	upd := []string{"updated_at=excluded.updated_at"}
	for _, f := range set {
		upd = append(upd, f.DB+"=excluded."+f.DB)
	}
	q := fmt.Sprintf(
		"INSERT INTO days (%s) VALUES (%s) ON CONFLICT(date) DO UPDATE SET %s",
		strings.Join(cols, ","), ph, strings.Join(upd, ","))
	_, err := s.db.Exec(q, vals...)
	if err != nil {
		return fmt.Errorf("запись дня: %w", err)
	}
	return nil
}

func dbValue(f *model.Field, d *model.Day) any {
	switch v := f.Get(d).(type) {
	case *float64:
		return *v
	case *int:
		return *v
	case *bool:
		if *v {
			return 1
		}
		return 0
	case *string:
		return *v
	}
	return nil
}

func dayColumns() (string, []model.Field) {
	fs := model.Fields()
	cols := make([]string, 0, len(fs)+1)
	cols = append(cols, "date")
	for _, f := range fs {
		cols = append(cols, f.DB)
	}
	return strings.Join(cols, ","), fs
}

func scanDay(rows *sql.Rows, fs []model.Field) (*model.Day, error) {
	d := &model.Day{}
	dest := make([]any, 0, len(fs)+1)
	dest = append(dest, &d.Date)
	holders := make([]any, len(fs))
	for i, f := range fs {
		switch f.Kind {
		case model.KindFloat:
			h := &sql.NullFloat64{}
			holders[i], dest = h, append(dest, h)
		case model.KindInt, model.KindScale, model.KindBool:
			h := &sql.NullInt64{}
			holders[i], dest = h, append(dest, h)
		default:
			h := &sql.NullString{}
			holders[i], dest = h, append(dest, h)
		}
	}
	if err := rows.Scan(dest...); err != nil {
		return nil, err
	}
	for i := range fs {
		f := fs[i]
		switch h := holders[i].(type) {
		case *sql.NullFloat64:
			if h.Valid {
				if err := f.SetAny(d, h.Float64); err != nil {
					return nil, err
				}
			}
		case *sql.NullInt64:
			if h.Valid {
				var v any = int(h.Int64)
				if f.Kind == model.KindBool {
					v = h.Int64 != 0
				}
				if err := f.SetAny(d, v); err != nil {
					return nil, err
				}
			}
		case *sql.NullString:
			if h.Valid && h.String != "" {
				if err := f.SetAny(d, h.String); err != nil {
					return nil, err
				}
			}
		}
	}
	return d, nil
}

// GetDay возвращает запись за дату. Если записи нет, возвращается пустая
// запись с проставленной датой — вызывающему не нужно различать эти случаи.
func (s *Store) GetDay(date string) (*model.Day, error) {
	cols, fs := dayColumns()
	rows, err := s.db.Query("SELECT "+cols+" FROM days WHERE date=?", date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if rows.Next() {
		return scanDay(rows, fs)
	}
	return &model.Day{Date: date}, rows.Err()
}

// Days возвращает записи за период [from, to] включительно, по возрастанию даты.
func (s *Store) Days(from, to string) ([]*model.Day, error) {
	cols, fs := dayColumns()
	rows, err := s.db.Query("SELECT "+cols+" FROM days WHERE date>=? AND date<=? ORDER BY date", from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Day
	for rows.Next() {
		d, err := scanDay(rows, fs)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// AddNote сохраняет заметку и возвращает её с проставленным id.
func (s *Store) AddNote(n *model.Note) error {
	res, err := s.db.Exec("INSERT INTO notes (ts, date, tag, text) VALUES (?,?,?,?)",
		n.TS.UTC().Format(time.RFC3339), n.Date, n.Tag, n.Text)
	if err != nil {
		return fmt.Errorf("запись заметки: %w", err)
	}
	n.ID, _ = res.LastInsertId()
	return nil
}

// Notes возвращает заметки за период по возрастанию времени.
func (s *Store) Notes(from, to string) ([]*model.Note, error) {
	rows, err := s.db.Query("SELECT id, ts, date, tag, text FROM notes WHERE date>=? AND date<=? ORDER BY ts, id", from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Note
	for rows.Next() {
		n := &model.Note{}
		var ts string
		if err := rows.Scan(&n.ID, &ts, &n.Date, &n.Tag, &n.Text); err != nil {
			return nil, err
		}
		n.TS, _ = time.Parse(time.RFC3339, ts)
		out = append(out, n)
	}
	return out, rows.Err()
}

// AddMoney сохраняет денежную запись.
func (s *Store) AddMoney(m *model.Money) error {
	res, err := s.db.Exec("INSERT INTO money (ts, date, kind, amount, currency, category, comment) VALUES (?,?,?,?,?,?,?)",
		m.TS.UTC().Format(time.RFC3339), m.Date, string(m.Kind), m.Amount, m.Currency, m.Category, m.Comment)
	if err != nil {
		return fmt.Errorf("запись денег: %w", err)
	}
	m.ID, _ = res.LastInsertId()
	return nil
}

// Money возвращает денежные записи за период.
func (s *Store) Money(from, to string) ([]*model.Money, error) {
	return s.queryMoney("SELECT id, ts, date, kind, amount, currency, category, comment FROM money WHERE date>=? AND date<=? ORDER BY ts, id", from, to)
}

// MoneyUntil возвращает все денежные записи по дату включительно — нужно для
// накопительного итога капитала.
func (s *Store) MoneyUntil(to string) ([]*model.Money, error) {
	return s.queryMoney("SELECT id, ts, date, kind, amount, currency, category, comment FROM money WHERE date<=? ORDER BY ts, id", to)
}

func (s *Store) queryMoney(q string, args ...any) ([]*model.Money, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Money
	for rows.Next() {
		m := &model.Money{}
		var ts, kind string
		if err := rows.Scan(&m.ID, &ts, &m.Date, &kind, &m.Amount, &m.Currency, &m.Category, &m.Comment); err != nil {
			return nil, err
		}
		m.TS, _ = time.Parse(time.RFC3339, ts)
		m.Kind = model.MoneyKind(kind)
		out = append(out, m)
	}
	return out, rows.Err()
}

// Undoable описывает последнюю запись, которую можно отменить.
type Undoable struct {
	Kind  string // "note" или "money"
	ID    int64
	Descr string
}

// LastUndoable возвращает самую свежую из последней заметки и последней
// денежной записи.
func (s *Store) LastUndoable() (*Undoable, error) {
	var (
		noteID, moneyID   sql.NullInt64
		noteTS, moneyTS   sql.NullString
		noteText          sql.NullString
		mKind, mCur, mCat sql.NullString
		mAmount           sql.NullFloat64
	)
	row := s.db.QueryRow("SELECT id, ts, text FROM notes ORDER BY ts DESC, id DESC LIMIT 1")
	if err := row.Scan(&noteID, &noteTS, &noteText); err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	row = s.db.QueryRow("SELECT id, ts, kind, amount, currency, category FROM money ORDER BY ts DESC, id DESC LIMIT 1")
	if err := row.Scan(&moneyID, &moneyTS, &mKind, &mAmount, &mCur, &mCat); err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	switch {
	case !noteID.Valid && !moneyID.Valid:
		return nil, nil
	case moneyID.Valid && (!noteID.Valid || moneyTS.String >= noteTS.String):
		k := model.MoneyKind(mKind.String)
		return &Undoable{Kind: "money", ID: moneyID.Int64,
			Descr: fmt.Sprintf("%s%.0f %s %s", k.Sign(), mAmount.Float64, mCur.String, mCat.String)}, nil
	default:
		return &Undoable{Kind: "note", ID: noteID.Int64, Descr: truncate(noteText.String, 80)}, nil
	}
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// Delete удаляет заметку или денежную запись по id.
func (s *Store) Delete(kind string, id int64) error {
	var q string
	switch kind {
	case "note":
		q = "DELETE FROM notes WHERE id=?"
	case "money":
		q = "DELETE FROM money WHERE id=?"
	default:
		return fmt.Errorf("нечего удалять: %q", kind)
	}
	_, err := s.db.Exec(q, id)
	return err
}

// SaveVoice кладёт сырую расшифровку рядом с тем, что из неё разобрали.
// По этим парам потом видно, где ASR и парсер систематически ошибаются.
func (s *Store) SaveVoice(date, raw, parsed, level string) error {
	_, err := s.db.Exec("INSERT INTO voice (ts, date, raw, parsed, level) VALUES (?,?,?,?,?)",
		time.Now().UTC().Format(time.RFC3339), date, raw, parsed, level)
	return err
}

// JobLastRun возвращает дату последнего запуска джобы планировщика.
func (s *Store) JobLastRun(name string) (string, error) {
	var v string
	err := s.db.QueryRow("SELECT last_run FROM jobs WHERE name=?", name).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

// SetJobLastRun запоминает, что джоба отработала в указанную дату. Благодаря
// этому рестарт бота не приводит к повторной отправке напоминания.
func (s *Store) SetJobLastRun(name, date string) error {
	_, err := s.db.Exec("INSERT INTO jobs (name, last_run) VALUES (?,?) ON CONFLICT(name) DO UPDATE SET last_run=excluded.last_run", name, date)
	return err
}

// FirstDate возвращает самую раннюю дату с записью дня; пустую строку, если
// записей ещё нет.
func (s *Store) FirstDate() (string, error) {
	var v sql.NullString
	if err := s.db.QueryRow("SELECT MIN(date) FROM days").Scan(&v); err != nil {
		return "", err
	}
	return v.String, nil
}
