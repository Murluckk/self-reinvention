// Package model описывает данные, которые бот собирает: запись дня, заметки и
// деньги. Запись дня — единственный источник правды о наборе полей: из тегов
// структуры Day строятся парсер команды /d, JSON-схема для LLM, схема таблицы
// SQLite и человекочитаемый вывод. Добавил поле в структуру — оно появилось
// везде, разъехаться нечему.
package model

import (
	"fmt"
	"reflect"
	"strings"
	"time"
)

// Kind — тип значения поля. От него зависят парсинг, валидация и формат вывода.
type Kind string

const (
	KindFloat  Kind = "float"  // произвольное дробное число
	KindInt    Kind = "int"    // целое
	KindScale  Kind = "scale"  // целое 1..5
	KindBool   Kind = "bool"   // да/нет
	KindString Kind = "string" // произвольная строка
	KindTime   Kind = "time"   // время суток HH:MM
)

// Day — запись за один день. Все поля, кроме даты, опциональны: запись
// наполняется в течение дня по частям, и nil означает «ещё не заполнено», а не
// «ноль».
type Day struct {
	Date string `db:"date"`

	Sleep    *float64 `db:"sleep"    key:"сон,спал,sleep"          kind:"float"  label:"Сон"        unit:"ч"   desc:"часы сна за ночь"`
	Wake     *string  `db:"wake"     key:"подъем,подъём,встал,wake" kind:"time"   label:"Подъём"                desc:"время подъёма, HH:MM"`
	Bed      *string  `db:"bed"      key:"отбой,лег,лёг,bed"        kind:"time"   label:"Отбой"                 desc:"время отбоя, HH:MM"`
	Workout  *string  `db:"workout"  key:"трен,тренировка,workout"  kind:"string" label:"Тренировка"            desc:"тип тренировки: зал, бег, улица или нет"`
	Weight   *float64 `db:"weight"   key:"вес,weight"               kind:"float"  label:"Вес"        unit:"кг" desc:"вес тела в килограммах"`
	English  *int     `db:"english"  key:"англ,английский,english"  kind:"int"    label:"Английский" unit:"мин" desc:"минут английского"`
	Kcal     *int     `db:"kcal"     key:"ккал,калории,kcal"        kind:"int"    label:"Калории"    unit:"ккал" desc:"съедено килокалорий"`
	Protein  *int     `db:"protein"  key:"белок,protein"            kind:"int"    label:"Белок"      unit:"г"  desc:"съедено белка в граммах"`
	Cooked   *bool    `db:"cooked"   key:"готовил,cooked"           kind:"bool"   label:"Готовил"               desc:"готовил ли еду сам"`
	Work     *float64 `db:"work"     key:"работа,work"              kind:"float"  label:"Работа"     unit:"ч"  desc:"часов работы"`
	DayOff   *bool    `db:"day_off"  key:"выходной,dayoff"          kind:"bool"   label:"Выходной"              desc:"полный день без работы"`
	Shift    *bool    `db:"shift"    key:"смена,shift"              kind:"bool"   label:"Смена"                 desc:"оплачиваемый рабочий выходной"`
	Focus    *int     `db:"focus"    key:"фокус,focus"              kind:"scale"  label:"Фокус"                 desc:"концентрация по шкале 1-5"`
	Mood     *int     `db:"mood"     key:"настроение,mood"          kind:"scale"  label:"Настроение"            desc:"настроение по шкале 1-5"`
	Energy   *int     `db:"energy"   key:"энергия,energy"           kind:"scale"  label:"Энергия"               desc:"энергия по шкале 1-5"`
	Telegram *int     `db:"telegram" key:"тг,телеграм,tg"           kind:"int"    label:"Телеграм"              desc:"сколько раз залезал в телеграм вне разрешённых окон"`
	Clean    *bool    `db:"clean"    key:"чисто,clean"              kind:"bool"   label:"Чисто"                 desc:"день без алкоголя и без порно"`
	Note     *string  `db:"note"     key:"note,заметка,коммент"     kind:"string" label:"Заметка"               desc:"свободный комментарий к дню" rest:"true"`
}

