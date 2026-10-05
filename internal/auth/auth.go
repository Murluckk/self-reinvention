// Package auth — пароли дашборда и защита от их подбора. Пароли хранятся
// только хэшем PBKDF2-SHA256: стандартная библиотека, без новых зависимостей.
package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	scheme     = "pbkdf2-sha256"
	iterations = 300_000
	keyLen     = 32
)

// Hash возвращает строку вида pbkdf2-sha256$итерации$соль$хэш.
func Hash(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, iterations, keyLen)
	if err != nil {
		return "", err
	}
	enc := base64.RawStdEncoding
	return fmt.Sprintf("%s$%d$%s$%s", scheme, iterations, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// Verify сравнивает пароль с хэшем за постоянное время.
func Verify(hash, password string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 4 || parts[0] != scheme {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1 {
		return false
	}
	enc := base64.RawStdEncoding
	salt, err1 := enc.DecodeString(parts[2])
	want, err2 := enc.DecodeString(parts[3])
	if err1 != nil || err2 != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iter, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// Generate создаёт пароль из 16 символов без похожих букв: его диктуют и
// переписывают с телефона.
func Generate() string {
	const alphabet = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKMNPQRSTUVWXYZ23456789"
	b := make([]byte, 16)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			panic(err) // crypto/rand не отказывает на поддерживаемых ОС
		}
		b[i] = alphabet[n.Int64()]
	}
	return string(b)
}

// Token — случайный идентификатор для ссылок-приглашений.
func Token() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// Limiter считает неудачные попытки входа и временно блокирует ключ (IP или
// логин), когда их становится слишком много. Состояние в памяти: после
// рестарта счётчики обнуляются, и это приемлемо для личного сервиса.
type Limiter struct {
	max    int
	window time.Duration
	now    func() time.Time

	mu    sync.Mutex
	fails map[string][]time.Time
}

// NewLimiter разрешает max неудач за window.
func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{max: max, window: window, now: time.Now, fails: map[string][]time.Time{}}
}

// Blocked сообщает, исчерпан ли лимит неудач для ключа.
func (l *Limiter) Blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.recent(key)) >= l.max
}

// Fail запоминает неудачную попытку.
func (l *Limiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fails[key] = append(l.recent(key), l.now())
}

// Reset забывает неудачи после успешного входа.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, key)
}

func (l *Limiter) recent(key string) []time.Time {
	cutoff := l.now().Add(-l.window)
	kept := l.fails[key][:0]
	for _, t := range l.fails[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.fails, key)
		return nil
	}
	l.fails[key] = kept
	return kept
}
