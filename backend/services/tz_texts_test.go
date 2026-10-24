package services

import (
	"strings"
	"testing"
	"time"

	"main/domain"
)

// Время в текстах бота по поясу новичка (FEATURE_TZ): {start_day}, «пришлю … в 10:00»,
// календарь «Напомнить завтра» (BotService.siteFor).
func TestTextsInNewcomerTZ(t *testing.T) {
	vy := domain.AddressVy
	e := &domain.Employee{Site: domain.SiteGTP, TZ: strp(tzEkb), StartDate: date(2026, 11, 3), AddressForm: &vy, FullName: "Тестов Пётр"}
	st := domain.ScenarioStep{TextVy: "Выход {start_day}"}
	at := msk(2026, 11, 2, 23, 30) // 01:30 3 ноября в Екатеринбурге

	on, off := tzScheduler(true), tzScheduler(false)
	if got := stepTextIn(st, e, at, on.recipient(e).siteFor(e)); got != "Выход сегодня" {
		t.Fatalf("с флагом: %q", got)
	}
	if got := stepTextIn(st, e, at, off.recipient(e).siteFor(e)); got != "Выход завтра" {
		t.Fatalf("без флага — по площадке: %q", got)
	}
	if m := stepMessage(on.recipient(e), st, e, [16]byte{}, false, at); m.Text != "Выход сегодня" {
		t.Fatalf("stepMessage: %q", m.Text)
	}

	next := time.Date(2026, 11, 9, 5, 0, 0, 0, time.UTC) // понедельник 10:00 Екб = 08:00 МСК
	now := msk(2026, 11, 6, 19, 0)
	b := &BotService{scheduler: on}
	if got := consentAnsweredText(e, true, &next, now, b.siteFor(e)); !strings.Contains(got, "в понедельник в 10:00") {
		t.Fatalf("согласие с флагом: %q", got)
	}
	b = &BotService{scheduler: off}
	if got := consentAnsweredText(e, true, &next, now, b.siteFor(e)); !strings.Contains(got, "в понедельник в 8:00") {
		t.Fatalf("согласие без флага: %q", got)
	}
	if b := (&BotService{}); b.siteFor(e).TZ != domain.TZMoscow || b.recipient(e).site != nil {
		t.Fatal("без планировщика — площадка")
	}
}

// {start_day} в сообщениях веток и кнопках причин «Чего-то нет» — по поясу новичка с FEATURE_TZ,
// без флага — по площадке.
func TestBranchTextsInNewcomerTZ(t *testing.T) {
	vy := domain.AddressVy
	e := &domain.Employee{Site: domain.SiteGTP, TZ: strp(tzEkb), StartDate: date(2026, 11, 3), AddressForm: &vy}
	at := msk(2026, 11, 2, 23, 30) // 01:30 3 ноября в Екатеринбурге
	ev := &domain.ScheduledEvent{SentAt: &at}
	m := domain.BranchMessage{TextVy: "Ждём вас {start_day}"}
	on, off := &BotService{scheduler: tzScheduler(true)}, &BotService{scheduler: tzScheduler(false)}
	if got := branchText(m, e, ev, on.siteFor(e)); got != "Ждём вас сегодня" {
		t.Fatalf("ветка с флагом: %q", got)
	}
	if got := branchText(m, e, ev, off.siteFor(e)); got != "Ждём вас завтра" {
		t.Fatalf("ветка без флага: %q", got)
	}
	st := domain.ScenarioStep{TextVy: "Выход {start_day}: чего нет?"}
	if got := accessReasonsMessage(on.recipient(e), st, e, ev, at).Text; !strings.HasPrefix(got, "Выход сегодня: чего нет?") {
		t.Fatalf("причины с флагом: %q", got)
	}
	if got := accessReasonsMessage(off.recipient(e), st, e, ev, at).Text; !strings.HasPrefix(got, "Выход завтра: чего нет?") {
		t.Fatalf("причины без флага: %q", got)
	}
}