// Field — описание одного поля записи дня, собранное из тегов структуры.
type Field struct {
	Index   int      // индекс поля в структуре Day, для reflect
	DB      string   // имя колонки в SQLite и ключ в JSON от LLM
	Kind    Kind     // тип значения
	Keys    []string // ключи, которые понимает парсер; первый — канонический
	Label   string   // человекочитаемое имя
	Unit    string   // единица измерения для вывода, может быть пустой
	Desc    string   // описание для JSON-схемы LLM
	Rest    bool     // забирает остаток строки, а не один токен
	GoField reflect.StructField
}

var (
	fields   []Field
	byKey    = map[string]*Field{}
	byColumn = map[string]*Field{}
)

func init() {
	t := reflect.TypeOf(Day{})
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		key := sf.Tag.Get("key")
		if key == "" {
			continue // Date и всё, что не является заполняемым полем
		}
		f := Field{
			Index:   i,
			DB:      sf.Tag.Get("db"),
			Kind:    Kind(sf.Tag.Get("kind")),
			Keys:    strings.Split(key, ","),
			Label:   sf.Tag.Get("label"),
			Unit:    sf.Tag.Get("unit"),
			Desc:    sf.Tag.Get("desc"),
			Rest:    sf.Tag.Get("rest") == "true",
			GoField: sf,
		}
		fields = append(fields, f)
	}
	for i := range fields {
		f := &fields[i]
		byColumn[f.DB] = f
		for _, k := range f.Keys {
			byKey[k] = f
		}
	}
}

// Fields возвращает поля записи дня в порядке объявления.
func Fields() []Field { return fields }

// FieldByKey ищет поле по любому из его ключей (регистр не важен).
func FieldByKey(k string) (*Field, bool) {
	f, ok := byKey[strings.ToLower(strings.TrimSpace(k))]
	return f, ok
}

// FieldByColumn ищет поле по имени колонки, оно же имя ключа в JSON от LLM.
func FieldByColumn(c string) (*Field, bool) {
	f, ok := byColumn[strings.ToLower(strings.TrimSpace(c))]
	return f, ok
}

// Get возвращает значение поля как *float64/*int/*bool/*string либо nil.
func (f *Field) Get(d *Day) any {
	v := reflect.ValueOf(d).Elem().Field(f.Index)
	if v.IsNil() {
		return nil
	}
	return v.Interface()
}

// IsSet сообщает, заполнено ли поле.
func (f *Field) IsSet(d *Day) bool {
	return !reflect.ValueOf(d).Elem().Field(f.Index).IsNil()
}

// Clear сбрасывает поле в «не заполнено».
func (f *Field) Clear(d *Day) {
	v := reflect.ValueOf(d).Elem().Field(f.Index)
	v.Set(reflect.Zero(v.Type()))
}

// SetFloat, SetInt, SetBool, SetString кладут значение в поле, приводя его к
// типу поля. Возвращают ошибку, если тип не подходит.
func (f *Field) set(d *Day, val reflect.Value) {
	dst := reflect.ValueOf(d).Elem().Field(f.Index)
	p := reflect.New(dst.Type().Elem())
	p.Elem().Set(val)
	dst.Set(p)
}

// SetAny кладёт в поле значение произвольного типа, приводя его к типу поля.
// Используется парсерами и декодером ответа LLM.
func (f *Field) SetAny(d *Day, v any) error {
	switch f.Kind {
	case KindFloat:
		x, err := toFloat(v)
		if err != nil {
			return err
		}
		f.set(d, reflect.ValueOf(x))
	case KindInt, KindScale:
		x, err := toFloat(v)
		if err != nil {
			return err
		}
		n := int(x + 0.5)
		if x < 0 {
			n = int(x - 0.5)
		}
		if f.Kind == KindScale && (n < 1 || n > 5) {
			return fmt.Errorf("ожидалось число от 1 до 5, а не %d", n)
		}
		f.set(d, reflect.ValueOf(n))
	case KindBool:
		x, err := toBool(v)
		if err != nil {
			return err
		}
		f.set(d, reflect.ValueOf(x))
	case KindString, KindTime:
		s, ok := v.(string)
		if !ok {
			s = fmt.Sprint(v)
		}
		s = strings.TrimSpace(s)
		if f.Kind == KindTime {
			t, err := NormalizeTime(s)
			if err != nil {
				return err
			}
			s = t
		}
		f.set(d, reflect.ValueOf(s))
	default:
		return fmt.Errorf("неизвестный тип поля %q", f.Kind)
	}
	return nil
}

