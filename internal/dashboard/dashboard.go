// Package dashboard отдаёт лёгкую read-only сводку поверх той же SQLite,
// в которую пишет Telegram-бот. Никакого отдельного фронтенда и сборщика нет:
// одна HTML-страница, встроенные стили и ноль внешних запросов.
package dashboard

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/murluckk/self-reinvention/internal/config"
	"github.com/murluckk/self-reinvention/internal/model"
	"github.com/murluckk/self-reinvention/internal/parse"
	"github.com/murluckk/self-reinvention/internal/report"
	"github.com/murluckk/self-reinvention/internal/store"
)

// Dashboard — HTTP-сервер личной сводки.
type Dashboard struct {
	cfg *config.Config
	st  *store.Store
	log *slog.Logger
	tpl *template.Template
}

// New создаёт дашборд. Обычно он слушает только 127.0.0.1, а HTTPS завершает
// reverse proxy на той же машине.
func New(cfg *config.Config, st *store.Store, log *slog.Logger) *Dashboard {
	return &Dashboard{
		cfg: cfg,
		st:  st,
		log: log,
		tpl: template.Must(template.New("dashboard").Parse(pageHTML)),
	}
}

// Handler возвращает защищённый HTTP-handler, чтобы сервер можно было тестировать
// без открытия порта.
func (d *Dashboard) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.Handle("/", d.auth(http.HandlerFunc(d.index)))
	return securityHeaders(mux)
}

// Run запускает сервер и корректно закрывает его вместе с ботом.
func (d *Dashboard) Run(ctx context.Context) error {
	srv := &http.Server{
		Addr:              d.cfg.DashboardAddr,
		Handler:           d.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	d.log.Info("дашборд запущен", "addr", d.cfg.DashboardAddr)
	err := srv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (d *Dashboard) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		var userID int64
		matched := false
		for _, candidate := range d.cfg.Users {
			userOK := subtle.ConstantTimeCompare([]byte(user), []byte(candidate.DashboardUser)) == 1
			passwordOK := subtle.ConstantTimeCompare([]byte(password), []byte(candidate.DashboardPassword)) == 1
			if userOK && passwordOK {
				userID, matched = candidate.TelegramID, true
			}
		}
		if !ok || !matched {
			w.Header().Set("WWW-Authenticate", `Basic realm="Личный трекер", charset="UTF-8"`)
			http.Error(w, "Нужен логин и пароль", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userIDKey{}, userID)))
	})
}

