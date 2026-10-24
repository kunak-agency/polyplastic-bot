package services

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"main/domain"
	"main/pkg/taxonomy"
	"main/repositories"

	"github.com/google/uuid"
)

// Зоны риска v2 (FEATURE_RISK_V2; ресерч 2026-10-24-role-dashboards.md, раздел 0). У новичка
// одна зона и одна главная причина словами. Красная — нужно действие сегодня, жёлтая —
// наблюдать. Не зона: обращение P1/P2 в пределах SLA, обращение в работе у человека, опрос без
// ответа меньше 48 ч. Без флага обзор считает зоны по прежним правилам (overview.go).

// Пороги v2.
const (
	// RiskPulseWindow — низкие пульсы старше двух недель зону не дают.
	RiskPulseWindow = 14 * 24 * time.Hour
	// RiskSilentYellowWD, RiskSilentRedWD — «не отвечает»: 2–4 рабочих дня — жёлтая, от 5 после
	// двух напоминаний (Employee.UnresponsiveSince) — красная.
	RiskSilentYellowWD = 2
	RiskSilentRedWD    = 5
	// RiskCatchupBehindDays — догоняет и отстаёт на 2 дня и больше.
	RiskCatchupBehindDays = 2
	// RiskInviteRedDay — приглашение не открыто, а выход завтра (D−1) или уже прошёл.
	RiskInviteRedDay = -1
	// riskFailedAttempts — «сообщение дважды не доставлено».
	riskFailedAttempts = 2
)

type riskPulse struct {
	avg float64
	at  time.Time
}

