package bot

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/murluckk/self-reinvention/internal/model"
	"github.com/murluckk/self-reinvention/internal/parse"
)

// RemindIncome напоминает записать фактическую сумму: она может меняться, и
// без настройки нельзя безопасно добавлять выдуманный доход автоматически.
func (b *Bot) RemindIncome(ctx context.Context, userID int64, title string) error {
	return b.Notify(ctx, userID, fmt.Sprintf(
		"💰 Сегодня %s. Запиши фактическую сумму:\n/m +150000 %s\n\nЕсли часть сразу отложил:\n/m =50000 накопления",
		title, title,
	))
}

// SendMonthlyFinance присылает итог прошлого календарного месяца и, когда LLM
// настроена, добавляет короткий анализ структуры расходов.
func (b *Bot) SendMonthlyFinance(ctx context.Context, userID int64) error {
	now := b.now(userID)
	thisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	start := thisMonth.AddDate(0, -1, 0)
	end := thisMonth.AddDate(0, 0, -1)
	from, to := start.Format("2006-01-02"), end.Format("2006-01-02")
	money, err := b.st.Money(userID, from, to)
	if err != nil {
		return err
	}

	summary := financeSummary(start.Format("01.2006"), money)
	if b.llm != nil && len(money) > 0 {
		analysis, err := b.llm.Complete(ctx,
			"Ты финансовый аналитик личного бюджета. Отвечай по-русски, кратко и без морализаторства. "+
				"Опирайся только на переданные числа. Выдели крупнейшие категории, долю накоплений и 2 конкретных наблюдения.",
			summary,
		)
		if err != nil {
			b.log.Warn("ежемесячный AI-анализ финансов", "user_id", userID, "err", err)
		} else {
			summary += "\n\n🤖 Анализ\n" + analysis
		}
	}
	return b.Notify(ctx, userID, summary)
}

type financeTotals struct {
	income, expense, saved float64
	categories             map[string]float64
}

func financeSummary(month string, money []*model.Money) string {
	byCurrency := map[string]*financeTotals{}
	for _, m := range money {
		t := byCurrency[m.Currency]
		if t == nil {
			t = &financeTotals{categories: map[string]float64{}}
			byCurrency[m.Currency] = t
		}
		switch m.Kind {
		case model.MoneyIncome:
			t.income += m.Amount
		case model.MoneyExpense:
			t.expense += m.Amount
			category := strings.TrimSpace(m.Category)
			if category == "" {
				category = "без категории"
			}
			t.categories[category] += m.Amount
		case model.MoneySaving:
			t.saved += m.Amount
		}
	}
	var out strings.Builder
	fmt.Fprintf(&out, "📊 Финансы · %s", month)
	if len(byCurrency) == 0 {
		out.WriteString("\nЗаписей за месяц нет.")
		return out.String()
	}
	currencies := make([]string, 0, len(byCurrency))
	for currency := range byCurrency {
		currencies = append(currencies, currency)
	}
	sort.Strings(currencies)
	for _, currency := range currencies {
		t := byCurrency[currency]
		fmt.Fprintf(&out,
			"\n\n%s\nДоход: %s\nРасход: %s\nОтложено: %s\nИзменение капитала: %s",
			currency,
			parse.FormatAmount(t.income),
			parse.FormatAmount(t.expense),
			parse.FormatAmount(t.saved),
			parse.FormatAmount(t.income-t.expense),
		)
		if t.income > 0 {
			fmt.Fprintf(&out, "\nДоля накоплений: %.0f%%", t.saved/t.income*100)
		}
		type categoryTotal struct {
			name   string
			amount float64
		}
		categories := make([]categoryTotal, 0, len(t.categories))
		for name, amount := range t.categories {
			categories = append(categories, categoryTotal{name, amount})
		}
		sort.Slice(categories, func(i, j int) bool { return categories[i].amount > categories[j].amount })
		if len(categories) > 0 {
			out.WriteString("\nКатегории расходов:")
			for _, c := range categories {
				fmt.Fprintf(&out, "\n• %s — %s", c.name, parse.FormatAmount(c.amount))
			}
		}
	}
	return out.String()
}
