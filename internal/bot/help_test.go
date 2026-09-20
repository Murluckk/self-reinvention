package bot

import (
	"strings"
	"testing"

	"github.com/murluckk/self-reinvention/internal/config"
	"github.com/murluckk/self-reinvention/internal/model"
)

// Шпаргалка строится из реестра полей, поэтому отстать от парсера она не может —
// но проверим, что реестр действительно в неё попадает.
func TestHelpListsEveryKey(t *testing.T) {
	for _, profile := range []string{config.ProfilePasha, config.ProfileSveta} {
		text := helpTextFor(profile)
		for _, f := range model.FieldsFor(profile) {
			if !strings.Contains(text, f.Keys[0]) {
				t.Fatalf("в /help профиля %s нет ключа %q", profile, f.Keys[0])
			}
		}
	}
	for _, want := range []string{"/d", "/m", "/s", "/w", "/undo", "/help", "23:00", "5-го", "20-го"} {
		if !strings.Contains(helpText, want) {
			t.Fatalf("в /help нет %q", want)
		}
	}
	sveta := helpTextFor(config.ProfileSveta)
	for _, want := range []string{"прогулка", "учеба", "полезное", "сладкое", "алкоголь"} {
		if !strings.Contains(sveta, want) {
			t.Fatalf("в /help Светы нет %q", want)
		}
	}
	if strings.Contains(sveta, "алго") || strings.Contains(sveta, "/m — деньги") {
		t.Fatal("в /help Светы попали поля или финансы Паши")
	}
}

func TestSplitCommand(t *testing.T) {
	cases := []struct{ in, cmd, args string }{
		{"/s", "s", ""},
		{"/d сон 7", "d", "сон 7"},
		{"/w@my_tracker_bot 30", "w", "30"},
		{"/UNDO", "undo", ""},
	}
	for _, c := range cases {
		cmd, args := splitCommand(c.in)
		if cmd != c.cmd || args != c.args {
			t.Fatalf("%q -> %q / %q, ожидалось %q / %q", c.in, cmd, args, c.cmd, c.args)
		}
	}
}

func TestCloudAudioNameUsesSupportedOggExtension(t *testing.T) {
	cases := map[string]string{
		"voice/file_123.oga": "file_123.ogg",
		"voice/file_123.ogg": "file_123.ogg",
		"":                   "voice.ogg",
	}
	for in, want := range cases {
		if got := cloudAudioName(in); got != want {
			t.Errorf("%q -> %q, ожидалось %q", in, got, want)
		}
	}
}
