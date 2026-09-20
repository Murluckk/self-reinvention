package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/murluckk/self-reinvention/internal/model"
	"github.com/murluckk/self-reinvention/internal/parse"
)

// Client — клиент Ollama.
type Client struct {
	url    string
	model  string
	apiKey string
	openAI bool
	http   *http.Client
}

// NewOpenAI создаёт клиент OpenAI-compatible Chat Completions API.
func NewOpenAI(baseURL, apiKey, model string) *Client {
	return &Client{
		url: strings.TrimRight(baseURL, "/"), model: model,
		apiKey: apiKey, openAI: true, http: &http.Client{Timeout: 3 * time.Minute},
	}
}

// New создаёт клиента к Ollama.
func New(url, ollamaModel string) *Client {
	return &Client{
		url:   strings.TrimRight(url, "/"),
		model: ollamaModel,
		http:  &http.Client{Timeout: 3 * time.Minute},
	}
}

type generateRequest struct {
	Model   string         `json:"model"`
	Prompt  string         `json:"prompt"`
	System  string         `json:"system"`
	Format  string         `json:"format"`
	Stream  bool           `json:"stream"`
	Options map[string]any `json:"options"`
}

type generateResponse struct {
	Response string `json:"response"`
	Error    string `json:"error"`
}

type rawResult struct {
	Day   map[string]any   `json:"day"`
	Money []map[string]any `json:"money"`
	Note  string           `json:"note"`
}

// Parse отдаёт фразу модели и раскладывает ответ по структурам. Температура
// нулевая, format=json — от модели нужна предсказуемость, а не фантазия.
func (c *Client) Parse(ctx context.Context, text, date string) (*parse.Voice, error) {
	if c.openAI {
		return c.parseOpenAI(ctx, text, date)
	}
	body, err := json.Marshal(generateRequest{
		Model:   c.model,
		Prompt:  text,
		System:  SystemPrompt(),
		Format:  "json",
		Stream:  false,
		Options: map[string]any{"temperature": 0},
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama недоступна: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama вернула статус %d: %s", resp.StatusCode, string(raw))
	}
	var gr generateResponse
	if err := json.Unmarshal(raw, &gr); err != nil {
		return nil, fmt.Errorf("ollama вернула не JSON: %w", err)
	}
	if gr.Error != "" {
		return nil, fmt.Errorf("ollama: %s", gr.Error)
	}
	return Decode(gr.Response, date)
}

type chatRequest struct {
	Model          string         `json:"model"`
	Messages       []chatMessage  `json:"messages"`
	ResponseFormat map[string]any `json:"response_format,omitempty"`
}

// Complete выполняет обычный текстовый запрос. Он нужен для аналитики, где
// структурированный JSON не требуется.
func (c *Client) Complete(ctx context.Context, system, prompt string) (string, error) {
	if c.openAI {
		body, err := json.Marshal(chatRequest{
			Model: c.model,
			Messages: []chatMessage{
				{Role: "system", Content: system},
				{Role: "user", Content: prompt},
			},
		})
		if err != nil {
			return "", err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/chat/completions", bytes.NewReader(body))
		if err != nil {
			return "", err
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.http.Do(req)
		if err != nil {
			return "", fmt.Errorf("llm api недоступен: %w", err)
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		if err != nil {
			return "", err
		}
		var out chatResponse
		if err := json.Unmarshal(raw, &out); err != nil {
			return "", fmt.Errorf("llm api вернул не JSON (%d): %w", resp.StatusCode, err)
		}
		if resp.StatusCode != http.StatusOK {
			if out.Error != nil && out.Error.Message != "" {
				return "", fmt.Errorf("llm api: %s", out.Error.Message)
			}
			return "", fmt.Errorf("llm api вернул статус %d", resp.StatusCode)
		}
		if len(out.Choices) == 0 || strings.TrimSpace(out.Choices[0].Message.Content) == "" {
			return "", fmt.Errorf("llm api вернул пустой ответ")
		}
		return strings.TrimSpace(out.Choices[0].Message.Content), nil
	}

	body, err := json.Marshal(generateRequest{
		Model: c.model, Prompt: prompt, System: system, Stream: false,
		Options: map[string]any{"temperature": 0.2},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama недоступна: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	var out generateResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK || out.Error != "" {
		return "", fmt.Errorf("ollama вернула статус %d: %s", resp.StatusCode, out.Error)
	}
	return strings.TrimSpace(out.Response), nil
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *Client) parseOpenAI(ctx context.Context, text, date string) (*parse.Voice, error) {
	body, err := json.Marshal(chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: SystemPrompt()},
			{Role: "user", Content: text},
		},
		ResponseFormat: map[string]any{"type": "json_object"},
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llm api недоступен: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	var out chatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("llm api вернул не JSON (%d): %w", resp.StatusCode, err)
	}
	if resp.StatusCode != http.StatusOK {
		if out.Error != nil && out.Error.Message != "" {
			return nil, fmt.Errorf("llm api: %s", out.Error.Message)
		}
		return nil, fmt.Errorf("llm api вернул статус %d: %s", resp.StatusCode, string(raw))
	}
	if len(out.Choices) == 0 || strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		return nil, fmt.Errorf("llm api вернул пустой ответ")
	}
	return Decode(out.Choices[0].Message.Content, date)
}

// Decode превращает JSON от модели в разобранную запись. Мусорные поля молча
// пропускаются: модель может выдумать ключ, но испортить этим данные она не должна.
func Decode(s, date string) (*parse.Voice, error) {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "{"); i > 0 {
		s = s[i:] // на случай, если модель всё же приписала что-то спереди
	}
	if i := strings.LastIndex(s, "}"); i >= 0 {
		s = s[:i+1]
	}
	var r rawResult
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		return nil, fmt.Errorf("модель вернула не по схеме: %w", err)
	}
	v := &parse.Voice{Day: &model.Day{Date: date}, Note: strings.TrimSpace(r.Note), Level: "llm"}
	for k, val := range r.Day {
		if val == nil {
			continue
		}
		f, ok := model.FieldByColumn(k)
		if !ok {
			continue
		}
		if s, isStr := val.(string); isStr && strings.TrimSpace(s) == "" {
			continue
		}
		_ = f.SetAny(v.Day, val)
	}
	for _, m := range r.Money {
		mm := decodeMoney(m, date)
		if mm != nil {
			v.Money = append(v.Money, mm)
		}
	}
	v.Recogn = v.Count()
	return v, nil
}

func decodeMoney(m map[string]any, date string) *model.Money {
	amount, ok := numeric(m["amount"])
	if !ok || amount <= 0 {
		return nil
	}
	kind := model.MoneySaving
	switch strings.ToLower(fmt.Sprint(m["kind"])) {
	case "income", "доход", "+":
		kind = model.MoneyIncome
	case "expense", "расход", "-":
		kind = model.MoneyExpense
	case "saving", "savings", "отложено", "=":
		kind = model.MoneySaving
	default:
		return nil
	}
	cur := strings.ToUpper(strings.TrimSpace(str(m["currency"])))
	if cur == "" {
		cur = "RUB"
	}
	cat := strings.TrimSpace(str(m["category"]))
	if cat == "" {
		cat = kind.Label()
	}
	return &model.Money{
		Date: date, Kind: kind, Amount: amount, Currency: cur,
		Category: cat, Comment: strings.TrimSpace(str(m["comment"])),
	}
}

func numeric(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case string:
		f, err := model.ParseFloat(x)
		return f, err == nil
	}
	return 0, false
}

func str(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}
