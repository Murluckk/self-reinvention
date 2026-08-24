package tg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"time"
)

// Client — клиент Bot API.
type Client struct {
	token string
	api   *http.Client
	poll  *http.Client
	base  string
	fileB string
}

// New создаёт клиента. Для long polling используется отдельный http-клиент с
// большим таймаутом: обычные запросы не должны его ждать.
func New(token string) *Client {
	return &Client{
		token: token,
		api:   &http.Client{Timeout: 60 * time.Second},
		poll:  &http.Client{Timeout: 120 * time.Second},
		base:  "https://api.telegram.org/bot" + token + "/",
		fileB: "https://api.telegram.org/file/bot" + token + "/",
	}
}

type apiResponse struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	Description string          `json:"description"`
	ErrorCode   int             `json:"error_code"`
}

func (c *Client) call(ctx context.Context, cl *http.Client, method string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+method, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := cl.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	defer resp.Body.Close()
	return decode(resp, method, out)
}

func decode(resp *http.Response, method string, out any) error {
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	var ar apiResponse
	if err := json.Unmarshal(raw, &ar); err != nil {
		return fmt.Errorf("%s: не разобрал ответ (%d): %s", method, resp.StatusCode, truncate(string(raw), 200))
	}
	if !ar.OK {
		return fmt.Errorf("%s: telegram вернул ошибку %d: %s", method, ar.ErrorCode, ar.Description)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(ar.Result, out)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// GetUpdates забирает обновления long polling'ом.
func (c *Client) GetUpdates(ctx context.Context, offset int64, timeout int) ([]Update, error) {
	payload := map[string]any{
		"offset":          offset,
		"timeout":         timeout,
		"allowed_updates": []string{"message", "callback_query"},
	}
	var out []Update
	if err := c.call(ctx, c.poll, "getUpdates", payload, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SendOptions — необязательные параметры отправки.
type SendOptions struct {
	Markup    *InlineKeyboardMarkup
	ParseMode string
}

// SendMessage отправляет текст. Текст режется на части: Telegram не принимает
// сообщения длиннее 4096 символов, а недельная сводка бывает длинной.
func (c *Client) SendMessage(ctx context.Context, chatID int64, text string, opt *SendOptions) (*Message, error) {
	chunks := split(text, 4000)
	var last *Message
	for i, chunk := range chunks {
		payload := map[string]any{"chat_id": chatID, "text": chunk}
		if opt != nil {
			if opt.ParseMode != "" {
				payload["parse_mode"] = opt.ParseMode
			}
			if opt.Markup != nil && i == len(chunks)-1 {
				payload["reply_markup"] = opt.Markup
			}
		}
		var m Message
		if err := c.call(ctx, c.api, "sendMessage", payload, &m); err != nil {
			return nil, err
		}
		last = &m
	}
	return last, nil
}

// EditMessageText переписывает уже отправленное сообщение — так ответ на
// нажатие кнопки не плодит новых сообщений в чате.
func (c *Client) EditMessageText(ctx context.Context, chatID, messageID int64, text string, markup *InlineKeyboardMarkup) error {
	if r := []rune(text); len(r) > 4000 {
		text = string(r[:3980]) + "\n…(обрезано)"
	}
	payload := map[string]any{"chat_id": chatID, "message_id": messageID, "text": text}
	if markup != nil {
		payload["reply_markup"] = markup
	} else {
		payload["reply_markup"] = InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{}}
	}
	return c.call(ctx, c.api, "editMessageText", payload, nil)
}

// AnswerCallbackQuery гасит «часики» на нажатой кнопке.
func (c *Client) AnswerCallbackQuery(ctx context.Context, id, text string) error {
	return c.call(ctx, c.api, "answerCallbackQuery", map[string]any{"callback_query_id": id, "text": text}, nil)
}

// GetMe возвращает информацию о самом боте — заодно это проверка токена.
func (c *Client) GetMe(ctx context.Context) (*User, error) {
	var u User
	if err := c.call(ctx, c.api, "getMe", map[string]any{}, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// GetFile узнаёт путь к файлу на серверах Telegram.
func (c *Client) GetFile(ctx context.Context, fileID string) (*File, error) {
	var f File
	if err := c.call(ctx, c.api, "getFile", map[string]any{"file_id": fileID}, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// Download скачивает файл по пути, полученному из GetFile.
func (c *Client) Download(ctx context.Context, filePath string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.fileB+filePath, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.api.Do(req)
	if err != nil {
		return nil, fmt.Errorf("скачивание файла: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("скачивание файла: статус %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}

// SendDocument отправляет файл как документ.
func (c *Client) SendDocument(ctx context.Context, chatID int64, filename string, content []byte, caption string) error {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("chat_id", strconv.FormatInt(chatID, 10))
	if caption != "" {
		_ = w.WriteField("caption", truncate(caption, 1000))
	}
	part, err := w.CreateFormFile("document", filename)
	if err != nil {
		return err
	}
	if _, err := part.Write(content); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"sendDocument", &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := c.api.Do(req)
	if err != nil {
		return fmt.Errorf("sendDocument: %w", err)
	}
	defer resp.Body.Close()
	return decode(resp, "sendDocument", nil)
}

// SetMyCommands показывает список команд в меню Telegram.
func (c *Client) SetMyCommands(ctx context.Context, cmds map[string]string) error {
	type cmd struct {
		Command     string `json:"command"`
		Description string `json:"description"`
	}
	// Порядок в меню фиксируем, чтобы он не прыгал между запусками.
	order := []string{"d", "m", "s", "w", "undo", "help"}
	var list []cmd
	for _, k := range order {
		if v, ok := cmds[k]; ok {
			list = append(list, cmd{Command: k, Description: v})
		}
	}
	return c.call(ctx, c.api, "setMyCommands", map[string]any{"commands": list}, nil)
}

// split режет длинный текст по границам строк.
func split(s string, limit int) []string {
	if len([]rune(s)) <= limit {
		return []string{s}
	}
	var out []string
	var cur []rune
	for _, line := range splitLines(s) {
		lr := []rune(line)
		if len(cur)+len(lr)+1 > limit && len(cur) > 0 {
			out = append(out, string(cur))
			cur = nil
		}
		if len(lr) > limit {
			for len(lr) > limit {
				out = append(out, string(lr[:limit]))
				lr = lr[limit:]
			}
		}
		cur = append(cur, lr...)
		cur = append(cur, '\n')
	}
	if len(cur) > 0 {
		out = append(out, string(cur))
	}
	return out
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}
