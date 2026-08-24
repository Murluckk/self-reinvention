package parse

import (
	"testing"

	"github.com/murluckk/self-reinvention/internal/model"
)

func TestParseMoneyIncome(t *testing.T) {
	m, err := ParseMoney("+250000 зп", today)
	if err != nil {
		t.Fatal(err)
	}
	if m.Kind != model.MoneyIncome || m.Amount != 250000 || m.Currency != "RUB" || m.Category != "зп" {
		t.Fatalf("получено %+v", m)
	}
	if m.Date != today {
		t.Fatalf("дата %q", m.Date)
	}
}

func TestParseMoneyExpenseWithComment(t *testing.T) {
	m, err := ParseMoney("-1200 еда обед в кафе", today)
	if err != nil {
		t.Fatal(err)
	}
	if m.Kind != model.MoneyExpense || m.Amount != 1200 {
		t.Fatalf("получено %+v", m)
	}
	if m.Category != "еда" || m.Comment != "обед в кафе" {
		t.Fatalf("категория %q, комментарий %q", m.Category, m.Comment)
	}
}

func TestParseMoneySavingWithCurrency(t *testing.T) {
	m, err := ParseMoney("=5000$ холодный кошелёк", today)
	if err != nil {
		t.Fatal(err)
	}
	if m.Kind != model.MoneySaving || m.Currency != "USD" || m.Amount != 5000 {
		t.Fatalf("получено %+v", m)
	}
	if m.Category != "холодный" || m.Comment != "кошелёк" {
		t.Fatalf("категория %q, комментарий %q", m.Category, m.Comment)
	}
}

func TestParseMoneyDecimalAndDate(t *testing.T) {
	m, err := ParseMoney("2026-08-20 -1200,50 такси", today)
	if err != nil {
		t.Fatal(err)
	}
	if m.Date != "2026-08-20" || m.Amount != 1200.5 {
		t.Fatalf("получено %+v", m)
	}
}

func TestParseMoneyErrors(t *testing.T) {
	for _, in := range []string{"", "250000 зп", "+250000", "+0 зп", "-100¥ еда"} {
		if _, err := ParseMoney(in, today); err == nil {
			t.Fatalf("ожидал ошибку на %q", in)
		}
	}
}

func TestFormatAmount(t *testing.T) {
	cases := map[float64]string{
		250000:  "250 000",
		1200.5:  "1 200.5",
		999:     "999",
		1000000: "1 000 000",
	}
	for in, want := range cases {
		if got := FormatAmount(in); got != want {
			t.Fatalf("%v -> %q, ожидалось %q", in, got, want)
		}
	}
}

func TestParseNote(t *testing.T) {
	tag, body := ParseNote("#идея сделать трекер сна")
	if tag != "идея" || body != "сделать трекер сна" {
		t.Fatalf("получено %q / %q", tag, body)
	}
	tag, body = ParseNote("просто мысль")
	if tag != "" || body != "просто мысль" {
		t.Fatalf("получено %q / %q", tag, body)
	}
}

func TestParseMoneyWithdrawalFromSavings(t *testing.T) {
	m, err := ParseMoney("=-30000 накопления на ремонт", today)
	if err != nil {
		t.Fatal(err)
	}
	if m.Kind != model.MoneySaving || m.Amount != -30000 {
		t.Fatalf("получено %+v", m)
	}
}

func TestParseMoneyRejectsZero(t *testing.T) {
	if _, err := ParseMoney("=0 накопления", today); err == nil {
		t.Fatal("ожидалась ошибка на нулевой сумме")
	}
}
