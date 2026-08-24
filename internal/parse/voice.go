package parse

import (
	"regexp"
	"strings"

	"github.com/murluckk/self-reinvention/internal/model"
)

// Voice — то, что удалось вытащить из фразы: запись дня, деньги и остаток в
// виде заметки.
type Voice struct {
	Day    *model.Day
	Money  []*model.Money
	Note   string
	Level  string // regex | llm | llm+regex | none
	Recogn int    // сколько полей записи дня распознано
}

// Count считает распознанные поля дня плюс денежные записи.
func (v *Voice) Count() int {
	n := 0
	if v.Day != nil {
		n = len(v.Day.SetFields())
	}
	return n + len(v.Money)
}

type rule struct {
	col string           // колонка поля записи дня
	res []*regexp.Regexp // альтернативные шаблоны, первый сработавший выигрывает
	val string           // фиксированное значение для строковых и булевых полей
}

// В Go \b — граница ASCII-слова и с кириллицей не работает вовсе, поэтому
// границы слова приходится выписывать явно.
const (
	bs = `(?:^|[^а-яa-z0-9])` // начало слова
	be = `(?:[^а-яa-z0-9]|$)` // конец слова
)

func rx(pats ...string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(pats))
	for _, p := range pats {
		out = append(out, regexp.MustCompile(`(?i)`+p))
	}
	return out
}

// Числовые правила: в первой группе шаблона должно оказаться значение.
var numRules = []rule{
	{col: "sleep", res: rx(
		`(?:спал|поспал|проспал)\D{0,12}?(\d{1,2}(?:[.,]\d+)?)`,
		`сон\D{0,6}?(\d{1,2}(?:[.,]\d+)?)`,
		`(\d{1,2}(?:[.,]\d+)?)\s*час\w*\s+сна`,
	)},
	{col: "wake", res: rx(
		`(?:подъем|встал|проснулся|проснул)\D{0,6}?(\d{1,2}(?:[:.]\d{2})?)`,
	)},
	{col: "bed", res: rx(
		`(?:отбой|лег|уснул|уснула|заснул)\D{0,6}?(\d{1,2}(?:[:.]\d{2})?)`,
	)},
	{col: "weight", res: rx(
		`вес\D{0,6}?(\d{2,3}(?:[.,]\d+)?)`,
	)},
	{col: "english", res: rx(
		`(?:англ|инглиш|english)\D{0,12}?(\d{1,3})`,
		`(\d{1,3})\s*минут\w*\s+англ`,
	)},
	{col: "kcal", res: rx(
		`(\d{3,5})\s*(?:ккал|килокалор|калор)`,
		`(?:ккал|калор)\D{0,6}?(\d{3,5})`,
	)},
	{col: "protein", res: rx(
		`(?:белк|белок|белка|протеин)\D{0,8}?(\d{2,3})`,
		`(\d{2,3})\s*(?:г|грамм\w*)\s+бел`,
	)},
	{col: "work", res: rx(
		`(?:работал|отработал|работы|работа)\D{0,10}?(\d{1,2}(?:[.,]\d+)?)\s*час`,
		`(\d{1,2}(?:[.,]\d+)?)\s*час\w*\s+работ`,
	)},
	{col: "focus", res: rx(`фокус\D{0,8}?([1-5])`)},
	{col: "mood", res: rx(`настроени\D{0,8}?([1-5])`)},
	{col: "energy", res: rx(`энерги\D{0,8}?([1-5])`)},
	{col: "telegram", res: rx(
		`(?:телеграм|тг)\D{0,12}?(\d{1,3})\s*раз`,
		`(\d{1,3})\s*раз\w*\D{0,12}?(?:телеграм|тг)`,
	)},
}

// Строковые правила: срабатывание шаблона означает конкретное значение.
var strRules = []rule{
	{col: "workout", val: "нет", res: rx(`не\s+трен`, `без\s+трен`, `трен\w*\s+нет`, `пропустил\s+трен`)},
	{col: "workout", val: "зал", res: rx(bs+`зал[аеуы]?`+be, `качалк`)},
	{col: "workout", val: "бег", res: rx(bs+`бег`, `пробежк`)},
	{col: "workout", val: "улица", res: rx(`турник`, `воркаут`, `улич\w*\s+трен`, `трен\w*\s+на\s+улиц`)},
}

