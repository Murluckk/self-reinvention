package bot

import (
	"strings"
	"testing"

	"github.com/murluckk/self-reinvention/internal/model"
)

// Шпаргалка строится из реестра полей, поэтому отстать от парсера она не может —
// но проверим, что реестр действительно в неё попадает.
func TestHelpListsEveryKey(t *testing.T) {
	for _, f := range model.Fields() {
		if !strings.Contains(helpText, f.Keys[0]) {
			t.Fatalf("в /help нет ключа %q", f.Keys[0])
		}
	}
	for _, want := range []string{"/d", "/m", "/s", "/w", "/undo", "/help", "23:00", "5-го", "20-го"} {
		if !strings.Contains(helpText, want) {
			t.Fatalf("в /help нет %q", want)
		}
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