func toFloat(v any) (float64, error) {
	switch x := v.(type) {
	case float64:
		return x, nil
	case float32:
		return float64(x), nil
	case int:
		return float64(x), nil
	case int64:
		return float64(x), nil
	case string:
		return ParseFloat(x)
	}
	return 0, fmt.Errorf("ожидалось число, а не %v", v)
}

func toBool(v any) (bool, error) {
	switch x := v.(type) {
	case bool:
		return x, nil
	case float64:
		return x != 0, nil
	case int:
		return x != 0, nil
	case string:
		return ParseBool(x)
	}
	return false, fmt.Errorf("ожидалось да/нет, а не %v", v)
}

// Merge переносит в dst все заполненные поля src. Пустые поля src не затирают
// то, что уже записано: в этом суть дописывания записи по частям.
func Merge(dst, src *Day) {
	dv := reflect.ValueOf(dst).Elem()
	sv := reflect.ValueOf(src).Elem()
	for i := range fields {
		f := &fields[i]
		s := sv.Field(f.Index)
		if s.IsNil() {
			continue
		}
		dv.Field(f.Index).Set(s)
	}
}

// SetFields возвращает поля, заполненные в записи.
func (d *Day) SetFields() []Field {
	var out []Field
	for i := range fields {
		if fields[i].IsSet(d) {
			out = append(out, fields[i])
		}
	}
	return out
}

// MissingFields возвращает поля, которые ещё не заполнены. Заметка не считается
// обязательной, поэтому в список не попадает.
func (d *Day) MissingFields() []Field {
	var out []Field
	for i := range fields {
		f := fields[i]
		if f.DB == "note" || f.IsSet(d) {
			continue
		}
		out = append(out, f)
	}
	return out
}

// Empty сообщает, что в записи нет ни одного заполненного поля.
func (d *Day) Empty() bool { return len(d.SetFields()) == 0 }

// FormatValue печатает значение поля так, как его увидит человек.
func (f *Field) FormatValue(d *Day) string {
	v := f.Get(d)
	if v == nil {
		return "—"
	}
	var s string
	switch x := v.(type) {
	case *float64:
		s = strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.2f", *x), "0"), ".")
	case *int:
		s = fmt.Sprint(*x)
	case *bool:
		if *x {
			return "да"
		}
		return "нет"
	case *string:
		s = *x
	}
	if f.Unit != "" {
		s += " " + f.Unit
	}
	return s
}

// Note — заметка на ходу.
type Note struct {
	ID   int64
	TS   time.Time
	Date string
	Tag  string
	Text string
}

// Money — денежная запись. Kind различает доход, расход и отложенное.
type Money struct {
	ID       int64
	TS       time.Time
	Date     string
	Kind     MoneyKind
	Amount   float64
	Currency string
	Category string
	Comment  string
}

// MoneyKind — вид денежной записи.
type MoneyKind string

const (
	MoneyIncome  MoneyKind = "income"  // +
	MoneyExpense MoneyKind = "expense" // -
	MoneySaving  MoneyKind = "saving"  // =
)

// Sign возвращает знак, которым запись вводилась.
func (k MoneyKind) Sign() string {
	switch k {
	case MoneyIncome:
		return "+"
	case MoneyExpense:
		return "-"
	default:
		return "="
	}
}

// Label — русское имя вида записи.
func (k MoneyKind) Label() string {
	switch k {
	case MoneyIncome:
		return "доход"
	case MoneyExpense:
		return "расход"
	default:
		return "отложено"
	}
}
