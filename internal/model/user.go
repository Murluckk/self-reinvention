package model

import (
	"slices"
	"strings"
	"time"
)

// FieldSet — колонки, которые отслеживает конкретный пользователь. Набор
// хранится в базе, поэтому новый человек получает свои показатели без правки
// кода: достаточно выбрать их из каталога полей Day. nil означает «все поля».
type FieldSet []string

// Has сообщает, входит ли колонка в набор. Заметка к дню доступна всем:
// это свободный текст, а не показатель.
func (fs FieldSet) Has(col string) bool {
	return fs == nil || col == "note" || slices.Contains(fs, col)
}

// Fields возвращает поля набора в порядке каталога, без заметки.
func (fs FieldSet) Fields() []Field {
	var out []Field
	for _, f := range fields {
		if f.DB != "note" && fs.Has(f.DB) {
			out = append(out, f)
		}
	}
	return out
}

// Toggle добавляет колонку в набор или убирает её оттуда. Порядок всегда
// каталожный, чтобы отчёты не зависели от порядка нажатий.
func (fs FieldSet) Toggle(col string) FieldSet {
	on := map[string]bool{}
	for _, c := range fs {
		on[c] = true
	}
	on[col] = !on[col]
	out := FieldSet{}
	for _, f := range fields {
		if f.DB != "note" && on[f.DB] {
			out = append(out, f.DB)
		}
	}
	return out
}

// String сериализует набор для базы.
func (fs FieldSet) String() string { return strings.Join(fs, ",") }

// ParseFieldSet читает набор из базы, отбрасывая колонки, которых больше нет.
func ParseFieldSet(s string) FieldSet {
	out := FieldSet{}
	for c := range strings.SplitSeq(s, ",") {
		c = strings.TrimSpace(c)
		if _, ok := byColumn[c]; ok && c != "note" {
			out = append(out, c)
		}
	}
	return out
}

// Preset — готовый набор показателей, с которого начинает новый пользователь.
type Preset struct {
	Name     string
	Title    string
	Fields   FieldSet
	Finance  bool
	Timezone string
}

// Presets — стартовые профили. pasha и sveta совпадают с тем, что раньше было
// зашито в код, basic достаётся приглашённым: дальше человек настроит /fields.
var Presets = []Preset{
	{
		Name: "pasha", Title: "режим, алгоритмы, системный дизайн, финансы",
		Fields:  FieldSet{"wake", "bed", "algorithms", "system_design", "workout", "mood"},
		Finance: true, Timezone: "Asia/Vladivostok",
	},
	{
		Name: "sveta", Title: "режим, прогулки, учёба, питание",
		Fields:   FieldSet{"wake", "bed", "workout", "walk", "study", "useful", "mood", "sweet", "alcohol"},
		Timezone: "Asia/Irkutsk",
	},
	{
		Name: "basic", Title: "сон, тренировка, состояние",
		Fields:   FieldSet{"wake", "bed", "workout", "mood"},
		Timezone: "Europe/Moscow",
	},
}

// PresetByName ищет стартовый профиль.
func PresetByName(name string) (Preset, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, p := range Presets {
		if p.Name == name {
			return p, true
		}
	}
	return Preset{}, false
}

// User — участник трекера со своими показателями, часовым поясом и доступом
// к дашборду. Пароль хранится только хэшем.
type User struct {
	ID           int64 // Telegram ID
	Name         string
	Fields       FieldSet
	Finance      bool
	Timezone     string
	Login        string
	PasswordHash string
	Share        bool // показывать друзьям серии и закрытие дня
	CreatedAt    time.Time

	Loc *time.Location // загруженный Timezone; заполняет реестр
}

// Location возвращает часовой пояс пользователя, UTC если он не загружен.
func (u *User) Location() *time.Location {
	if u.Loc != nil {
		return u.Loc
	}
	if loc, err := time.LoadLocation(u.Timezone); err == nil {
		return loc
	}
	return time.UTC
}

// Now — текущее время пользователя.
func (u *User) Now() time.Time { return time.Now().In(u.Location()) }

// Today — сегодняшняя дата пользователя, YYYY-MM-DD.
func (u *User) Today() string { return u.Now().Format("2006-01-02") }
