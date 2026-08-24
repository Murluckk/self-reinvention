// Package parse разбирает пользовательский ввод: команды /d и /m, заметки и
// свободную речь из голосовых. Общий принцип — быть терпимым: в 23:00 никто не
// вспоминает точный синтаксис, поэтому принимаем и `сон=7.5`, и `сон 7.5`, и
// `сон 7,5ч`, а о непонятом честно сообщаем текстом, а не молча игнорируем.
package parse

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/murluckk/self-reinvention/internal/model"
)

var dateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

type token struct {
	text  string
	start int
	end   int
}

func tokenize(s string) []token {
	var out []token
	start := -1
	for i, r := range s {
		if unicode.IsSpace(r) {
			if start >= 0 {
				out = append(out, token{s[start:i], start, i})
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, token{s[start:], start, len(s)})
	}
	return out
}

// DayCommand — результат разбора /d.
type DayCommand struct {
	Day    *model.Day // заполненные поля; дата уже проставлена
	Errors []string   // человекочитаемые претензии к вводу
}

// ParseDay разбирает аргументы команды /d. Первым аргументом может идти дата
// YYYY-MM-DD, чтобы дописать день задним числом; иначе берётся today.
//
// Формат значений намеренно свободный: `ключ=значение`, `ключ:значение` и
// `ключ значение` равнозначны, булев ключ можно писать голым флагом (`чисто`),
// а числа принимаются с запятой и с единицами измерения (`вес 73,4кг`).
func ParseDay(args, today string) DayCommand {
	res := DayCommand{Day: &model.Day{Date: today}}
	toks := tokenize(args)
	i := 0
	if len(toks) > 0 && dateRe.MatchString(toks[0].text) {
		res.Day.Date = toks[0].text
		i++
	}
	if i >= len(toks) {
		res.Errors = append(res.Errors, "нечего записывать: после /d нет ни одного ключа")
		return res
	}
	for i < len(toks) {
		t := toks[i]
		key, val := t.text, ""
		hasVal := false
		if idx := strings.IndexAny(t.text, "=:"); idx > 0 {
			key, val, hasVal = t.text[:idx], t.text[idx+1:], true
		}
		f, ok := model.FieldByKey(key)
		if !ok {
			// Голые значения-сокращения: «зал» вместо «трен=зал». Ради скорости
			// ввода — то, что пишется чаще всего, должно писаться короче всего.
			if bv, isBare := bareValues[strings.ToLower(key)]; isBare && !hasVal {
				if bf, found := model.FieldByColumn(bv.col); found {
					if err := bf.SetAny(res.Day, bv.val); err != nil {
						res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", key, err))
					}
					i++
					continue
				}
			}
			// Съедаем и значение неизвестного ключа, иначе на «фигня 5» прилетит
			// две претензии вместо одной.
			if !hasVal && i+1 < len(toks) {
				if _, known := model.FieldByKey(toks[i+1].text); !known {
					i++
				}
			}
			res.Errors = append(res.Errors, fmt.Sprintf("не знаю ключ %q", key))
			i++
			continue
		}
		if f.Rest {
			// note забирает всё до конца сообщения
			var rest string
			if hasVal {
				rest = args[t.start+len(key)+1:]
			} else if i+1 < len(toks) {
				rest = args[toks[i+1].start:]
			}
			rest = strings.TrimSpace(rest)
			if rest == "" {
				res.Errors = append(res.Errors, fmt.Sprintf("у ключа %q пустое значение", key))
			} else if err := f.SetAny(res.Day, rest); err != nil {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", key, err))
			}
			break
		}
		if !hasVal {
			switch {
			case f.Kind == model.KindBool:
				// голый флаг означает «да», но `чисто нет` тоже надо понять
				if i+1 < len(toks) {
					if _, err := model.ParseBool(toks[i+1].text); err == nil {
						val = toks[i+1].text
						i++
						break
					}
				}
				val = "да"
			case i+1 < len(toks):
				val = toks[i+1].text
				i++
			default:
				res.Errors = append(res.Errors, fmt.Sprintf("у ключа %q нет значения", key))
				i++
				continue
			}
		}
		if err := setField(res.Day, f, val); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("не понял «%s %s»: %v", key, val, err))
		}
		i++
	}
	return res
}

// bareValues — сокращения, которые можно писать без ключа.
var bareValues = map[string]struct{ col, val string }{
	"зал":      {"workout", "зал"},
	"качалка":  {"workout", "зал"},
	"бег":      {"workout", "бег"},
	"пробежка": {"workout", "бег"},
	"улица":    {"workout", "улица"},
	"турник":   {"workout", "улица"},
	"безтрена": {"workout", "нет"},
	"отдых":    {"workout", "нет"},
}

var unitSuffix = regexp.MustCompile(`^([0-9]+(?:[.,][0-9]+)?)\s*[^\s0-9.,:]*$`)

// setField кладёт строковое значение в поле, отрезая единицы измерения у чисел:
// `73,4кг` и `40мин` должны проходить так же, как `73.4` и `40`.
func setField(d *model.Day, f *model.Field, val string) error {
	val = strings.TrimSpace(val)
	if val == "" {
		return fmt.Errorf("пустое значение")
	}
	switch f.Kind {
	case model.KindFloat, model.KindInt, model.KindScale:
		if m := unitSuffix.FindStringSubmatch(val); m != nil {
			val = m[1]
		}
	}
	return f.SetAny(d, val)
}

// Summary печатает заполненные поля записи одной строкой на поле — то, что бот
// показывает после /d и в подтверждении голосового.
func Summary(d *model.Day) string {
	var b strings.Builder
	for _, f := range d.SetFields() {
		fmt.Fprintf(&b, "%s: %s\n", f.Label, f.FormatValue(d))
	}
	return b.String()
}
