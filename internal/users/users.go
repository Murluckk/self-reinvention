// Package users — реестр участников трекера. Источник правды — таблица users
// в SQLite, а в памяти лежит копия: к ней обращается каждое сообщение, каждый
// тик планировщика и каждый запрос дашборда.
package users

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/murluckk/self-reinvention/internal/auth"
	"github.com/murluckk/self-reinvention/internal/model"
	"github.com/murluckk/self-reinvention/internal/store"
)

// Registry — потокобезопасный реестр пользователей.
type Registry struct {
	st *store.Store

	mu   sync.RWMutex
	byID map[int64]*model.User
}

// Load читает пользователей из базы.
func Load(st *store.Store) (*Registry, error) {
	list, err := st.Users()
	if err != nil {
		return nil, err
	}
	r := &Registry{st: st, byID: map[int64]*model.User{}}
	for _, u := range list {
		u.Loc = location(u.Timezone)
		r.byID[u.ID] = u
	}
	return r, nil
}

func location(tz string) *time.Location {
	if loc, err := time.LoadLocation(tz); err == nil {
		return loc
	}
	return time.UTC
}

// Get возвращает копию пользователя: вызывающий может её менять и сохранять
// через Save, не трогая общее состояние.
func (r *Registry) Get(id int64) (*model.User, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	u, ok := r.byID[id]
	if !ok {
		return nil, false
	}
	return clone(u), true
}

// All возвращает копии всех пользователей в порядке регистрации.
func (r *Registry) All() []*model.User {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*model.User, 0, len(r.byID))
	for _, u := range r.byID {
		out = append(out, clone(u))
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// ByLogin ищет пользователя по логину дашборда.
func (r *Registry) ByLogin(login string) (*model.User, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, u := range r.byID {
		if u.Login == login {
			return clone(u), true
		}
	}
	return nil, false
}

// Save проверяет часовой пояс, пишет пользователя в базу и обновляет кэш.
func (r *Registry) Save(u *model.User) error {
	loc, err := time.LoadLocation(u.Timezone)
	if err != nil {
		return fmt.Errorf("не знаю часовой пояс %q", u.Timezone)
	}
	if strings.TrimSpace(u.Name) == "" || strings.TrimSpace(u.Login) == "" || u.PasswordHash == "" {
		return fmt.Errorf("у пользователя %d пустое имя, логин или пароль", u.ID)
	}
	saved := clone(u)
	saved.Loc = loc
	if err := r.st.SaveUser(saved); err != nil {
		return err
	}
	r.mu.Lock()
	r.byID[saved.ID] = saved
	r.mu.Unlock()
	u.Loc, u.CreatedAt = loc, saved.CreatedAt
	return nil
}

// Remove закрывает доступ пользователю.
func (r *Registry) Remove(id int64) error {
	if err := r.st.DeleteUser(id); err != nil {
		return err
	}
	r.mu.Lock()
	delete(r.byID, id)
	r.mu.Unlock()
	return nil
}

func clone(u *model.User) *model.User {
	c := *u
	c.Fields = append(model.FieldSet{}, u.Fields...)
	return &c
}

var loginUnsafe = regexp.MustCompile(`[^a-z0-9_-]+`)

// FreeLogin подбирает свободный логин: Telegram username, если он есть, иначе
// user<id>; при совпадении добавляет цифру.
func (r *Registry) FreeLogin(username string, id int64) string {
	base := loginUnsafe.ReplaceAllString(strings.ToLower(username), "")
	if base == "" {
		base = "user" + strconv.FormatInt(id, 10)
	}
	login := base
	for i := 2; ; i++ {
		if _, taken := r.ByLogin(login); !taken {
			return login
		}
		login = base + strconv.Itoa(i)
	}
}

// Seed — пользователь из старого формата TRACKER_USERS в env.
type Seed struct {
	ID       int64
	Name     string
	Preset   string
	Timezone string
	Login    string
	Password string
}

// Import переносит пользователей из env в базу, если их там ещё нет. После
// первого запуска env больше не источник правды: пароль и настройки меняются
// командами бота, а переменную можно убрать. В старой конфигурации финансовые
// напоминания получал только владелец, поэтому финансы при импорте включаются
// только ему; остальные включат их сами в /fields.
func (r *Registry) Import(seeds []Seed, ownerID int64) (imported []string, err error) {
	for _, s := range seeds {
		if _, ok := r.Get(s.ID); ok {
			continue
		}
		preset, ok := model.PresetByName(s.Preset)
		if !ok {
			preset, _ = model.PresetByName("basic")
		}
		tz := s.Timezone
		if tz == "" {
			tz = preset.Timezone
		}
		password := s.Password
		if password == "" {
			password = auth.Generate()
		}
		hash, err := auth.Hash(password)
		if err != nil {
			return imported, err
		}
		login := s.Login
		if login == "" {
			login = r.FreeLogin("", s.ID)
		}
		u := &model.User{
			ID: s.ID, Name: s.Name, Fields: preset.Fields, Finance: preset.Finance && s.ID == ownerID,
			Timezone: tz, Login: login, PasswordHash: hash,
		}
		if err := r.Save(u); err != nil {
			return imported, fmt.Errorf("импорт %d: %w", s.ID, err)
		}
		imported = append(imported, s.Name)
	}
	return imported, nil
}
