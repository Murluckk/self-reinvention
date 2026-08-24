package llm

import (
	"strings"
	"testing"

	"github.com/murluckk/self-reinvention/internal/model"
)

func TestSchemaCoversEveryField(t *testing.T) {
	sch := Schema()
	for _, f := range model.Fields() {
		if !strings.Contains(sch, `"`+f.DB+`"`) {
			t.Fatalf("в схеме нет поля %q — модель о нём не узнает", f.DB)
		}
	}
	if !strings.Contains(sch, `"money"`) || !strings.Contains(sch, `"note"`) {
		t.Fatal("в схеме должны быть деньги и заметка")
	}
}

func TestDecodeHappyPath(t *testing.T) {
	in := `{"day":{"sleep":7.5,"wake":"07:30","workout":"зал","english":40,"clean":true,"focus":4},
	        "money":[{"kind":"expense","amount":1200,"currency":"RUB","category":"еда","comment":"обед"}],
	        "note":"голова тяжёлая"}`
	v, err := Decode(in, "2026-08-24")
	if err != nil {
		t.Fatal(err)
	}
	if v.Day.Sleep == nil || *v.Day.Sleep != 7.5 {
		t.Fatalf("сон: %v", v.Day.Sleep)
	}
	if v.Day.Wake == nil || *v.Day.Wake != "07:30" {
		t.Fatalf("подъём: %v", v.Day.Wake)
	}
	if v.Day.English == nil || *v.Day.English != 40 {
		t.Fatalf("английский: %v", v.Day.English)
	}
	if v.Day.Clean == nil || !*v.Day.Clean {
		t.Fatalf("чисто: %v", v.Day.Clean)
	}
	if len(v.Money) != 1 || v.Money[0].Kind != model.MoneyExpense || v.Money[0].Amount != 1200 {
		t.Fatalf("деньги: %+v", v.Money)
	}
	if v.Note != "голова тяжёлая" {
		t.Fatalf("заметка: %q", v.Note)
	}
	if v.Recogn != 7 {
		t.Fatalf("распознано %d, ожидалось 7", v.Recogn)
	}
}

// Модель иногда приписывает текст вокруг JSON и выдумывает ключи; ни то, ни
// другое не должно ломать разбор.
func TestDecodeToleratesNoiseAndUnknownKeys(t *testing.T) {
	in := "Вот результат:\n```json\n{\"day\":{\"sleep\":8,\"настроение_души\":5,\"weight\":null,\"workout\":\"\"}}\n```"
	v, err := Decode(in, "2026-08-24")
	if err != nil {
		t.Fatal(err)
	}
	if v.Day.Sleep == nil || *v.Day.Sleep != 8 {
		t.Fatalf("сон: %v", v.Day.Sleep)
	}
	if v.Day.Weight != nil || v.Day.Workout != nil {
		t.Fatal("null и пустая строка не должны превращаться в значения")
	}
}

func TestDecodeRejectsGarbage(t *testing.T) {
	if _, err := Decode("это не json вовсе", "2026-08-24"); err == nil {
		t.Fatal("ожидалась ошибка")
	}
}

func TestDecodeSkipsBrokenMoney(t *testing.T) {
	in := `{"money":[{"kind":"expense","amount":0},{"kind":"чепуха","amount":100},{"kind":"saving","amount":5000}]}`
	v, err := Decode(in, "2026-08-24")
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Money) != 1 || v.Money[0].Kind != model.MoneySaving {
		t.Fatalf("деньги: %+v", v.Money)
	}
	if v.Money[0].Currency != "RUB" || v.Money[0].Category != "отложено" {
		t.Fatalf("умолчания не проставились: %+v", v.Money[0])
	}
}
