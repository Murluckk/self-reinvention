package parse

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/murluckk/self-reinvention/internal/model"
)

// amountRe разбирает первый аргумент /m: знак, сумму и необязательный суффикс
// валюты, слитно или через пробел.
var amountRe = regexp.MustCompile(`^([+\-=])\s*(-?[0-9][0-9 ]*(?:[.,][0-9]+)?)\s*([^\s0-9]*)$`)

var currencies = map[string]string{
	"":     "RUB",
	"р":    "RUB",
	"₽":    "RUB",
	"руб":  "RUB",
	"rub":  "RUB",
	"$":    "USD",
	"usd":  "USD",
	"€":    "EUR",
	"eur":  "EUR",
	"usdt": "USDT",
	"btc":  "BTC",
}

// ParseMoney разбирает аргументы команды /m:
//
//	+250000 зп            — доход
//	-1200 еда обед в кафе — расход, «обед в кафе» уходит в комментарий
//	=180000 накопления    — отложено
//	=5000$ холодный кошелёк — то же, но в долларах
//	=-30000 накопления    — снятие из накоплений
func ParseMoney(args, today string) (*model.Money, error) {
	toks := tokenize(args)
	if len(toks) == 0 {
		return nil, fmt.Errorf("после /m нужна сумма со знаком, например: /m -1200 еда")
	}
	i := 0
	date := today
	if dateRe.MatchString(toks[0].text) {
		date = toks[0].text
		i++
	}
	if i >= len(toks) {
		return nil, fmt.Errorf("после даты нужна сумма со знаком, например: /m -1200 еда")
	}
	amountTok := toks[i]
	m := amountRe.FindStringSubmatch(amountTok.text)
	if m == nil {
		return nil, fmt.Errorf("не понял сумму %q: нужен знак и число, например +250000, -1200 или =5000$", amountTok.text)
	}
	amount, err := model.ParseFloat(strings.ReplaceAll(m[2], " ", ""))
	if err != nil {
		return nil, fmt.Errorf("не понял сумму %q: %v", amountTok.text, err)
	}
	cur, ok := currencies[strings.ToLower(m[3])]
	if !ok {
		return nil, fmt.Errorf("не знаю валюту %q; знаю ₽/$/€/usdt/btc", m[3])
	}
	var kind model.MoneyKind
	switch m[1] {
	case "+":
		kind = model.MoneyIncome
	case "-":
		kind = model.MoneyExpense
	default:
		kind = model.MoneySaving
	}
	// Минус у отложенного — снятие из накоплений: «=-5000 накопления» уменьшает
	// капитал. Доход и расход отрицательными не бывают, знак у них и так есть.
	if amount <= 0 && kind != model.MoneySaving {
		return nil, fmt.Errorf("сумма должна быть больше нуля")
	}
	if amount == 0 {
		return nil, fmt.Errorf("нулевая сумма ничего не меняет")
	}
	out := &model.Money{Date: date, Kind: kind, Amount: amount, Currency: cur}
	i++
	if i < len(toks) {
		out.Category = toks[i].text
		i++
	}
	if i < len(toks) {
		out.Comment = strings.TrimSpace(args[toks[i].start:])
	}
	if out.Category == "" {
		return nil, fmt.Errorf("не хватает категории, например: /m -1200 еда")
	}
	return out, nil
}

// FormatMoney печатает денежную запись одной строкой.
func FormatMoney(m *model.Money) string {
	s := fmt.Sprintf("%s%s %s — %s", m.Kind.Sign(), FormatAmount(m.Amount), m.Currency, m.Category)
	if m.Comment != "" {
		s += " (" + m.Comment + ")"
	}
	return s
}

// FormatAmount печатает сумму с разделителями тысяч и без лишних нулей.
func FormatAmount(v float64) string {
	s := fmt.Sprintf("%.2f", v)
	s = strings.TrimSuffix(strings.TrimRight(s, "0"), ".")
	intPart, frac := s, ""
	if i := strings.Index(s, "."); i >= 0 {
		intPart, frac = s[:i], s[i:]
	}
	neg := strings.HasPrefix(intPart, "-")
	intPart = strings.TrimPrefix(intPart, "-")
	var b strings.Builder
	for i, r := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteRune(' ')
		}
		b.WriteRune(r)
	}
	out := b.String() + frac
	if neg {
		out = "-" + out
	}
	return out
}
