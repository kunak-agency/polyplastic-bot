package repositories

import (
	"context"
	"os"
	"testing"
	"time"

	"main/domain"

	"github.com/google/uuid"
)

// Схема итерации 24.10 (волна 0): users, sessions, nudges, holidays, catchup. Интеграционный,
// TEST_DATABASE_URL; без базы пропускается.
func iterRepos(t *testing.T) *Repos {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL не задан")
	}
	r, err := InitRepos(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.MigrateDB(); err != nil {
		t.Fatal(err)
	}
	if err := r.Truncate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestUsersAndSessions(t *testing.T) {
	r, ctx := iterRepos(t), context.Background()
	off := &domain.User{Login: "off", PasswordHash: "x", FullName: "Выключен", Role: domain.RoleRecruiter, Active: false}
	if err := r.Users.Create(ctx, off); err != nil {
		t.Fatal(err)
	}
	got, err := r.Users.ByLogin(ctx, "off")
	if err != nil || got == nil || got.Active {
		t.Fatalf("Active=false должен сохраниться: %+v, %v", got, err)
	}
	if err := r.Users.Create(ctx, &domain.User{Login: "off", Role: domain.RoleManager}); !IsUniqueViolation(err) {
		t.Fatalf("логин уникален: %v", err)
	}
	if err := r.Users.SetActive(ctx, off.ID, true); err != nil {
		t.Fatal(err)
	}
	if got, _ = r.Users.ByID(ctx, off.ID); !got.Active {
		t.Fatal("SetActive(true) не применился")
	}

	id, err := domain.NewSessionID()
	if err != nil || len(id) != 64 {
		t.Fatalf("id сессии: %q, %v", id, err)
	}
	now := time.Now()
	if err := r.Sessions.Create(ctx, &domain.Session{ID: id, UserID: off.ID, ExpiresAt: now.Add(domain.SessionTTL), CreatedIP: "127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	if s, _ := r.Sessions.ByID(ctx, id, now); s == nil || s.UserID != off.ID {
		t.Fatalf("сессия не найдена: %+v", s)
	}
	if s, _ := r.Sessions.ByID(ctx, id, now.Add(13*time.Hour)); s != nil {
		t.Fatal("истёкшая сессия не должна находиться")
	}
	if n, err := r.Sessions.DeleteExpired(ctx, now.Add(13*time.Hour)); err != nil || n != 1 {
		t.Fatalf("DeleteExpired: %d, %v", n, err)
	}
}

func TestNudgesHolidaysCatchup(t *testing.T) {
	r, ctx := iterRepos(t), context.Background()
	emp := uuid.New()
	if err := r.Nudges.Create(ctx, &domain.Nudge{EmployeeID: emp, Kind: domain.NudgeAuto, N: 1, SentAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if ns, err := r.Nudges.ListByEmployee(ctx, emp); err != nil || len(ns) != 1 || ns[0].CreatedBy != nil {
		t.Fatalf("nudges: %+v, %v", ns, err)
	}

	d := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	if err := r.Holidays.Create(ctx, &domain.Holiday{Date: d, Title: "День народного единства"}); err != nil {
		t.Fatal(err)
	}
	if hs, err := r.Holidays.List(ctx, d.AddDate(0, 0, -1), d); err != nil || len(hs) != 1 {
		t.Fatalf("holidays: %+v, %v", hs, err)
	}
	if hs, _ := r.Holidays.List(ctx, d.AddDate(0, 0, 1), time.Time{}); len(hs) != 0 {
		t.Fatalf("после 4.11 праздников нет: %+v", hs)
	}

	mk := func(status, waiting string, catchup bool) domain.ScheduledEvent {
		return domain.ScheduledEvent{ID: uuid.New(), EmployeeID: emp, StepID: uuid.New(), Status: status, Waiting: waiting, Catchup: catchup, FireAt: time.Now()}
	}
	evs := []domain.ScheduledEvent{
		mk(domain.EventAnswered, "", true),
		mk(domain.EventSent, domain.WaitAnswer, true),
		mk(domain.EventScheduled, "", true),
		mk(domain.EventScheduled, "", false),
	}
	if err := r.Events.CreateMissing(ctx, evs); err != nil {
		t.Fatal(err)
	}
	if done, total, err := r.Events.CatchupProgress(ctx, emp); err != nil || done != 1 || total != 3 {
		t.Fatalf("догоняет %d из %d, %v; ждали 1 из 3", done, total, err)
	}
}
