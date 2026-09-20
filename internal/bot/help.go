package bot

import (
	"fmt"
	"strings"

	"github.com/murluckk/self-reinvention/internal/config"
	"github.com/murluckk/self-reinvention/internal/model"
)

var helpText = buildHelp(config.ProfilePasha)

func helpTextFor(profile string) string {
	return buildHelp(profile)
}

// buildHelp собирает шпаргалку из того же реестра полей, что и парсер: список
// ключей в /help не может отстать от того, что бот реально понимает.
func buildHelp(profile string) string {
	var b strings.Builder
	b.WriteString(`Шпаргалка

Просто текст — заметка с таймстемпом. С «#тег» в начале — с тегом.
Голосовое — распознаю, покажу разбор, запишу после подтверждения.

/d [YYYY-MM-DD] ключ значение ...
`)
	if profile == config.ProfileSveta {
		b.WriteString(`  /d подъем 8:00 отбой 23:30 трен прогулка учеба состояние 8
  /d сладкое нет алкоголь нет полезное книга по психологии
`)
	} else {
		b.WriteString(`  /d подъем 7:30 отбой 23:40 алго 60 системы 30 трен состояние 8
  /d 2026-08-20 алго нет системы 45 трен нет состояние 6
`)
	}
	b.WriteString(`  Разделитель — пробел, «=» или «:». Тренировку можно писать флагом: «трен».
  Также понимаю «зал», «бег», «улица» и «отдых».
  Дата первым аргументом — дописать задним числом (upsert по дате).

Ключи:
`)
	for _, f := range model.FieldsFor(profile) {
		alias := strings.Join(f.Keys, ", ")
		kind := map[model.Kind]string{
			model.KindFloat:    "число",
			model.KindInt:      "целое",
			model.KindDuration: "мин/нет",
			model.KindScale:    "1-10",
			model.KindBool:     "да/нет",
			model.KindString:   "текст",
			model.KindTime:     "ЧЧ:ММ",
		}[f.Kind]
		fmt.Fprintf(&b, "  %-28s %-7s %s\n", alias, kind, f.Desc)
	}
	b.WriteString(`
Да/нет понимаю как 1/0, да/нет, +/-, y/n, true/false.
`)
	if profile == config.ProfilePasha {
		b.WriteString(`
/m — деньги
  /m +250000 зп            доход
  /m -1200 еда обед в кафе расход, остаток строки — комментарий
  /m =180000 накопления    отложено
  /m =5000$ холодный кошелёк  валюта суффиксом: ₽ $ € usdt btc
`)
	}
	if profile == config.ProfilePasha {
		b.WriteString(`
/s        статус: сегодня, неделя, капитал, стрики
`)
	} else {
		b.WriteString(`
/s        статус: сегодня, неделя, состояние, стрики
`)
	}
	b.WriteString(`
/w [n]    выгрузка за n дней (по умолчанию 7) markdown-файлом
/undo     удалить последнюю заметку
/help     эта шпаргалка

В 22:00 по твоему часовому поясу напомню внести итоги дня.
В воскресенье пришлю разбор недели.`)
	if profile == config.ProfilePasha {
		b.WriteString(`
5-го напомню записать зарплату, 20-го — аванс. 1-го числа придёт финансовый
итог и AI-анализ предыдущего месяца.`)
	}
	return b.String()
}
