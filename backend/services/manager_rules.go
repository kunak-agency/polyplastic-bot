package services

import (
	"fmt"
	"sort"
	"time"

	"main/domain"
	"main/pkg/taxonomy"
	"main/repositories"

	"github.com/google/uuid"
)

// Правила push руководителю подразделения (кейс, роль 03; UX 1 и 3.7). Решает код, не LLM.
// Руководитель получает только повод встретиться: без текстов ответов, без причин, без
// категорий флагов. Если низкий пульс сопровождается категорией, которую руководителю видеть
// нельзя (F0/F4/F5/F9/F10, taxonomy.yaml manager_visibility=none и never_to_manager), повод
// не отправляется вовсе: иначе push сам указал бы на сигнал (например, на жалобу на самого
// руководителя). Такими случаями занимаются HR-партнёр и СБ.

// Пороги правил.
const (
	// NudgeSilentHours — «не ответил боту больше 3 дней»: опрос ушёл и без ответа ≥ 72 ч.
	NudgeSilentHours = 72
	// NudgeProbationDays — «решение по ИС через 3 дня»: до D+90 осталось 0–3 дня.
	NudgeProbationDays = 3
	// NudgeMonthDay — D+30; push в окне D+30…D+33 (выходные и пропущенные такты).
	NudgeMonthDay    = 30
	nudgeMonthWindow = 3
	// nudgeLowPulseFresh — низкие пульсы старше двух недель повода не дают.
	nudgeLowPulseFresh = 14 * 24 * time.Hour
)

// managerHiddenFallback — коды, которые руководителю не видны, если таксономии нет под рукой.
var managerHiddenFallback = map[string]bool{"F0": true, "F4": true, "F5": true, "F9": true, "F10": true}

// managerHidden — категорию руководителю не показывать (таксономия: ManagerHidden).
func managerHidden(code string, tax *taxonomy.Taxonomy) bool {
	if tax != nil {
		if c, ok := tax.Get(code); ok {
			return c.ManagerHidden
		}
	}
	return managerHiddenFallback[code]
}

// NudgeCandidate — повод написать руководителю.
type NudgeCandidate struct {
	Employee domain.Employee
	Day      int
	Kind     string
	Key      string // идемпотентность: новичок/правило/повод
	Reason   string // коротко, для карточки «Мои новички»
	Text     string // текст push
}

type surveyPulse struct {
	event uuid.UUID
	avg   float64
	at    time.Time
	codes []string
}

// ManagerNudgeCandidates — поводы по всем новичкам в адаптации. Чистая функция: её проверяют
// тесты без базы.
func ManagerNudgeCandidates(rows *repositories.OverviewRows, now time.Time, tax *taxonomy.Taxonomy) []NudgeCandidate {
	if rows == nil {
		return nil
	}
	// Пульс по опросам (как в обзоре): среднее ответов одного события, по времени.
	type acc struct {
		emp   uuid.UUID
		sum   float64
		n     int
		at    time.Time
		codes []string
	}
	byEvent := map[uuid.UUID]*acc{}
	var order []uuid.UUID
	for _, a := range rows.Answers {
		if a.IsTest {
			continue
		}
		x := byEvent[a.EventID]
		if x == nil {
			x = &acc{emp: a.EmployeeID}
			byEvent[a.EventID] = x
			order = append(order, a.EventID)
		}
		x.codes = append(x.codes, a.Flags...)
		if v, ok := pulseValue(a); ok {
			x.sum += v
			x.n++
			if a.CreatedAt.After(x.at) {
				x.at = a.CreatedAt
			}
		}
	}
	pulses := map[uuid.UUID][]surveyPulse{}
	for _, id := range order {
		x := byEvent[id]
		if x.n == 0 {
			continue
		}
		pulses[x.emp] = append(pulses[x.emp], surveyPulse{event: id, avg: x.sum / float64(x.n), at: x.at, codes: x.codes})
	}
	events := map[uuid.UUID][]repositories.OverviewEvent{}
	for _, ev := range rows.Events {
		events[ev.EmployeeID] = append(events[ev.EmployeeID], ev)
	}

	var out []NudgeCandidate
	for _, e := range rows.Employees {
		site := domain.SiteOf(e.Site)
		day := DayNumber(e.StartDate, now, site)
		stage := domain.StageForDay(day)
		if stage == domain.StageDone || day < 0 {
			continue
		}
		name := e.FullName
		add := func(kind, key, reason, text string) {
			out = append(out, NudgeCandidate{Employee: e, Day: day, Kind: kind, Key: kind + ":" + e.ID.String() + ":" + key, Reason: reason, Text: text})
		}

		// 1. Второй низкий пульс подряд (свежие, не скрытые категории).
		if ps := pulses[e.ID]; len(ps) >= 2 {
			sort.SliceStable(ps, func(i, j int) bool { return ps[i].at.Before(ps[j].at) })
			a, b := ps[len(ps)-2], ps[len(ps)-1]
			if a.avg <= LowPulse && b.avg <= LowPulse && now.Sub(b.at) <= nudgeLowPulseFresh && !anyHidden(tax, a.codes, b.codes) {
				add(domain.NudgeLowPulseTwice, b.event.String(), "второй низкий пульс подряд",
					fmt.Sprintf("%s (%s): второй низкий пульс подряд. Рекомендую встретиться 1:1 на этой неделе.", name, dayRu(day)))
			}
		}

		// 2. Не отвечает боту больше 3 дней.
		if e.Connection != domain.ConnStopped {
			var oldest *repositories.OverviewEvent
			for i, ev := range events[e.ID] {
				if ev.IsTest || ev.Kind != domain.KindSurvey || ev.Status != domain.EventSent || ev.SentAt == nil {
					continue
				}
				if now.Sub(*ev.SentAt) >= NudgeSilentHours*time.Hour && (oldest == nil || ev.SentAt.Before(*oldest.SentAt)) {
					oldest = &events[e.ID][i]
				}
			}
			if oldest != nil {
				add(domain.NudgeSilent, oldest.ID.String(), "не отвечает боту больше 3 дней",
					fmt.Sprintf("%s (%s) не отвечает боту больше 3 дней. Встретьтесь 1:1 и узнайте, всё ли в порядке.", name, dayRu(day)))
			}
		}

		// 3. Решение по испытательному сроку через 0–3 дня.
		left := 90 - day
		if left >= 0 && left <= NudgeProbationDays {
			when := "сегодня"
			if left > 0 {
				when = "через " + daysRu(left)
			}
			add(domain.NudgeProbation, "d90", "решение по ИС "+when,
				fmt.Sprintf("%s (%s): решение по испытательному сроку %s. Встретьтесь 1:1 и подведите итоги.", name, dayRu(day), when))
		}

		// 4. D+30 — встреча 1:1.
		if day >= NudgeMonthDay && day <= NudgeMonthDay+nudgeMonthWindow {
			add(domain.NudgeMonth, "d30", "месяц в компании — встреча 1:1",
				fmt.Sprintf("%s: месяц в компании (%s). Самое время встретиться 1:1 — как идут задачи и что мешает.", name, dayRu(day)))
		}
	}
	return out
}

func anyHidden(tax *taxonomy.Taxonomy, lists ...[]string) bool {
	for _, l := range lists {
		for _, c := range l {
			if managerHidden(c, tax) {
				return true
			}
		}
	}
	return false
}

// dayRu — «D+12», «D0».
func dayRu(day int) string {
	switch {
	case day > 0:
		return fmt.Sprintf("D+%d", day)
	case day < 0:
		return fmt.Sprintf("D−%d", -day)
	}
	return "D0"
}