// Булевы правила. neg — шаблоны, означающие «нет»; они проверяются первыми,
// потому что «не готовил» содержит в себе «готовил».
var boolRules = []struct {
	col string
	pos []*regexp.Regexp
	neg []*regexp.Regexp
}{
	{col: "cooked",
		neg: rx(`не\s+готов`, `без\s+готовк`),
		pos: rx(`готовил`, `готовк`)},
	{col: "day_off",
		neg: rx(`не\s+выходн`, `без\s+выходн`),
		pos: rx(bs+`выходн`, `не\s+работал`, `полный\s+отдых`)},
	{col: "shift",
		neg: rx(`без\s+смен`, `не\s+было\s+смен`),
		pos: rx(bs+`смен[ауы]`+be, `подработ`)},
	{col: "clean",
		neg: rx(`не\s+чист`, `сорвал`, bs+`выпил`, bs+`пил`+be, `алкогол`, `порн`),
		pos: rx(bs+`чист`, bs+`трезв`)},
}

var moneyRules = []struct {
	kind model.MoneyKind
	re   *regexp.Regexp
}{
	{model.MoneyExpense, regexp.MustCompile(`(?i)(?:потратил|расход|отдал|заплатил)\D{0,10}?(\d[\d\s]{2,})\s*(?:руб\w*|р\b|₽)?\s*(?:на\s+(\p{L}+))?`)},
	{model.MoneyIncome, regexp.MustCompile(`(?i)(?:получил|заработал|пришл[аои]|зарплат\w*|\bзп\b)\D{0,10}?(\d[\d\s]{2,})`)},
	{model.MoneySaving, regexp.MustCompile(`(?i)(?:отложил|накопил|сберёг|сберег)\D{0,10}?(\d[\d\s]{2,})`)},
}

var wsRe = regexp.MustCompile(`\s+`)

func normalize(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "ё", "е")
	return wsRe.ReplaceAllString(s, " ")
}

// ParseVoiceRegex — первый уровень разбора свободной речи: поиск ключевых слов
// регулярками. Он мгновенный, бесплатный и покрывает типовые фразы вроде
// «спал семь с половиной, зал, англ сорок минут, чисто». LLM зовём только там,
// где этого не хватило.
func ParseVoiceRegex(text, date string) *Voice {
	n := normalize(text)
	v := &Voice{Day: &model.Day{Date: date}, Level: "regex"}

	for _, r := range numRules {
		f, ok := model.FieldByColumn(r.col)
		if !ok {
			continue
		}
		for _, re := range r.res {
			m := re.FindStringSubmatchIndex(n)
			if m == nil {
				continue
			}
			val := n[m[2]:m[3]]
			if negatedAt(n, m[0]) {
				break
			}
			if err := setField(v.Day, f, val); err == nil {
				break
			}
		}
	}
	for _, r := range strRules {
		f, ok := model.FieldByColumn(r.col)
		if !ok || f.IsSet(v.Day) {
			continue
		}
		for _, re := range r.res {
			if re.MatchString(n) {
				_ = f.SetAny(v.Day, r.val)
				break
			}
		}
	}
	for _, r := range boolRules {
		f, ok := model.FieldByColumn(r.col)
		if !ok {
			continue
		}
		if matchAny(n, r.neg) {
			_ = f.SetAny(v.Day, false)
			continue
		}
		if matchAny(n, r.pos) {
			_ = f.SetAny(v.Day, true)
		}
	}
	// «выходной» и «работал 8 часов» одновременно — противоречие; доверяем часам.
	if w, ok := model.FieldByColumn("work"); ok && w.IsSet(v.Day) {
		if hours, _ := w.Get(v.Day).(*float64); hours != nil && *hours > 0 {
			if off, ok := model.FieldByColumn("day_off"); ok {
				_ = off.SetAny(v.Day, false)
			}
		}
	}
	for _, r := range moneyRules {
		m := r.re.FindStringSubmatch(n)
		if m == nil {
			continue
		}
		amount, err := model.ParseFloat(strings.ReplaceAll(m[1], " ", ""))
		if err != nil || amount <= 0 {
			continue
		}
		cat := r.kind.Label()
		if len(m) > 2 && m[2] != "" {
			cat = m[2]
		}
		v.Money = append(v.Money, &model.Money{
			Date: date, Kind: r.kind, Amount: amount, Currency: "RUB", Category: cat,
		})
	}
	v.Recogn = v.Count()
	return v
}

func matchAny(s string, res []*regexp.Regexp) bool {
	for _, re := range res {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

var negRe = regexp.MustCompile(`(?:^|\s)(?:не|без|нет)\s*$`)

// negatedAt проверяет, не стоит ли прямо перед совпадением отрицание: «не спал»
// не должно превратиться в часы сна.
func negatedAt(s string, idx int) bool {
	from := idx - 12
	if from < 0 {
		from = 0
	}
	return negRe.MatchString(s[from:idx])
}
