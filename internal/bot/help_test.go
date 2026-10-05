package bot

import (
	"strings"
	"testing"

	"github.com/murluckk/self-reinvention/internal/model"
	"github.com/murluckk/self-reinvention/internal/parse"
)

func presetUser(name string) *model.User {
	p, _ := model.PresetByName(name)
	return &model.User{Name: name, Fields: p.Fields, Finance: p.Finance, Timezone: p.Timezone}
}

// Шпаргалка строится из реестра полей, поэтому отстать от парсера она не может —
// но проверим, что реестр действительно в неё попадает.
func TestHelpListsEveryKey(t *testing.T) {
	for _, p := range model.Presets {
		u := presetUser(p.Name)
		text := helpText(u, false)
		for _, f := range u.Fields.Fields() {
			if !strings.Contains(text, f.Keys[0]) {
				t.Fatalf("в /help профиля %s нет ключа %q", p.Name, f.Keys[0])
			}
		}
		if strings.Contains(text, "/invite") {
			t.Fatalf("админские команды видны обычному пользователю %s", p.Name)
		}
	}
	pasha := helpText(presetUser("pasha"), true)
	for _, want := range []string{"/d", "/c", "/m", "/s", "/w", "/i", "/undo", "/fields", "/tz", "5-го", "20-го", "/invite"} {
		if !strings.Contains(pasha, want) {
			t.Fatalf("в /help нет %q", want)
		}
	}
	sveta := helpText(presetUser("sveta"), false)
	for _, want := range []string{"прогулка", "учеба", "полезное", "сладкое", "алкоголь"} {
		if !strings.Contains(sveta, want) {
			t.Fatalf("в /help Светы нет %q", want)
		}
	}
	if strings.Contains(sveta, "алго") || strings.Contains(sveta, "/m — деньги") {
		t.Fatal("в /help Светы попали поля или финансы Паши")
	}
}

// Пример /d из шпаргалки должен разбираться без ошибок, а текстовое поле —
// стоять последним, иначе оно проглотит остальные ключи.
func TestHelpExampleParses(t *testing.T) {
	for _, p := range model.Presets {
		example := exampleFor(p.Fields.Fields())
		res := parse.ParseDayIn(example, "2026-10-05", p.Fields)
		if len(res.Errors) > 0 {
			t.Fatalf("%s: пример %q не разбирается: %v", p.Name, example, res.Errors)
		}
		if missing := res.Day.MissingIn(p.Fields); len(missing) > 0 {
			t.Fatalf("%s: пример %q не заполняет %v", p.Name, example, missing[0].Label)
		}
	}
}

func TestFieldKeyboardValuesParse(t *testing.T) {
	for _, f := range model.Fields() {
		for _, row := range fieldOptions(&f) {
			for _, o := range row {
				d := &model.Day{Date: "2026-10-05"}
				if err := parse.SetValue(d, &f, o.value); err != nil {
					t.Fatalf("%s: кнопка %q даёт ошибку %v", f.DB, o.label, err)
				}
				if len("c:2026-10-05:"+f.DB+":"+o.value) > 64 {
					t.Fatalf("%s: callback длиннее 64 байт", f.DB)
				}
			}
		}
	}
}

func TestParseZone(t *testing.T) {
	cases := map[string]string{
		"+3":            "Etc/GMT-3",
		"UTC+5":         "Etc/GMT-5",
		"gmt-2":         "Etc/GMT+2",
		"0":             "",
		"Europe/Berlin": "Europe/Berlin",
	}
	for in, want := range cases {
		got, err := parseZone(in)
		if want == "" {
			if err == nil {
				t.Errorf("%q: ожидалась ошибка, получили %q", in, got)
			}
			continue
		}
		if err != nil || got != want {
			t.Errorf("%q -> %q (%v), ожидалось %q", in, got, err, want)
		}
	}
	if _, err := parseZone("Mars/Olympus"); err == nil {
		t.Error("ожидалась ошибка на несуществующий пояс")
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