type userIDKey struct{}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src data:; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func (d *Dashboard) index(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	period := periodFrom(r)
	userID, _ := r.Context().Value(userIDKey{}).(int64)
	page, err := d.page(userID, period, r.URL.Query().Get("date"))
	if err != nil {
		d.log.Error("сборка дашборда", "err", err)
		http.Error(w, "Не смог собрать данные", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := d.tpl.Execute(w, page); err != nil {
		d.log.Error("шаблон дашборда", "err", err)
	}
}

func periodFrom(r *http.Request) int {
	n, _ := strconv.Atoi(r.URL.Query().Get("days"))
	switch n {
	case 7, 30, 90, 365:
		return n
	default:
		return 30
	}
}

type pageData struct {
	Name       string
	Period     int
	From       string
	To         string
	Periods    []periodLink
	Cards      []card
	States     []stateCard
	Chart      chart
	Days       []dayView
	Money      []moneyView
	Expense    *expenseDetail
	Streaks    []streak
	Flags      []string
	FilledDays int
	TotalDays  int
}

type periodLink struct {
	Days   int
	Label  string
	Active bool
}

type card struct {
	Label string
	Value string
	Hint  string
}

type stateCard struct {
	Label string
	Value string
	Class string
}

type chart struct {
	HasData bool
	Series  []chartSeries
}

type chartSeries struct {
	Name   string
	Class  string
	Points string
	Dots   []chartDot
}

type chartDot struct {
	X, Y  float64
	Value int
	Date  string
}

type dayView struct {
	Date    string
	Weekday string
	Metrics []metric
}

type metric struct {
	Label string
	Value string
	Class string
	Href  string
}

type moneyView struct {
	Code    string
	Income  string
	Expense string
	Saved   string
	Capital string
}

type streak struct {
	Label string
	Value string
}

type expenseDetail struct {
	Date       string
	Total      string
	Categories []expenseCategory
	Entries    []expenseEntry
}

type expenseCategory struct {
	Name   string
	Amount string
}

type expenseEntry struct {
	Category string
	Comment  string
	Amount   string
}

func (d *Dashboard) page(userID int64, period int, selectedDate string) (*pageData, error) {
	to := d.cfg.Today()
	from := report.AddDays(to, -(period - 1))
	days, err := d.st.Days(userID, from, to)
	if err != nil {
		return nil, err
	}
	daysAll, err := d.st.Days(userID, report.AddDays(to, -400), to)
	if err != nil {
		return nil, err
	}
	money, err := d.st.Money(userID, from, to)
	if err != nil {
		return nil, err
	}
	moneyAll, err := d.st.MoneyUntil(userID, to)
	if err != nil {
		return nil, err
	}
	stats := report.Build(report.Input{
		From: from, To: to,
		Days: days, DaysAll: daysAll,
		Money: money, MoneyAll: moneyAll,
		Today: to, Cfg: d.cfg,
	})

	p := &pageData{
		Period: period, From: from, To: to,
		Periods: []periodLink{
			{7, "7 дней", period == 7},
			{30, "30 дней", period == 30},
			{90, "3 месяца", period == 90},
			{365, "Год", period == 365},
		},
		FilledDays: stats.FilledDays,
		TotalDays:  stats.TotalDays,
		Flags:      stats.Flags,
	}
	if user, ok := d.cfg.User(userID); ok {
		p.Name = user.Name
	}
	p.Cards = []card{
		{"Последний подъём", seriesLastTime(stats.Wake), seriesHint(stats.Wake, "отмечено")},
		{"Последнее засыпание", seriesLastTime(stats.Bed), seriesHint(stats.Bed, "отмечено")},
		{"Алгоритмы", fmt.Sprintf("%.0f мин", stats.Algorithms.Sum()), fmt.Sprintf("%d дней", stats.AlgorithmDays)},
		{"Системный дизайн", fmt.Sprintf("%.0f мин", stats.SystemDesign.Sum()), fmt.Sprintf("%d дней", stats.SystemDesignDays)},
	}
	p.States = []stateCard{
		{"Эмоциональное состояние", scaleValue(stats.Mood), "mood"},
	}
	p.Chart = makeChart(days)
	expenses := expenseTotals(money)
	p.Days = makeDays(days, expenses, period, 31)
	p.Expense = makeExpenseDetail(selectedDate, from, to, money)
	p.Streaks = []streak{
		{"Алгоритмы", fmt.Sprintf("%d дн.", stats.Streaks.Algorithms)},
		{"Системный дизайн", fmt.Sprintf("%d дн.", stats.Streaks.SystemDesign)},
		{"Тренировки", fmt.Sprintf("%d дн.", stats.Streaks.Workout)},
		{"Записи", fmt.Sprintf("%d дн.", stats.Streaks.Filled)},
	}
	for _, code := range stats.CurOrder {
		c := stats.Currencies[code]
		p.Money = append(p.Money, moneyView{
			Code: code, Income: parse.FormatAmount(c.Income),
			Expense: parse.FormatAmount(c.Expense), Saved: parse.FormatAmount(c.Saved),
			Capital: parse.FormatAmount(c.Capital),
		})
	}
	return p, nil
}

func seriesLastTime(s report.Series) string {
	if s.N() == 0 {
		return "—"
	}
	hours := s.Values[s.N()-1]
	h := int(hours)
	m := int(math.Round((hours - float64(h)) * 60))
	if m == 60 {
		h, m = h+1, 0
	}
	return fmt.Sprintf("%02d:%02d", h, m)
}

func seriesHint(s report.Series, label string) string {
	if s.N() == 0 {
		return "нет данных"
	}
	return fmt.Sprintf("%s · %d дн.", label, s.N())
}

func scaleValue(s report.Series) string {
	if s.N() == 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f/10", s.Avg())
}

func makeChart(days []*model.Day) chart {
	type source struct {
		name, class string
		value       func(*model.Day) *int
	}
	sources := []source{
		{"Состояние", "mood", func(d *model.Day) *int { return d.Mood }},
	}
	out := chart{}
	for _, src := range sources {
		line := chartSeries{Name: src.name, Class: src.class}
		for i, day := range days {
			v := src.value(day)
			if v == nil {
				continue
			}
			x := 400.0
			if len(days) > 1 {
				x = 40 + float64(i)*720/float64(len(days)-1)
			}
			y := 200 - float64(*v-1)*160/9
			line.Dots = append(line.Dots, chartDot{X: x, Y: y, Value: *v, Date: day.Date})
		}
		var points []string
		for _, dot := range line.Dots {
			points = append(points, fmt.Sprintf("%.1f,%.1f", dot.X, dot.Y))
		}
		line.Points = strings.Join(points, " ")
		if len(line.Dots) > 0 {
			out.HasData = true
		}
		out.Series = append(out.Series, line)
	}
	return out
}

func makeDays(days []*model.Day, expenses map[string]float64, period, limit int) []dayView {
	merged := append([]*model.Day(nil), days...)
	known := make(map[string]bool, len(days))
	for _, d := range days {
		known[d.Date] = true
	}
	for date := range expenses {
		if !known[date] {
			merged = append(merged, &model.Day{Date: date})
		}
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].Date < merged[j].Date })
	start := 0
	if len(merged) > limit {
		start = len(merged) - limit
	}
	out := make([]dayView, 0, len(merged)-start)
	for i := len(merged) - 1; i >= start; i-- {
		d := merged[i]
		if d.Empty() {
			continue
		}
		v := dayView{Date: shortDate(d.Date), Weekday: report.Weekday(d.Date)}
		add := func(label, value, class, href string) {
			if value != "" {
				v.Metrics = append(v.Metrics, metric{Label: label, Value: value, Class: class, Href: href})
			}
		}
		if d.Wake != nil {
			add("Подъём", *d.Wake, "", "")
		}
		if d.Bed != nil {
			add("Заснул", *d.Bed, "", "")
		}
		if d.Algorithms != nil {
			add("Алгоритмы", durationValue(*d.Algorithms), "", "")
		}
		if d.SystemDesign != nil {
			add("Системный дизайн", durationValue(*d.SystemDesign), "", "")
		}
		if d.Workout != nil {
			value, class := "нет", "bad"
			if *d.Workout {
				value, class = "да", "good"
			}
			add("Тренировка", value, class, "")
		}
		if d.Mood != nil {
			add("Состояние", fmt.Sprintf("%d/10", *d.Mood), "mood", "")
		}
		if total := expenses[d.Date]; total > 0 {
			add("Траты", parse.FormatAmount(total)+" ₽", "expense",
				fmt.Sprintf("/?days=%d&date=%s#expenses", period, d.Date))
		}
		out = append(out, v)
	}
	return out
}

