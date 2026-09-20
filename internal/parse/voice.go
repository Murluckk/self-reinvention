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
	{col: "wake", res: rx(
		`(?:подъем|встал|проснулся|проснул)\D{0,6}?(\d{1,2}(?:[:.]\d{2})?)`,
	)},
	{col: "bed", res: rx(
		`(?:отбой|лег|уснул|уснула|заснул)\D{0,6}?(\d{1,2}(?:[:.]\d{2})?)`,
	)},
	{col: "algorithms", res: rx(
		`(?:алго|алгоритм)\D{0,12}?(\d{1,3})\s*мин`,
		`(\d{1,3})\s*минут\w*\D{0,8}?(?:алго|алгоритм)`,
	)},
	{col: "system_design", res: rx(
		`(?:системн[а-яё]*\s+дизайн|системдизайн|системы)\D{0,12}?(\d{1,3})\s*мин`,
		`(\d{1,3})\s*минут[а-яё]*\D{0,8}?(?:системн[а-яё]*\s+дизайн|системы)`,
	)},
	{col: "mood", res: rx(`настроени\D{0,8}?(10|[1-9])(?:\D|$)`)},
	{col: "mood", res: rx(`состояни\D{0,8}?(10|[1-9])(?:\D|$)`)},
}

// Булевы правила. neg — шаблоны, означающие «нет»; они проверяются первыми,
// потому что «не готовил» содержит в себе «готовил».
var boolRules = []struct {
	col string
	pos []*regexp.Regexp
	neg []*regexp.Regexp
}{
	{col: "algorithms",
		neg: rx(`(?:алго|алгоритм)[а-яё]*\s+(?:не|нет)`, `не\s+занимал[а-яё]*\s+(?:алго|алгоритм)`),
		pos: nil},
	{col: "system_design",
		neg: rx(`(?:систем[а-яё]*\s+дизайн|системы)\s+(?:не|нет)`, `не\s+занимал[а-яё]*\s+систем`),
		pos: nil},
	{col: "workout",
		neg: rx(`не\s+трен`, `без\s+трен`, `трен\w*\s+нет`, `пропустил\s+трен`),
		pos: rx(bs+`зал[аеуы]?`+be, `качалк`, bs+`бег`, `пробежк`, `турник`, `воркаут`, `трениров`)},
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
