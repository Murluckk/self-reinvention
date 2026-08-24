// Package asr — клиент к локальному сервису распознавания речи. Сам ML живёт в
// отдельном питоновском процессе (faster-whisper + FastAPI), Go-бот только
// отправляет туда wav и получает текст.
package asr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

// Client — HTTP-клиент ASR-сервиса.
type Client struct {
	url  string
	http *http.Client
}

// New создаёт клиента. Таймаут щедрый: распознавание минутного голосового на
// CPU занимает секунды, но на холодном старте модель ещё грузится.
func New(url string) *Client {
	return &Client{url: url, http: &http.Client{Timeout: 5 * time.Minute}}
}

type response struct {
	Text     string  `json:"text"`
	Language string  `json:"language"`
	Duration float64 `json:"duration"`
}

// Transcribe отправляет wav в сервис и возвращает расшифровку.
func (c *Client) Transcribe(ctx context.Context, wav []byte, filename string) (string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(wav); err != nil {
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
