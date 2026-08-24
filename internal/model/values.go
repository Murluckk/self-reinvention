package model

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseFloat разбирает число, допуская запятую как разделитель дробной части:
// на телефоне запятая под пальцем, а точка — нет.
func ParseFloat(s string) (float64, error) {
	s = strings.TrimSpace(strings.ReplaceAll(s, ",", "."))
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("не число: %q", s)
	}
	return f, nil
}

// ParseInt разбирает целое, терпимо относясь к «40мин» и «2100 ккал».
func ParseInt(s string) (int, error) {
	f, err := ParseFloat(strings.TrimSpace(s))
	if err != nil {
		return 0, err
	}
	return int(f), nil
}

var boolWords = map[string]bool{
	"1": true, "0": false,
	"да": true, "нет": false,
	"д": true, "н": false,
	"+": true, "-": false,
	"y": true, "n": false,
	"yes": true, "no": false,
	"true": true, "false": false,
	"ага": true, "неа": false,
	"есть": true, "не": false,
}

// ParseBool разбирает булево значение во всех формах, которые приходят в голову
// в 23:00: 1/0, да/нет, +/-, y/n, true/false.
func ParseBool(s string) (bool, error) {
	v, ok := boolWords[strings.ToLower(strings.TrimSpace(s))]
	if !ok {
		return false, fmt.Errorf("не да/нет: %q", s)
	}
	return v, nil
}

// NormalizeTime приводит время суток к HH:MM. Понимает 7:30, 7.30, 0730, 730 и
// просто 7.
func NormalizeTime(s string) (string, error) {
	s = strings.TrimSpace(s)
	s = strings.NewReplacer(".", ":", ",", ":", "-", ":").Replace(s)
	var h, m int
	var err error
	if i := strings.Index(s, ":"); i >= 0 {
		if h, err = strconv.Atoi(s[:i]); err != nil {
			return "", fmt.Errorf("не время: %q", s)
		}
		if m, err = strconv.Atoi(s[i+1:]); err != nil {
			return "", fmt.Errorf("не время: %q", s)
		}
	} else {
		n, err := strconv.Atoi(s)
		if err != nil {
			return "", fmt.Errorf("не время: %q", s)
		}
		switch {
		case len(s) >= 3: // 730 или 0730
			h, m = n/100, n%100
		default:
			h = n
		}
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return "", fmt.Errorf("время вне диапазона: %q", s)
	}
	return fmt.Sprintf("%02d:%02d", h, m), nil
}

// TimeToHours переводит HH:MM в часы с дробной частью — так удобнее считать
// разброс времени подъёма.
func TimeToHours(s string) (float64, bool) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 0, false
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, false
	}
	return float64(h) + float64(m)/60, true
}