func durationValue(minutes int) string {
	if minutes <= 0 {
		return "нет"
	}
	return fmt.Sprintf("%d мин", minutes)
}

func expenseTotals(money []*model.Money) map[string]float64 {
	out := map[string]float64{}
	for _, m := range money {
		if m.Kind == model.MoneyExpense {
			out[m.Date] += m.Amount
		}
	}
	return out
}

func makeExpenseDetail(date, from, to string, money []*model.Money) *expenseDetail {
	if date == "" || date < from || date > to {
		return nil
	}
	detail := &expenseDetail{Date: date}
	categories := map[string]float64{}
	var total float64
	for _, m := range money {
		if m.Date != date || m.Kind != model.MoneyExpense {
			continue
		}
		category := strings.TrimSpace(m.Category)
		if category == "" {
			category = "без категории"
		}
		total += m.Amount
		categories[category] += m.Amount
		detail.Entries = append(detail.Entries, expenseEntry{
			Category: category,
			Comment:  m.Comment,
			Amount:   parse.FormatAmount(m.Amount) + " ₽",
		})
	}
	if len(detail.Entries) == 0 {
		return nil
	}
	detail.Total = parse.FormatAmount(total) + " ₽"
	for name, amount := range categories {
		detail.Categories = append(detail.Categories, expenseCategory{
			Name: name, Amount: parse.FormatAmount(amount) + " ₽",
		})
	}
	sort.Slice(detail.Categories, func(i, j int) bool {
		return categories[detail.Categories[i].Name] > categories[detail.Categories[j].Name]
	})
	return detail
}

func shortDate(date string) string {
	t, err := time.Parse(report.DateFmt, date)
	if err != nil {
		return date
	}
	months := [...]string{"янв", "фев", "мар", "апр", "май", "июн", "июл", "авг", "сен", "окт", "ноя", "дек"}
	return fmt.Sprintf("%d %s", t.Day(), months[int(t.Month())-1])
}

