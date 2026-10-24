package services

import (
	"testing"
	"time"

	"main/domain"
)

func TestLoginLimiterWindow(t *testing.T) {
	l := newLoginLimiter(LoginMaxFailures, LoginWindow)
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	for i := 0; i < LoginMaxFailures; i++ {
		if l.Blocked("1.1.1.1", now) {
			t.Fatalf("заблокирован после %d неудач", i)
		}
		l.Fail("1.1.1.1", now)
	}
	if !l.Blocked("1.1.1.1", now) || l.Blocked("2.2.2.2", now) {
		t.Fatal("лимит по IP")
	}
	if l.Blocked("1.1.1.1", now.Add(LoginWindow)) {
		t.Fatal("окно в минуту прошло — снова можно")
	}
}

func TestContactHiddenUntilWorkdays(t *testing.T) {
	site := domain.SiteOf(domain.SiteGTP)
	fri := time.Date(2026, 10, 23, 15, 0, 0, 0, site.Loc) // пятница
	until := ContactHiddenUntil(fri, site)
	if until.Weekday() != time.Wednesday {
		t.Fatalf("2 рабочих дня после пятницы — до среды 00:00, получили %v", until)
	}
	if workdaysSince(fri, fri.AddDate(0, 0, 3), site) != 1 {
		t.Fatal("пт → пн: один рабочий день")
	}
}
