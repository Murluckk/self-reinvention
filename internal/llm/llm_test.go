package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
	in := `{"day":{"wake":"07:30","algorithms":60,"system_design":45,"workout":true,"mood":8},
	        "money":[{"kind":"expense","amount":1200,"currency":"RUB","category":"еда","comment":"обед"}],
	        "note":"голова тяжёлая"}`
	v, err := Decode(in, "2026-08-24")
	if err != nil {
		t.Fatal(err)
	}
	if v.Day.Wake == nil || *v.Day.Wake != "07:30" {
		t.Fatalf("подъём: %v", v.Day.Wake)
	}
	if v.Day.Algorithms == nil || *v.Day.Algorithms != 60 {
		t.Fatalf("алгоритмы: %v", v.Day.Algorithms)
	}
	if v.Day.Workout == nil || !*v.Day.Workout {
		t.Fatalf("тренировка: %v", v.Day.Workout)
	}
	if len(v.Money) != 1 || v.Money[0].Kind != model.MoneyExpense || v.Money[0].Amount != 1200 {
		t.Fatalf("деньги: %+v", v.Money)
	}
	if v.Note != "голова тяжёлая" {
		t.Fatalf("заметка: %q", v.Note)
	}
	if v.Recogn != 6 {
		t.Fatalf("распознано %d, ожидалось 6", v.Recogn)
	}
}

// Модель иногда приписывает текст вокруг JSON и выдумывает ключи; ни то, ни
// другое не должно ломать разбор.
func TestDecodeToleratesNoiseAndUnknownKeys(t *testing.T) {
	in := "Вот результат:\n```json\n{\"day\":{\"algorithms\":30,\"настроение_души\":5,\"mood\":null,\"note\":\"\"}}\n```"
	v, err := Decode(in, "2026-08-24")
	if err != nil {
		t.Fatal(err)
	}
	if v.Day.Algorithms == nil || *v.Day.Algorithms != 30 {
		t.Fatalf("алгоритмы: %v", v.Day.Algorithms)
	}
	if v.Day.Mood != nil || v.Day.Note != nil {
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

// Проверяем провод к Ollama: путь, format=json и нулевую температуру. Ошибка в
// любом из них проявилась бы только на живом голосовом.
func TestParseCallsOllamaCorrectly(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"response":"{\"day\":{\"algorithms\":70,\"workout\":true}}"}`)
	}))
	defer srv.Close()

	v, err := New(srv.URL, "qwen2.5:7b-instruct").Parse(context.Background(), "алгоритмы 70 минут, тренировался", "2026-08-24")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/generate" {
		t.Fatalf("путь %q", gotPath)
	}
	if gotBody["model"] != "qwen2.5:7b-instruct" || gotBody["format"] != "json" {
		t.Fatalf("тело запроса: %v", gotBody)
	}
	if gotBody["stream"] != false {
		t.Fatalf("stream должен быть false: %v", gotBody["stream"])
	}
	if opts, ok := gotBody["options"].(map[string]any); !ok || opts["temperature"] != float64(0) {
		t.Fatalf("температура должна быть нулевой: %v", gotBody["options"])
	}
	if sys, _ := gotBody["system"].(string); !strings.Contains(sys, `"algorithms"`) {
		t.Fatal("в системный промпт не попала схема")
	}
	if v.Day.Algorithms == nil || *v.Day.Algorithms != 70 {
		t.Fatalf("алгоритмы: %v", v.Day.Algorithms)
	}
}

func TestParseCallsOpenAICompatibleAPI(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"{\"day\":{\"mood\":8}}"}}]}`)
	}))
	defer srv.Close()

	v, err := NewOpenAI(srv.URL, "sk-test", "gpt-test").Parse(
		context.Background(), "настроение восемь", "2026-08-24",
	)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/chat/completions" || gotAuth != "Bearer sk-test" {
		t.Fatalf("path=%q auth=%q", gotPath, gotAuth)
	}
	if gotBody["model"] != "gpt-test" {
		t.Fatalf("тело запроса: %v", gotBody)
	}
	messages, ok := gotBody["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("messages: %v", gotBody["messages"])
	}
	if v.Day.Mood == nil || *v.Day.Mood != 8 {
		t.Fatalf("настроение: %v", v.Day.Mood)
	}
}

func TestCompleteCallsOpenAIWithoutJSONMode(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"Краткий анализ"}}]}`)
	}))
	defer srv.Close()

	got, err := NewOpenAI(srv.URL, "sk-test", "gpt-test").Complete(
		context.Background(), "Системная инструкция", "Финансовые числа",
	)
	if err != nil || got != "Краткий анализ" {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if _, exists := gotBody["response_format"]; exists {
		t.Fatalf("текстовый анализ не должен запрашивать JSON: %v", gotBody)
	}
}

// Ollama не поднята — ошибка должна быть внятной, на неё завязан фолбэк.
func TestParseServiceDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	_, err := New(url, "m").Parse(context.Background(), "текст", "2026-08-24")
	if err == nil || !strings.Contains(err.Error(), "ollama недоступна") {
		t.Fatalf("ошибка %v", err)
	}
}

// Ollama отвечает 200, но с ошибкой в теле — например, модель не скачана.
func TestParseModelMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"error":"model 'qwen2.5:7b-instruct' not found, try pulling it first"}`)
	}))
	defer srv.Close()
	_, err := New(srv.URL, "qwen2.5:7b-instruct").Parse(context.Background(), "текст", "2026-08-24")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("ошибка %v", err)
	}
}