const pageHTML = `<!doctype html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="color-scheme" content="dark">
<title>{{.Name}} · Личный трекер</title>
<style>
:root{--bg:#0b0d10;--panel:#15181d;--panel2:#1b1f25;--text:#f4f6f8;--muted:#9299a3;--line:#292e36;--green:#86efac;--blue:#7dd3fc;--violet:#c4b5fd;--orange:#fdba74;--red:#fca5a5}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--text);font:15px/1.45 system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}
.wrap{width:min(1120px,calc(100% - 28px));margin:auto;padding:34px 0 60px}
header{display:flex;justify-content:space-between;align-items:flex-end;gap:20px;margin-bottom:26px}h1{font-size:clamp(28px,5vw,46px);letter-spacing:-.04em;margin:0}header p{margin:6px 0 0;color:var(--muted)}
.periods{display:flex;gap:6px;flex-wrap:wrap}.periods a{color:var(--muted);text-decoration:none;padding:8px 12px;border:1px solid var(--line);border-radius:999px}.periods a.active{background:var(--text);color:var(--bg);border-color:var(--text)}
.grid{display:grid;grid-template-columns:repeat(4,1fr);gap:10px}.card,.panel{background:var(--panel);border:1px solid var(--line);border-radius:18px}.card{padding:18px}.label{color:var(--muted);font-size:13px}.value{font-size:28px;font-weight:720;letter-spacing:-.03em;margin-top:4px}.hint{color:var(--muted);font-size:12px;margin-top:3px}
section{margin-top:26px}h2{font-size:18px;margin:0 0 10px;letter-spacing:-.01em}.sub{color:var(--muted);font-weight:400}.states{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:10px}.state{padding:16px 18px;border-left:3px solid}.state.mood{border-color:var(--violet)}
.chart{padding:18px;margin-top:10px;overflow:hidden}.chart svg{display:block;width:100%;height:auto}.axis{stroke:var(--line);stroke-width:1}.axis-label{fill:var(--muted);font-size:11px}.trend{fill:none;stroke-width:3;stroke-linecap:round;stroke-linejoin:round}.trend.focus,.dot.focus{stroke:var(--blue)}.trend.mood,.dot.mood{stroke:var(--violet)}.trend.energy,.dot.energy{stroke:var(--orange)}.dot{fill:var(--panel);stroke-width:3}.legend{display:flex;gap:16px;justify-content:center;color:var(--muted);font-size:12px;margin-top:8px}.legend i{display:inline-block;width:8px;height:8px;border-radius:50%;margin-right:6px}.legend .focus{background:var(--blue)}.legend .mood{background:var(--violet)}.legend .energy{background:var(--orange)}
.streaks{display:grid;grid-template-columns:repeat(4,1fr);gap:1px;overflow:hidden}.streak{padding:16px;background:var(--panel2)}.streak b{display:block;font-size:20px;margin-top:2px}
.money{display:grid;grid-template-columns:repeat(auto-fit,minmax(240px,1fr));gap:10px}.money-card{padding:18px}.money-card h3{margin:0 0 14px}.money-row{display:flex;justify-content:space-between;padding:6px 0;color:var(--muted)}.money-row b{color:var(--text)}.money-row.capital{border-top:1px solid var(--line);margin-top:6px;padding-top:12px}
.expense-detail{padding:18px}.expense-total{font-size:28px;font-weight:720;margin:0 0 14px}.expense-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(200px,1fr));gap:8px}.expense-line{display:flex;justify-content:space-between;gap:14px;background:var(--panel2);border-radius:10px;padding:10px 12px}.expense-entries{margin-top:14px;border-top:1px solid var(--line);padding-top:8px}.expense-entry{display:grid;grid-template-columns:1fr 2fr auto;gap:12px;padding:7px 0;color:var(--muted)}.expense-entry b{color:var(--text)}
.days{display:grid;grid-template-columns:repeat(2,1fr);gap:8px}.day{padding:16px}.day-head{display:flex;gap:8px;align-items:baseline;margin-bottom:12px}.day-head b{font-size:17px}.day-head span{color:var(--muted)}.metrics{display:flex;flex-wrap:wrap;gap:7px}.metric{display:block;background:var(--panel2);border-radius:9px;padding:7px 9px;font-size:12px;color:var(--text);text-decoration:none}.metric span{color:var(--muted);margin-right:5px}.metric.mood b{color:var(--violet)}.metric.expense b{color:var(--orange)}.metric.good b{color:var(--green)}.metric.bad b{color:var(--red)}a.metric:hover{outline:1px solid var(--orange)}
.flags{margin:0;padding:14px 18px 14px 36px}.flags li{padding:4px 0}.empty{color:var(--muted);padding:24px;text-align:center}
@media(max-width:760px){.wrap{padding-top:22px}header{display:block}.periods{margin-top:16px}.grid{grid-template-columns:repeat(2,1fr)}.states{grid-template-columns:1fr}.streaks{grid-template-columns:repeat(2,1fr)}.days{grid-template-columns:1fr}.chart{padding:10px}.value{font-size:24px}}
</style>
</head>
<body><main class="wrap">
<header><div><h1>{{.Name}}</h1><p>Личный трекер · {{.From}} — {{.To}} · заполнено {{.FilledDays}} из {{.TotalDays}}</p></div>
<nav class="periods">{{range .Periods}}<a href="/?days={{.Days}}"{{if .Active}} class="active"{{end}}>{{.Label}}</a>{{end}}</nav></header>

<div class="grid">{{range .Cards}}<article class="card"><div class="label">{{.Label}}</div><div class="value">{{.Value}}</div><div class="hint">{{.Hint}}</div></article>{{end}}</div>

<section><h2>Состояние <span class="sub">· среднее за период</span></h2>
<div class="states">{{range .States}}<article class="card state {{.Class}}"><div class="label">{{.Label}}</div><div class="value">{{.Value}}</div></article>{{end}}</div>
{{if .Chart.HasData}}<div class="panel chart"><svg viewBox="0 0 800 230" role="img" aria-label="Динамика оценок от 1 до 10">
<line class="axis" x1="40" y1="40" x2="760" y2="40"/><line class="axis" x1="40" y1="120" x2="760" y2="120"/><line class="axis" x1="40" y1="200" x2="760" y2="200"/>
<text class="axis-label" x="12" y="44">10</text><text class="axis-label" x="18" y="124">5</text><text class="axis-label" x="18" y="204">1</text>
{{range .Chart.Series}}{{if .Points}}{{$class := .Class}}<polyline class="trend {{$class}}" points="{{.Points}}"/>{{range .Dots}}<circle class="dot {{$class}}" cx="{{.X}}" cy="{{.Y}}" r="4"><title>{{.Date}}: {{.Value}}/10</title></circle>{{end}}{{end}}{{end}}
</svg><div class="legend">{{range .Chart.Series}}<span><i class="{{.Class}}"></i>{{.Name}}</span>{{end}}</div></div>{{end}}</section>

<section><h2>Стрики</h2><div class="panel streaks">{{range .Streaks}}<div class="streak"><span class="label">{{.Label}}</span><b>{{.Value}}</b></div>{{end}}</div></section>

{{if .Money}}<section><h2>Деньги</h2><div class="money">{{range .Money}}<article class="panel money-card"><h3>{{.Code}}</h3>
<div class="money-row"><span>Доход</span><b>{{.Income}}</b></div><div class="money-row"><span>Расход</span><b>{{.Expense}}</b></div><div class="money-row"><span>Отложено</span><b>{{.Saved}}</b></div><div class="money-row capital"><span>Капитал</span><b>{{.Capital}}</b></div>
</article>{{end}}</div></section>{{end}}

{{with .Expense}}<section id="expenses"><h2>Траты за {{.Date}}</h2><article class="panel expense-detail"><div class="expense-total">{{.Total}}</div>
<div class="expense-grid">{{range .Categories}}<div class="expense-line"><span>{{.Name}}</span><b>{{.Amount}}</b></div>{{end}}</div>
<div class="expense-entries">{{range .Entries}}<div class="expense-entry"><b>{{.Category}}</b><span>{{.Comment}}</span><b>{{.Amount}}</b></div>{{end}}</div>
</article></section>{{end}}

{{if .Flags}}<section><h2>Стоит обратить внимание</h2><div class="panel"><ul class="flags">{{range .Flags}}<li>{{.}}</li>{{end}}</ul></div></section>{{end}}

<section><h2>Последние дни</h2>{{if .Days}}<div class="days">{{range .Days}}<article class="panel day"><div class="day-head"><b>{{.Date}}</b><span>{{.Weekday}}</span></div><div class="metrics">{{range .Metrics}}{{if .Href}}<a href="{{.Href}}" class="metric {{.Class}}"><span>{{.Label}}</span><b>{{.Value}}</b></a>{{else}}<div class="metric {{.Class}}"><span>{{.Label}}</span><b>{{.Value}}</b></div>{{end}}{{end}}</div></article>{{end}}</div>{{else}}<div class="panel empty">Пока нет записей. Отправьте боту команду /d.</div>{{end}}</section>
</main></body></html>`
