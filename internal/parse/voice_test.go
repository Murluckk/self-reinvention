package parse

import "testing"

func TestVoiceRegexTypicalPhrase(t *testing.T) {
	v := ParseVoiceRegex(
		"Встал в 7:30, заснул в 23:40, алгоритмы 60 минут, системный дизайн 45 минут, был в зале, состояние 8",
		today,
	)
	mustStr(t, v.Day.Wake, "07:30")
	mustStr(t, v.Day.Bed, "23:40")
	mustInt(t, v.Day.Algorithms, 60)
	mustInt(t, v.Day.SystemDesign, 45)
	mustBool(t, v.Day.Workout, true)
	mustInt(t, v.Day.Mood, 8)
}

func TestVoiceRegexNegations(t *testing.T) {
	v := ParseVoiceRegex("алгоритмами не занимался, системы нет, без тренировки", today)
	mustInt(t, v.Day.Algorithms, 0)
	mustInt(t, v.Day.SystemDesign, 0)
	mustBool(t, v.Day.Workout, false)
}

func TestVoiceRegexMoney(t *testing.T) {
	v := ParseVoiceRegex("потратил 1200 на еду и отложил 50000", today)
	if len(v.Money) != 2 {
		t.Fatalf("получено %d денежных записей: %+v", len(v.Money), v.Money)
	}
}

func TestVoiceRegexUnknownPhraseFallsThrough(t *testing.T) {
	v := ParseVoiceRegex("сегодня было довольно странно, но в целом норм", today)
	if v.Recogn >= 2 {
		t.Fatalf("не должен был ничего распознать, получил %+v", v.Day)
	}
}
