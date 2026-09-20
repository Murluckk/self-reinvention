package bot

import (
	"strings"
	"testing"

	"github.com/murluckk/self-reinvention/internal/model"
)

func TestFinanceSummaryAggregatesCategories(t *testing.T) {
	money := []*model.Money{
		{Kind: model.MoneyIncome, Amount: 200000, Currency: "RUB", Category: "зарплата"},
		{Kind: model.MoneyExpense, Amount: 30000, Currency: "RUB", Category: "жильё"},
		{Kind: model.MoneyExpense, Amount: 1200, Currency: "RUB", Category: "еда"},
		{Kind: model.MoneyExpense, Amount: 800, Currency: "RUB", Category: "еда"},
		{Kind: model.MoneySaving, Amount: 100000, Currency: "RUB", Category: "накопления"},
	}
	got := financeSummary("08.2026", money)
	for _, want := range []string{
		"08.2026", "Доход: 200 000", "Расход: 32 000", "Отложено: 100 000",
		"Доля накоплений: 50%", "жильё — 30 000", "еда — 2 000",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("нет %q:\n%s", want, got)
		}
	}
	if strings.Index(got, "жильё") > strings.Index(got, "еда") {
		t.Fatalf("категории должны идти по убыванию:\n%s", got)
	}
}

func TestFinanceSummaryWithoutRecords(t *testing.T) {
	if got := financeSummary("08.2026", nil); !strings.Contains(got, "Записей за месяц нет") {
		t.Fatalf("неожиданный итог: %s", got)
	}
}
