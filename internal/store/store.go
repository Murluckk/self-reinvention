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

// Open открывает базу и приводит схему в актуальное состояние. legacyOwnerID
// нужен только один раз: старые однопользовательские записи закрепляются за ним.
func Open(path string, legacyOwnerID ...int64) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("каталог для базы: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	// Нагрузка маленькая, а один коннект избавляет SQLite от database is locked
	// при одновременной записи бота и чтении дашборда.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	var ownerID int64
	if len(legacyOwnerID) > 0 {
		ownerID = legacyOwnerID[0]
	}
	if err := s.migrate(ownerID); err != nil {
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
	case model.KindInt, model.KindScale, model.KindDuration, model.KindBool:
		return "INTEGER"
	default:
		return "TEXT"
	}
}

// migrate создаёт таблицы и добавляет колонки под поля, которых ещё нет в базе.
// Схема таблицы дней выводится из структуры model.Day, поэтому новое поле в
// структуре само приезжает в базу при следующем запуске.
func (s *Store) migrate(legacyOwnerID int64) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS days (
			user_id INTEGER NOT NULL, date TEXT NOT NULL, updated_at TEXT,
			PRIMARY KEY (user_id, date))`,
		`CREATE TABLE IF NOT EXISTS notes (
			id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER NOT NULL,
			ts TEXT NOT NULL, date TEXT NOT NULL, tag TEXT NOT NULL DEFAULT '', text TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS money (
			id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER NOT NULL,
			ts TEXT NOT NULL, date TEXT NOT NULL, kind TEXT NOT NULL,
			amount REAL NOT NULL, currency TEXT NOT NULL DEFAULT 'RUB',
			category TEXT NOT NULL DEFAULT '', comment TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS voice (
			id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER NOT NULL,
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
	if err := s.migrateUsers(legacyOwnerID); err != nil {
		return err
	}
	if err := s.resetDaySchema(); err != nil {
		return err
	}
	for _, q := range []string{
		`CREATE INDEX IF NOT EXISTS notes_user_date ON notes(user_id, date)`,
		`CREATE INDEX IF NOT EXISTS money_user_date ON money(user_id, date)`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("индекс пользователя: %w", err)
		}
	}
	return nil
}

// resetDaySchema один раз начинает упрощённый дневник с чистого листа. Формат
// поля workout изменился со строки на bool, а пользователь явно отказался от
// старых дневных данных, поэтому переносить несовместимые значения не нужно.
func (s *Store) resetDaySchema() error {
	const name = "simplified_day_v3"
	var applied int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE name=?", name).Scan(&applied); err != nil {
		return err
	}
	if applied > 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	defs := make([]string, 0, len(model.Fields()))
	for _, f := range model.Fields() {
		defs = append(defs, f.DB+" "+sqlType(f.Kind))
	}
	create := `CREATE TABLE days_v3 (
		user_id INTEGER NOT NULL, date TEXT NOT NULL, updated_at TEXT`
	if len(defs) > 0 {
		create += ", " + strings.Join(defs, ", ")
	}
	create += `, PRIMARY KEY (user_id, date))`
	if _, err := tx.Exec(create); err != nil {
		return fmt.Errorf("новая схема дневника: %w", err)
	}
	if _, err := tx.Exec(`DROP TABLE days`); err != nil {
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE days_v3 RENAME TO days`); err != nil {
		return err
	}
	if _, err := tx.Exec(
		"INSERT INTO schema_migrations (name, applied_at) VALUES (?,?)",
		name, time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		return err
	}
	return tx.Commit()
}

// migrateUsers переводит старую базу с PRIMARY KEY(date) на независимые
// пространства данных с PRIMARY KEY(user_id,date).
func (s *Store) migrateUsers(legacyOwnerID int64) error {
	have, err := s.columns("days")
	if err != nil {
		return err
	}
	if !have["user_id"] {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		defs := make([]string, 0, len(model.Fields()))
		cols := make([]string, 0, len(model.Fields()))
		for _, f := range model.Fields() {
			defs = append(defs, f.DB+" "+sqlType(f.Kind))
			cols = append(cols, f.DB)
		}
		create := `CREATE TABLE days_v2 (
			user_id INTEGER NOT NULL, date TEXT NOT NULL, updated_at TEXT`
		if len(defs) > 0 {
			create += ", " + strings.Join(defs, ", ")
		}
		create += `, PRIMARY KEY (user_id, date))`
		if _, err := tx.Exec(create); err != nil {
			return fmt.Errorf("создание многопользовательской days: %w", err)
		}
		insertCols := "user_id,date,updated_at"
		selectCols := "?,date,updated_at"
		if len(cols) > 0 {
			insertCols += "," + strings.Join(cols, ",")
			selectCols += "," + strings.Join(cols, ",")
		}
		if _, err := tx.Exec(
			"INSERT INTO days_v2 ("+insertCols+") SELECT "+selectCols+" FROM days",
			legacyOwnerID,
		); err != nil {
			return fmt.Errorf("перенос дней по пользователям: %w", err)
		}
		if _, err := tx.Exec(`DROP TABLE days`); err != nil {
			return err
		}
		if _, err := tx.Exec(`ALTER TABLE days_v2 RENAME TO days`); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}

	for _, table := range []string{"notes", "money", "voice"} {
		cols, err := s.columns(table)
		if err != nil {
			return err
		}
		if cols["user_id"] {
			continue
		}
		q := fmt.Sprintf("ALTER TABLE %s ADD COLUMN user_id INTEGER NOT NULL DEFAULT %d", table, legacyOwnerID)
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("добавление user_id в %s: %w", table, err)
		}
	}
	return nil
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

// UpsertDay записывает заполненные поля d в запись пользователя за d.Date, не
// трогая те, что в d равны nil. Это и есть дописывание задним числом.
func (s *Store) UpsertDay(userID int64, d *model.Day) error {
	if d.Date == "" {
		return fmt.Errorf("не указана дата записи")
	}
	set := d.SetFields()
	cols := []string{"user_id", "date", "updated_at"}
	vals := []any{userID, d.Date, time.Now().UTC().Format(time.RFC3339)}
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
		"INSERT INTO days (%s) VALUES (%s) ON CONFLICT(user_id,date) DO UPDATE SET %s",
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
		case model.KindInt, model.KindScale, model.KindDuration, model.KindBool:
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
func (s *Store) GetDay(userID int64, date string) (*model.Day, error) {
	cols, fs := dayColumns()
	rows, err := s.db.Query("SELECT "+cols+" FROM days WHERE user_id=? AND date=?", userID, date)
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
func (s *Store) Days(userID int64, from, to string) ([]*model.Day, error) {
	cols, fs := dayColumns()
	rows, err := s.db.Query("SELECT "+cols+" FROM days WHERE user_id=? AND date>=? AND date<=? ORDER BY date", userID, from, to)
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
func (s *Store) AddNote(userID int64, n *model.Note) error {
	res, err := s.db.Exec("INSERT INTO notes (user_id, ts, date, tag, text) VALUES (?,?,?,?,?)",
		userID, n.TS.UTC().Format(time.RFC3339), n.Date, n.Tag, n.Text)
	if err != nil {
		return fmt.Errorf("запись заметки: %w", err)
	}
	n.ID, _ = res.LastInsertId()
	return nil
}

// Notes возвращает заметки за период по возрастанию времени.
func (s *Store) Notes(userID int64, from, to string) ([]*model.Note, error) {
	rows, err := s.db.Query("SELECT id, ts, date, tag, text FROM notes WHERE user_id=? AND date>=? AND date<=? ORDER BY ts, id", userID, from, to)
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
func (s *Store) AddMoney(userID int64, m *model.Money) error {
	res, err := s.db.Exec("INSERT INTO money (user_id, ts, date, kind, amount, currency, category, comment) VALUES (?,?,?,?,?,?,?,?)",
		userID, m.TS.UTC().Format(time.RFC3339), m.Date, string(m.Kind), m.Amount, m.Currency, m.Category, m.Comment)
	if err != nil {
		return fmt.Errorf("запись денег: %w", err)
	}
	m.ID, _ = res.LastInsertId()
	return nil
}

// Money возвращает денежные записи за период.
func (s *Store) Money(userID int64, from, to string) ([]*model.Money, error) {
	return s.queryMoney("SELECT id, ts, date, kind, amount, currency, category, comment FROM money WHERE user_id=? AND date>=? AND date<=? ORDER BY ts, id", userID, from, to)
}

// MoneyUntil возвращает все денежные записи по дату включительно — нужно для
// накопительного итога капитала.
func (s *Store) MoneyUntil(userID int64, to string) ([]*model.Money, error) {
	return s.queryMoney("SELECT id, ts, date, kind, amount, currency, category, comment FROM money WHERE user_id=? AND date<=? ORDER BY ts, id", userID, to)
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
func (s *Store) LastUndoable(userID int64) (*Undoable, error) {
	var (
		noteID, moneyID   sql.NullInt64
		noteTS, moneyTS   sql.NullString
		noteText          sql.NullString
		mKind, mCur, mCat sql.NullString
		mAmount           sql.NullFloat64
	)
	row := s.db.QueryRow("SELECT id, ts, text FROM notes WHERE user_id=? ORDER BY ts DESC, id DESC LIMIT 1", userID)
	if err := row.Scan(&noteID, &noteTS, &noteText); err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	row = s.db.QueryRow("SELECT id, ts, kind, amount, currency, category FROM money WHERE user_id=? ORDER BY ts DESC, id DESC LIMIT 1", userID)
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
func (s *Store) Delete(userID int64, kind string, id int64) error {
	var q string
	switch kind {
	case "note":
		q = "DELETE FROM notes WHERE user_id=? AND id=?"
	case "money":
		q = "DELETE FROM money WHERE user_id=? AND id=?"
	default:
		return fmt.Errorf("нечего удалять: %q", kind)
	}
	_, err := s.db.Exec(q, userID, id)
	return err
}

// SaveVoice кладёт сырую расшифровку рядом с тем, что из неё разобрали.
// По этим парам потом видно, где ASR и парсер систематически ошибаются.
func (s *Store) SaveVoice(userID int64, date, raw, parsed, level string) error {
	_, err := s.db.Exec("INSERT INTO voice (user_id, ts, date, raw, parsed, level) VALUES (?,?,?,?,?,?)",
		userID, time.Now().UTC().Format(time.RFC3339), date, raw, parsed, level)
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
func (s *Store) FirstDate(userID int64) (string, error) {
	var v sql.NullString
	if err := s.db.QueryRow("SELECT MIN(date) FROM days WHERE user_id=?", userID).Scan(&v); err != nil {
		return "", err
	}
	return v.String, nil
}

// Backup создаёт консистентную копию SQLite, включая данные из WAL. VACUUM INTO
// работает через текущее соединение и поэтому безопаснее простого копирования
// файла во время работы бота.
func (s *Store) Backup(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	quoted := strings.ReplaceAll(path, "'", "''")
	if _, err := s.db.Exec("VACUUM INTO '" + quoted + "'"); err != nil {
		return fmt.Errorf("backup sqlite: %w", err)
	}
	return os.Chmod(path, 0o600)
}
