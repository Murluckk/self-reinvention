package asr

import (
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Проверяем именно провод: сервис ждёт multipart-поле "file", и любая опечатка
// здесь всплыла бы только на живом голосовом.
func TestTranscribeSendsMultipartFile(t *testing.T) {
	var gotField, gotBody, gotFilename string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Errorf("content-type: %v", err)
			return
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		part, err := mr.NextPart()
		if err != nil {
			t.Errorf("часть не прочиталась: %v", err)
			return
		}
		gotField, gotFilename = part.FormName(), part.FileName()
		b, _ := io.ReadAll(part)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"text":"спал семь часов","language":"ru","duration":4.2}`)
	}))
	defer srv.Close()

	text, err := New(srv.URL).Transcribe(context.Background(), []byte("RIFFfake"), "voice.wav")
	if err != nil {
		t.Fatal(err)
	}
	if gotField != "file" {
		t.Fatalf("поле %q, сервис ждёт \"file\"", gotField)
	}
	if gotFilename != "voice.wav" || gotBody != "RIFFfake" {
		t.Fatalf("имя %q, тело %q", gotFilename, gotBody)
	}
	if text != "спал семь часов" {
		t.Fatalf("расшифровка %q", text)
	}
}

func TestTranscribeErrors(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
		want    string
	}{
		{"статус", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "модель ещё грузится", http.StatusServiceUnavailable)
		}, "статус 503"},
		{"не json", func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, "<html>502</html>")
		}, "не JSON"},
		{"пустой текст", func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `{"text":""}`)
		}, "пустой текст"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(c.handler)
			defer srv.Close()
			_, err := New(srv.URL).Transcribe(context.Background(), []byte("x"), "voice.wav")
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("ошибка %v, ожидалось упоминание %q", err, c.want)
			}
		})
	}
}

// Если сервис не поднят, ошибка должна быть внятной: на неё завязан фолбэк.
func TestTranscribeServiceDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	_, err := New(url).Transcribe(context.Background(), []byte("x"), "voice.wav")
	if err == nil || !strings.Contains(err.Error(), "asr недоступен") {
		t.Fatalf("ошибка %v", err)
	}
}
