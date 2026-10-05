package bot

import (
	"fmt"
	"strings"

	"github.com/murluckk/self-reinvention/internal/model"
)

// helpText собирает шпаргалку из того же реестра полей, что и парсер: список
// ключей в /help не может отстать от того, что бот реально понимает, и
// показывает только показатели этого пользователя.
func helpText(u *model.User, admin bool) string {
	var b strings.Builder
	b.WriteString(`Шпаргалка

Вечером сам спрошу кнопками, как прошёл день. Начать раньше — /c, за вчера — /c вчера.
Голосовое — распознаю, покажу разбор, запишу после подтверждения; любое поле можно поправить кнопкой «Исправить».
Просто текст — заметка с таймстемпом. С «#тег» в начале — с тегом.

/d [YYYY-MM-DD] ключ значение ...
  /d ` + exampleFor(u.Fields.Fields()) + `
  Разделитель — пробел, «=» или «:». Да/нет можно писать флагом: «трен».
  Дата первым аргументом — дописать задним числом.

Ключи:
`)
	kinds := map[model.Kind]string{
		model.KindFloat:    "число",
		model.KindInt:      "целое",
		model.KindDuration: "мин/нет",
		model.KindScale:    "1-10",
		model.KindBool:     "да/нет",
		model.KindString:   "текст",
		model.KindTime:     "ЧЧ:ММ",
	}
	fields := u.Fields.Fields()
	if note, ok := model.FieldByColumn("note"); ok {
		fields = append(fields, *note)
	}
	for _, f := range fields {
		fmt.Fprintf(&b, "  %-28s %-7s %s\n", strings.Join(f.Keys, ", "), kinds[f.Kind], f.Desc)
	}
	b.WriteString(`
Да/нет понимаю как 1/0, да/нет, +/-, y/n, true/false.
`)
	if u.Finance {
		b.WriteString(`
/m — деньги
  /m +250000 зп            доход
  /m -1200 еда обед в кафе расход, остаток строки — комментарий
  /m =180000 накопления    отложено
  /m =5000$ холодный кошелёк  валюта суффиксом: ₽ $ € usdt btc
`)
	}
	b.WriteString(`
/s        статус: сегодня, неделя, стрики
/w [n]    выгрузка за n дней (по умолчанию 7) markdown-файлом
/i        наблюдения: что связано с твоим состоянием
/review   AI-разбор последней недели
/undo     удалить последнюю заметку` + map[bool]string{true: " или трату", false: ""}[u.Finance] + `

Настройки
/fields   что отслеживать
/tz       часовой пояс (сейчас ` + u.Timezone + `)
/password новый пароль от дашборда
/share    делиться сериями с друзьями, /friends — их серии

В воскресенье пришлю выгрузку недели и AI-разбор.`)
	if u.Finance {
		b.WriteString(`
5-го напомню записать зарплату, 20-го — аванс. 1-го числа придёт финансовый
итог и AI-анализ предыдущего месяца.`)
	}
	if admin {
		b.WriteString(`

Админ
/invite [профиль]  ссылка-приглашение (профили: ` + presetNames() + `)
/users             участники
/remove <id>       закрыть доступ
/health            состояние бота и последние сбои`)
	}
	return b.String()
}

func presetNames() string {
	names := make([]string, 0, len(model.Presets))
	for _, p := range model.Presets {
		names = append(names, p.Name)
	}
	return strings.Join(names, ", ")
}

// exampleFor строит пример /d из полей пользователя. Поля, забирающие остаток
// строки, идут последними — иначе они съедят всё, что после них.
func exampleFor(fields []model.Field) string {
	var parts, rest []string
	for _, f := range fields {
		key := f.Keys[0]
		switch f.Kind {
		case model.KindBool:
			if f.Bad {
				parts = append(parts, key+" нет")
			} else {
				parts = append(parts, key)
			}
		case model.KindTime:
			v := "7:30"
			if f.DB == "bed" {
				v = "23:40"
			}
			parts = append(parts, key+" "+v)
		case model.KindDuration:
			parts = append(parts, key+" 45")
		case model.KindScale:
			parts = append(parts, key+" 8")
		case model.KindInt, model.KindFloat:
			parts = append(parts, key+" 5")
		case model.KindString:
			rest = append(rest, key+" книга по психологии")
		}
	}
	if len(rest) > 0 {
		parts = append(parts, rest[0])
	}
	return strings.Join(parts, " ")
}
