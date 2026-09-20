package config

import "testing"

func TestParseUsers(t *testing.T) {
	users, err := parseUsers(
		"949465743:owner:pass-one,908821693:friend:pass-two",
		User{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 2 || users[0].TelegramID != 949465743 ||
		users[1].DashboardUser != "friend" || users[1].DashboardPassword != "pass-two" {
		t.Fatalf("users: %+v", users)
	}
}

func TestParseUsersRejectsDuplicates(t *testing.T) {
	for _, raw := range []string{
		"1:a:x,1:b:y",
		"1:a:x,2:a:y",
		"bad:a:x",
		"1:missing",
	} {
		if _, err := parseUsers(raw, User{}); err == nil {
			t.Errorf("%q: ожидалась ошибка", raw)
		}
	}
}
