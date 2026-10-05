package auth

import (
	"strings"
	"testing"
	"time"
)

func TestHashVerify(t *testing.T) {
	h, err := Hash("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(h, "correct horse") {
		t.Fatal("пароль попал в хэш открытым текстом")
	}
	if !Verify(h, "correct horse") || Verify(h, "wrong") || Verify("garbage", "correct horse") {
		t.Fatal("проверка пароля работает неверно")
	}
	h2, _ := Hash("correct horse")
	if h == h2 {
		t.Fatal("соль не используется: одинаковые пароли дали одинаковый хэш")
	}
}

func TestGenerateIsRandomAndReadable(t *testing.T) {
	a, b := Generate(), Generate()
	if len(a) != 16 || a == b || strings.ContainsAny(a, "0O1lI") {
		t.Fatalf("плохие пароли: %q %q", a, b)
	}
}

func TestLimiterWindow(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	l := NewLimiter(3, 15*time.Minute)
	l.now = func() time.Time { return now }
	for i := 0; i < 3; i++ {
		if l.Blocked("ip") {
			t.Fatalf("заблокирован после %d попыток", i)
		}
		l.Fail("ip")
	}
	if !l.Blocked("ip") || l.Blocked("other") {
		t.Fatal("лимит не сработал или задел чужой ключ")
	}
	now = now.Add(16 * time.Minute)
	if l.Blocked("ip") {
		t.Fatal("блокировка не снялась после окна")
	}
}
