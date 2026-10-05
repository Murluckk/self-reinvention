package users

import (
	"path/filepath"
	"testing"

	"github.com/murluckk/self-reinvention/internal/auth"
	"github.com/murluckk/self-reinvention/internal/store"
)

func open(t *testing.T) (*store.Store, *Registry) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	reg, err := Load(st)
	if err != nil {
		t.Fatal(err)
	}
	return st, reg
}

func TestImportOnceAndPersist(t *testing.T) {
	st, reg := open(t)
	seeds := []Seed{
		{ID: 1, Name: "Паша", Preset: "pasha", Login: "owner", Password: "p1"},
		{ID: 2, Name: "Света", Preset: "sveta", Timezone: "Asia/Irkutsk", Login: "friend", Password: "p2"},
	}
	imported, err := reg.Import(append(seeds, Seed{ID: 3, Name: "Тимур", Preset: "pasha", Login: "timur", Password: "p3"}), 1)
	if err != nil || len(imported) != 3 {
		t.Fatalf("импорт: %v %v", imported, err)
	}
	pasha, _ := reg.Get(1)
	if !pasha.Finance || pasha.Timezone != "Asia/Vladivostok" || !pasha.Fields.Has("algorithms") ||
		!auth.Verify(pasha.PasswordHash, "p1") {
		t.Fatalf("Паша импортирован неверно: %+v", pasha)
	}

	// Смена пароля в боте не должна откатываться следующим импортом из env.
	pasha.PasswordHash, _ = auth.Hash("new")
	if err := reg.Save(pasha); err != nil {
		t.Fatal(err)
	}
	if timur, _ := reg.Get(3); timur.Finance || !timur.Fields.Has("algorithms") {
		t.Fatalf("финансы при импорте положены только владельцу: %+v", timur)
	}
	if imported, _ := reg.Import(seeds, 1); len(imported) != 0 {
		t.Fatalf("повторный импорт перезаписал пользователей: %v", imported)
	}
	reloaded, err := Load(st)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := reloaded.Get(1)
	if !ok || !auth.Verify(p.PasswordHash, "new") || p.Location().String() != "Asia/Vladivostok" {
		t.Fatalf("после перезагрузки: %+v", p)
	}
	if s, _ := reloaded.Get(2); s.Finance || !s.Fields.Has("walk") {
		t.Fatalf("Света после перезагрузки: %+v", s)
	}
}

func TestSaveRejectsBadTimezoneAndDuplicateLogin(t *testing.T) {
	_, reg := open(t)
	if _, err := reg.Import([]Seed{{ID: 1, Name: "A", Preset: "basic", Login: "a", Password: "x"}}, 1); err != nil {
		t.Fatal(err)
	}
	u, _ := reg.Get(1)
	u.Timezone = "Mars/Olympus"
	if err := reg.Save(u); err == nil {
		t.Fatal("сохранился несуществующий пояс")
	}
	if _, err := reg.Import([]Seed{{ID: 2, Name: "B", Preset: "basic", Login: "a", Password: "y"}}, 1); err == nil {
		t.Fatal("сохранился дубликат логина")
	}
	if got := reg.FreeLogin("A", 2); got != "a2" {
		t.Fatalf("FreeLogin -> %q", got)
	}
	if got := reg.FreeLogin("", 42); got != "user42" {
		t.Fatalf("FreeLogin без username -> %q", got)
	}
}