// RiskZonesV2 — зоны риска v2 по новичкам в адаптации (без завершивших). Чистая функция.
func RiskZonesV2(rows *repositories.OverviewRows, now time.Time, tax *taxonomy.Taxonomy) []RiskZone {
	if rows == nil {
		return nil
	}
	// Пульс по опросам: среднее ответов одного события и время последнего ответа.
	type acc struct {
		emp uuid.UUID
		sum float64
		n   int
		at  time.Time
	}
	byEvent := map[uuid.UUID]*acc{}
	var evOrder []uuid.UUID
	for _, a := range rows.Answers {
		if a.IsTest {
			continue
		}
		v, ok := pulseValue(a)
		if !ok {
			continue
		}
		x := byEvent[a.EventID]
		if x == nil {
			x = &acc{emp: a.EmployeeID}
			byEvent[a.EventID] = x
			evOrder = append(evOrder, a.EventID)
		}
		x.sum += v
		x.n++
		if a.CreatedAt.After(x.at) {
			x.at = a.CreatedAt
		}
	}
	pulses := map[uuid.UUID][]riskPulse{}
	for _, id := range evOrder {
		x := byEvent[id]
		pulses[x.emp] = append(pulses[x.emp], riskPulse{avg: x.sum / float64(x.n), at: x.at})
	}
	events := map[uuid.UUID][]repositories.OverviewEvent{}
	for _, ev := range rows.Events {
		if !ev.IsTest {
			events[ev.EmployeeID] = append(events[ev.EmployeeID], ev)
		}
	}
	texts := toneTexts(rows)
	tickets := map[uuid.UUID][]repositories.OverviewTicket{}
	for _, t := range rows.Tickets {
		if !t.IsTest {
			tickets[t.EmployeeID] = append(tickets[t.EmployeeID], t)
		}
	}

	var out []RiskZone
	for _, e := range rows.Employees {
		site := domain.SiteOf(e.Site)
		day := DayNumber(e.StartDate, now, site)
		stage := domain.StageForDay(day)
		if stage == domain.StageDone {
			continue
		}
		r := riskOfV2(e, day, site, pulses[e.ID], events[e.ID], tickets[e.ID], now, tax)
		if r.zone == "" {
			if reason := earlySignal(pulses[e.ID], texts[e.ID], now); reason != "" {
				r.zone, r.reason = domain.RiskYellow, reason
			}
		}
		if r.zone == "" {
			continue
		}
		out = append(out, RiskZone{
			NewcomerID: e.ID, FullName: e.FullName, Site: e.Site, Day: day, Stage: stage,
			Zone: r.zone, Reasons: []string{r.reason}, Overdue: r.overdue, LastPulse: e.LastPulse, OpenTickets: r.open,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Zone != out[j].Zone {
			return out[i].Zone == domain.RiskRed
		}
		return out[i].Day > out[j].Day
	})
	return out
}

// riskV2 — зона новичка, главная причина и счётчики для строки.
type riskV2 struct {
	zone    string
	reason  string
	overdue bool
	open    int
}

// riskOfV2 — правила раздела 0 по порядку важности: первая сработавшая красная причина, иначе
// первая жёлтая.
func riskOfV2(e domain.Employee, day int, site domain.Site, ps []riskPulse, evs []repositories.OverviewEvent,
	ts []repositories.OverviewTicket, now time.Time, tax *taxonomy.Taxonomy) riskV2 {
	var r riskV2
	var red, yellow []string

	// Обращения: P0 без ответа человека — красная; P1/P2 с просроченным SLA — жёлтая; в работе у
	// человека или в пределах SLA — не зона.
	for _, t := range ts {
		if !isOpenTicket(t) {
			continue
		}
		r.open++
		topic := ticketTopic(t.Category, tax)
		switch {
		case t.Priority == taxonomy.P0 && t.Status != domain.TicketHumanActive:
			red = append(red, "срочное обращение «"+topic+"» ждёт ответа")
		case slaRunning(t) && t.SLADueAt.Before(now):
			yellow = append(yellow, "обращение «"+topic+"» просрочено по SLA")
		}
	}

	// Отметка бота: F7 при уверенности не ниже порога и критические флаги — красная; флаг ниже
	// порога автоэскалации, «отключил бота» — жёлтая. Жёлтая «оценка … в …» (оценка ниже порога
	// опроса, bot.go) в v2 не считается: её заменяет правило пульса за 14 дней ниже — отметка
	// бота не стареет, и одна давняя оценка 7/10 держала бы новичка в зоне до конца адаптации.
	if e.Risk == domain.RiskRed || (e.Risk == domain.RiskYellow && !lowScoreMark(e.RiskReason)) {
		reason := "риск отмечен ботом"
		if e.RiskReason != nil && *e.RiskReason != "" {
			reason = *e.RiskReason
		}
		if e.Risk == domain.RiskRed {
			red = append(red, reason)
		} else {
			yellow = append(yellow, reason)
		}
	}

	// Пульс: два низких подряд за 14 дней — красная, один за 14 дней — жёлтая.
	sort.SliceStable(ps, func(i, j int) bool { return ps[i].at.Before(ps[j].at) })
	fresh := func(p riskPulse) bool { return now.Sub(p.at) <= RiskPulseWindow }
	if n := len(ps); n >= 2 && ps[n-1].avg <= LowPulse && ps[n-2].avg <= LowPulse && fresh(ps[n-1]) && fresh(ps[n-2]) {
		red = append(red, "два низких пульса подряд")
	} else {
		for _, p := range ps {
			if p.avg <= LowPulse && fresh(p) {
				yellow = append(yellow, fmt.Sprintf("низкий пульс %.0f/10", p.avg))
				break
			}
		}
	}

	// Приглашение не открыто, а выход завтра или уже прошёл.
	if e.Connection == domain.ConnInvited && day >= RiskInviteRedDay {
		if day < 0 {
			red = append(red, "не открыл(а) приглашение, выход завтра")
		} else {
			red = append(red, "не открыл(а) приглашение, уже вышел(ла)")
		}
	}

	if e.Connection != domain.ConnStopped {
		// «Не отвечает»: от самого старого опроса без ответа дольше 48 ч, ушедшего после последней
		// активности новичка (ответил на что-то позже — он на связи, пропущенный опрос — не
		// молчание), или от статуса после двух напоминаний — что раньше. Меньше 48 ч — не зона.
		var since *time.Time
		for _, ev := range evs {
			if ev.Kind == domain.KindSurvey && ev.Status == domain.EventSent && ev.SentAt != nil &&
				(e.LastActivityAt == nil || ev.SentAt.After(*e.LastActivityAt)) &&
				now.Sub(*ev.SentAt) >= SurveyOverdueHours*time.Hour && (since == nil || ev.SentAt.Before(*since)) {
				since = ev.SentAt
				r.overdue = true
			}
		}
		if e.UnresponsiveSince != nil && (since == nil || e.UnresponsiveSince.Before(*since)) &&
			now.Sub(*e.UnresponsiveSince) >= SurveyOverdueHours*time.Hour {
			since = e.UnresponsiveSince
		}
		if since != nil {
			wd := riskWorkdays(*since, now, site)
			switch {
			case wd >= RiskSilentRedWD && e.UnresponsiveSince != nil:
				red = append(red, fmt.Sprintf("не отвечает %s после двух напоминаний", workdaysRu(wd)))
			case wd >= RiskSilentYellowWD:
				yellow = append(yellow, "не отвечает "+workdaysRu(wd))
			}
		}

		// Сообщение дважды не доставлено.
		for _, ev := range evs {
			if ev.Status == domain.EventFailed || (ev.Attempts >= riskFailedAttempts && ev.Status == domain.EventScheduled) {
				yellow = append(yellow, fmt.Sprintf("сообщение «%s» не доставлено", ev.Title))
				r.overdue = true
				break
			}
		}

		// Догоняет и отстаёт: очередь — один блок в день, за N дней ожидалось N+1 ответов.
		if e.CatchupStartedAt != nil && e.CatchupDoneAt == nil {
			total, done := 0, 0
			for _, ev := range evs {
				if !ev.Catchup {
					continue
				}
				total++
				if ev.Status == domain.EventAnswered || ev.Status == domain.EventSkipped {
					done++
				}
			}
			expected := int(now.Sub(*e.CatchupStartedAt).Hours()/24) + 1
			if expected > total {
				expected = total
			}
			if behind := expected - done; behind >= RiskCatchupBehindDays {
				yellow = append(yellow, fmt.Sprintf("догоняет %d из %d, отстаёт на %s", done, total, daysRu(behind)))
			}
		}
	}

	switch {
	case len(red) > 0:
		r.zone, r.reason = domain.RiskRed, red[0]
	case len(yellow) > 0:
		r.zone, r.reason = domain.RiskYellow, yellow[0]
	}
	return r
}

// riskReasonLowScore — начало причины жёлтой отметки за низкую оценку (bot.go, seed-demo).
const riskReasonLowScore = "оценка "

// lowScoreMark — жёлтая отметка бота поставлена за низкую оценку опроса.
func lowScoreMark(reason *string) bool {
	return reason != nil && strings.HasPrefix(*reason, riskReasonLowScore)
}

// riskWorkdays — сколько рабочих дней площадки прошло после from до now (день from не считается).
func riskWorkdays(from, now time.Time, site domain.Site) int {
	d := civilDate(from.In(site.Loc), site.Loc).AddDate(0, 0, 1)
	end := civilDate(now.In(site.Loc), site.Loc)
	n := 0
	for i := 0; !d.After(end) && i < 400; i++ {
		if site.IsWorkDate(d) {
			n++
		}
		d = d.AddDate(0, 0, 1)
	}
	return n
}

// workdaysRu — «1 рабочий день», «3 рабочих дня», «5 рабочих дней».
func workdaysRu(n int) string {
	return plural(n, "рабочий день", "рабочих дня", "рабочих дней")
}
