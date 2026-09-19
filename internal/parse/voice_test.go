package parse

import "testing"

func TestVoiceRegexTypicalPhrase(t *testing.T) {
	v := ParseVoiceRegex("Спал 7 часов, встал в 7:30, был в зале, английский 40 минут, день чистый", today)
	mustFloat(t, v.Day.Sleep, 7)
	mustStr(t, v.Day.Wake, "07:30")
	mustStr(t, v.Day.Workout, "зал")
	mustInt(t, v.Day.English, 40)
	mustBool(t, v.Day.Clean, true)
	if v.Recogn < 5 {
		t.Fatalf("распознано %d полей, ожидалось минимум 5", v.Recogn)
	}
}

func TestVoiceRegexNegations(t *testing.T) {
	v := ParseVoiceRegex("не готовил, без тренировки, сорвался вечером", today)
	mustBool(t, v.Day.Cooked, false)
	mustStr(t, v.Day.Workout, "нет")
	mustBool(t, v.Day.Clean, false)
}

func TestVoiceRegexNumbers(t *testing.T) {
	v := ParseVoiceRegex("вес 73.4, работал 8 часов, тг 3 раза", today)
	mustFloat(t, v.Day.Weight, 73.4)
	mustFloat(t, v.Day.Work, 8)
	mustInt(t, v.Day.Telegram, 3)
}

func TestVoiceRegexScales(t *testing.T) {
	v := ParseVoiceRegex("фокус 10, настроение 8, энергия 6", today)
	mustInt(t, v.Day.Focus, 10)
	mustInt(t, v.Day.Mood, 8)
	mustInt(t, v.Day.Energy, 6)
}

func TestVoiceRegexMoney(t *testing.T) {
	v := ParseVoiceRegex("потратил 1200 на еду и отложил 50000", today)
	if len(v.Money) != 2 {
		t.Fatalf("получено %d денежных записей: %+v", len(v.Money), v.Money)
	}
}

func TestVoiceRegexWorkBeatsDayOff(t *testing.T) {
	// «отработал смену в выходной» не должно превратиться в полный выходной
	v := ParseVoiceRegex("выходной, но отработал смену 6 часов", today)
	mustBool(t, v.Day.DayOff, false)
	mustBool(t, v.Day.Shift, true)
	mustFloat(t, v.Day.Work, 6)
}

func TestVoiceRegexUnknownPhraseFallsThrough(t *testing.T) {
	v := ParseVoiceRegex("сегодня было довольно странно, но в целом норм", today)
	if v.Recogn >= 2 {
		t.Fatalf("не должен был ничего распознать, получил %+v", v.Day)
	}
}
