package services

import (
	"time"

	"main/domain"
	"main/pkg/taxonomy"
)

// SLA обращения (api_contract.md, v2): P0 — 1 рабочий час, P1 — 1 рабочий день, P2 — 3
// рабочих дня. Рабочее время — часы площадки (пн–чт 9–18, пт 9–17). Таймер стартует при OPEN;
// создано вне рабочего времени — счёт идёт с начала ближайшего рабочего дня.

// SLADue — срок ответа по приоритету.
func SLADue(created time.Time, priority string, site domain.Site) time.Time {
	start := workStart(created, site)
	switch priority {
	case taxonomy.P0:
		return addWorkMinutes(start, 60, site)
	case taxonomy.P2:
		return addWorkdays(start, 3, site)
	}
	return addWorkdays(start, 1, site)
}

// InWorkHours — сейчас рабочее время площадки (для текста OPEN «нерабочее время»).
func InWorkHours(now time.Time, site domain.Site) bool {
	t := now.In(site.Loc)
	wd := t.Weekday()
	if !site.IsWorkday(wd) {
		return false
	}
	m := t.Hour()*60 + t.Minute()
	return m >= site.DayStart[wd] && m < site.DayEnd[wd]
}

// workStart — t, если оно в рабочем времени; иначе начало ближайшего рабочего дня.
func workStart(t time.Time, site domain.Site) time.Time {
	t = t.In(site.Loc)
	day := civilDate(t, site.Loc)
	m := t.Hour()*60 + t.Minute()
	if site.IsWorkday(day.Weekday()) {
		if m < site.DayStart[day.Weekday()] {
			return atMinutes(day, site.DayStart[day.Weekday()], site.Loc)
		}
		if m < site.DayEnd[day.Weekday()] {
			return t
		}
	}
	next := nextWorkday(day.AddDate(0, 0, 1), site)
	return atMinutes(next, site.DayStart[next.Weekday()], site.Loc)
}

// addWorkMinutes — плюс n минут рабочего времени (переходит через вечер и выходные).
func addWorkMinutes(t time.Time, n int, site domain.Site) time.Time {
	for n > 0 {
		day := civilDate(t, site.Loc)
		end := atMinutes(day, site.DayEnd[day.Weekday()], site.Loc)
		left := int(end.Sub(t).Minutes())
		if n <= left {
			return t.Add(time.Duration(n) * time.Minute)
		}
		n -= left
		next := nextWorkday(day.AddDate(0, 0, 1), site)
		t = atMinutes(next, site.DayStart[next.Weekday()], site.Loc)
	}
	return t
}

// addWorkdays — то же время через n рабочих дней; в короткий день (пятница) — не позже его конца.
func addWorkdays(t time.Time, n int, site domain.Site) time.Time {
	day := civilDate(t, site.Loc)
	m := t.Hour()*60 + t.Minute()
	for i := 0; i < n; i++ {
		day = nextWorkday(day.AddDate(0, 0, 1), site)
	}
	if hi := site.DayEnd[day.Weekday()]; m > hi {
		m = hi
	}
	return atMinutes(day, m, site.Loc)
}
