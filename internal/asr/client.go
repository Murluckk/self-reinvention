// Package asr — клиенты облачного Transcriptions API и локального Whisper.
package asr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

// Client — HTTP-клиент ASR-сервиса.
type Client struct {
	url         string
	apiKey      string
	model       string
	prompt      string
	directAudio bool
	http        *http.Client
}

// New создаёт клиента. Таймаут щедрый: распознавание минутного голосового на
// CPU занимает секунды, но на холодном старте модель ещё грузится.
func New(url string) *Client {
	return &Client{url: url, http: &http.Client{Timeout: 5 * time.Minute}}
}

// NewOpenAI создаёт клиент OpenAI-compatible Transcriptions API. Он принимает
// исходный OGG/OGA из Telegram без промежуточного ffmpeg.
func NewOpenAI(baseURL, apiKey, model, prompt string) *Client {
	return &Client{
		url:         strings.TrimRight(baseURL, "/") + "/audio/transcriptions",
		apiKey:      apiKey,
		model:       model,
		prompt:      prompt,
		directAudio: true,
		http:        &http.Client{Timeout: 5 * time.Minute},
	}
}

// DirectAudio сообщает, можно ли отправить исходный Telegram-файл без WAV.
func (c *Client) DirectAudio() bool { return c.directAudio }

type response struct {
	Text     string  `json:"text"`
	Language string  `json:"language"`
	Duration float64 `json:"duration"`
}

// Transcribe отправляет аудиофайл в сервис и возвращает расшифровку.
func (c *Client) Transcribe(ctx context.Context, audio []byte, filename string) (string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if c.directAudio {
		_ = w.WriteField("model", c.model)
		_ = w.WriteField("language", "ru")
		if c.prompt != "" {
			_ = w.WriteField("prompt", c.prompt)
		}
	}
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(audio); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("asr недоступен: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("asr вернул статус %d: %s", resp.StatusCode, string(raw))
	}
	var r response
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", fmt.Errorf("asr вернул не JSON: %w", err)
	}
	if r.Text == "" {
		return "", fmt.Errorf("asr вернул пустой текст")
	}
	return r.Text, nil
}
