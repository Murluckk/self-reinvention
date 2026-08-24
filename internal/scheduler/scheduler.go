// Package scheduler — напоминания по расписанию. Отдельная горутина с тикером
// вместо cron: одна зависимость меньше, а точность до минуты здесь и не нужна.
package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/murluckk/self-reinvention/internal/config"
	"github.com/murluckk/self-reinvention/internal/store"
)

// Job — задача по расписанию.
type Job struct {
	Name    string        // ключ в таблице jobs
	At      string        // время срабатывания HH:MM в часовом поясе конфига
	Weekday *time.Weekday // если задан, срабатывает только в этот день недели
	Run     func(context.Context) error
}

// Scheduler запускает задачи и помнит, что уже отправлял.
type Scheduler struct {
	cfg  *config.Config
	st   *store.Store
	log  *slog.Logger
	jobs []Job
}

// New создаёт планировщик.
func New(cfg *config.Config, st *store.Store, log *slog.Logger, jobs ...Job) *Scheduler {
	return &Scheduler{cfg: cfg, st: st, log: log, jobs: jobs}
}

// Run крутит тикер до отмены контекста.
func (s *Scheduler) Run(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	s.tick(ctx) // проверяем сразу на старте: бот мог быть выключен в час икс
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.tick(ctx)
		}
	}
}

func (s *Scheduler) tick(ctx context.Context) {
	now := s.cfg.Now()
	today := now.Format("2006-01-02")
	nowMin := now.Hour()*60 + now.Minute()
	for _, j := range s.jobs {
		if j.Weekday != nil && now.Weekday() != *j.Weekday {
			continue
		}
		at, err := minutes(j.At)
		if err != nil {
			s.log.Error("плохое время задачи", "job", j.Name, "at", j.At, "err", err)
			continue
		}
		if nowMin < at {
			continue
		}
		// Дата последнего запуска — защита от дублей при рестарте: перезапуск
		// бота в 23:05 не должен присылать напоминание второй раз.
		last, err := s.st.JobLastRun(j.Name)
		if err != nil {
			s.log.Error("чтение состояния задачи", "job", j.Name, "err", err)
			continue
		}
		if last == today {
			continue
		}
		if err := j.Run(ctx); err != nil {
			s.log.Error("задача не отработала", "job", j.Name, "err", err)
			continue // не отмечаем как выполненную — повторим на следующем тике
		}
		if err := s.st.SetJobLastRun(j.Name, today); err != nil {
			s.log.Error("сохранение состояния задачи", "job", j.Name, "err", err)
		}
		s.log.Info("задача отработала", "job", j.Name, "date", today)
	}
}

func minutes(hhmm string) (int, error) {
	parts := strings.SplitN(strings.TrimSpace(hhmm), ":", 2)
	if len(parts) != 2 {
		return 0, fmt.Errorf("ожидал HH:MM, получил %q", hhmm)
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, fmt.Errorf("ожидал HH:MM, получил %q", hhmm)
	}
	return h*60 + m, nil
}
