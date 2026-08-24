package report

import "fmt"

// plural выбирает русскую форму слова по числу: 1 день, 2 дня, 5 дней.
func plural(n int, one, few, many string) string {
	n = abs(n)
	if n%100 >= 11 && n%100 <= 14 {
		return many
	}
	switch n % 10 {
	case 1:
		return one
	case 2, 3, 4:
		return few
	default:
		return many
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// days печатает «7 дней» с правильным окончанием.
func days(n int) string { return fmt.Sprintf("%d %s", n, plural(n, "день", "дня", "дней")) }

// measures печатает «2 замера».
func measures(n int) string {
	return fmt.Sprintf("%d %s", n, plural(n, "замер", "замера", "замеров"))
}
