package model

import "testing"

func TestMergeMissingDoesNotOverwriteExistingFields(t *testing.T) {
	dst := &Day{Date: "2026-09-21"}
	src := &Day{Date: "2026-09-21"}

	workout, _ := FieldByColumn("workout")
	mood, _ := FieldByColumn("mood")
	if err := workout.SetAny(dst, false); err != nil {
		t.Fatal(err)
	}
	if err := workout.SetAny(src, true); err != nil {
		t.Fatal(err)
	}
	if err := mood.SetAny(src, 5); err != nil {
		t.Fatal(err)
	}

	MergeMissing(dst, src)

	if dst.Workout == nil || *dst.Workout {
		t.Fatalf("существующее значение тренировки перезаписано: %v", dst.Workout)
	}
	if dst.Mood == nil || *dst.Mood != 5 {
		t.Fatalf("пропущенное поле не дополнено: %v", dst.Mood)
	}
}
