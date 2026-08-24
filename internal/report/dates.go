// Package report считает агрегаты, стрики и собирает недельную выгрузку в
// markdown. Выгрузка — главный артефакт: её читает человек на еженедельном
// разборе, поэтому она должна быть самодостаточной.
package report

import "time"

// DateFmt — формат дат во всём проекте.
const DateFmt = "2006-01-02"

// ParseDate разбирает дату в формате YYYY-MM-DD.
func ParseDate(s string) (time.Time, error) { return time.Parse(DateFmt, s) }

// AddDays сдвигает дату на n дней.
func AddDays(date string, n int) string {
	t, err := ParseDate(date)
	if err != nil {
		return date
	}
	return t.AddDate(0, 0, n).Format(DateFmt)
}

// PrevDate возвращает предыдущий день.
func PrevDate(date string) string { return AddDays(date, -1) }

// DaysBetween считает количество дней в отрезке [from, to] включительно.
func DaysBetween(from, to string) int {
	f, err1 := ParseDate(from)
	t, err2 := ParseDate(to)
	if err1 != nil || err2 != nil {
		return 0
	}
	return int(t.Sub(f).Hours()/24) + 1
}

// Weekday возвращает русское сокращение дня недели.
func Weekday(date string) string {
	t, err := ParseDate(date)
	if err != nil {
		return ""
	}
	return [...]string{"вс", "пн", "вт", "ср", "чт", "пт", "сб"}[int(t.Weekday())]
}
