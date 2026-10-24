package out

import (
	"time"

	"main/services"
)

// NewcomerScheduleOut — поля карточки новичка про расписание (api_contract.md, «Пояса» и «Наджи»).
type NewcomerScheduleOut struct {
	// TZ — свой пояс новичка (IANA), null — пояс площадки; SiteTZ — пояс площадки
	// (api_contract_auth.md, «Пояс»). EffectiveTZ — пояс, по которому новичку на самом деле уходят
	// шаги («10:00» шага — 10:00 в нём): с FEATURE_TZ — tz ?? site_tz, без флага — site_tz.
	TZ          *string `json:"tz"`
	SiteTZ      string  `json:"site_tz"`
	EffectiveTZ string  `json:"effective_tz"`
	// UnresponsiveSince — «не отвечает» с этого момента (после второго автонапоминания); null —
	// отвечает.
	UnresponsiveSince *string `json:"unresponsive_since"`
	// Catchup — догоняющая очередь позднего входа; null — не собиралась.
	Catchup *CatchupOut `json:"catchup"`
}

// CatchupOut — «Догоняет: done из total» / «Догнал ✓» (finished).
type CatchupOut struct {
	Done     int  `json:"done"`
	Total    int  `json:"total"`
	Finished bool `json:"finished"`
}

func scheduleOut(c services.NewcomerCard) NewcomerScheduleOut {
	o := NewcomerScheduleOut{TZ: c.Employee.TZ, SiteTZ: c.SiteTZ, EffectiveTZ: c.TZ}
	if c.Unresponsive != nil {
		s := zoned(*c.Unresponsive, c.Employee.Site)
		o.UnresponsiveSince = &s
	}
	if c.Catchup != nil {
		o.Catchup = &CatchupOut{Done: c.Catchup.Done, Total: c.Catchup.Total, Finished: c.Catchup.Finished}
	}
	return o
}

// Nudge — строка истории напоминаний (GET /api/newcomers/{id}/nudges).
type Nudge struct {
	ID         string  `json:"id"`
	EventID    *string `json:"event_id"`
	EventTitle string  `json:"event_title"`
	Kind       string  `json:"kind"` // auto | manual
	N          int     `json:"n"`    // номер напоминания по этому вопросу
	// At — когда ушло; у planned — когда уйдёт. В поясе новичка (TZ ответа).
	At        string  `json:"at"`
	Planned   bool    `json:"planned"`   // ждёт рабочего окна новичка
	Delivered bool    `json:"delivered"` // ушло в MAX
	CreatedBy *string `json:"created_by"`
}

// NudgeList — история и пояс новичка.
type NudgeList struct {
	List[Nudge]
	TZ string `json:"tz"`
}

// NudgesFrom — история напоминаний в поясе tz.
func NudgesFrom(items []services.NudgeItem, tz string) NudgeList {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	out := make([]Nudge, len(items))
	for i, it := range items {
		n := Nudge{ID: it.ID.String(), EventTitle: it.EventTitle, Kind: it.Kind, N: it.N,
			At: it.SentAt.In(loc).Format(time.RFC3339), Planned: it.Planned, Delivered: it.Delivered}
		if it.EventID != nil {
			s := it.EventID.String()
			n.EventID = &s
		}
		if it.CreatedBy != nil {
			s := it.CreatedBy.String()
			n.CreatedBy = &s
		}
		out[i] = n
	}
	return NudgeList{List: NewList(out), TZ: tz}
}

// NudgeResult — ответ POST /api/newcomers/{id}/nudge.
type NudgeResult struct {
	Status  string `json:"status"` // sent | planned | failed
	At      string `json:"at"`     // когда ушло или уйдёт, в поясе новичка
	TZ      string `json:"tz"`
	Message string `json:"message"` // «Напоминание отправлено» / «Отправится в 08:00 по времени новичка»
	Nudge   Nudge  `json:"nudge"`
}

// NudgeResultFrom — итог «Напомнить».
func NudgeResultFrom(r services.NudgeResult) NudgeResult {
	status := "sent"
	switch {
	case r.Nudge.MessageID == "":
		status = "planned"
	case !r.Sent:
		status = "failed"
	}
	item := services.NudgeItem{Nudge: r.Nudge, Planned: status == "planned", Delivered: r.Sent}
	l := NudgesFrom([]services.NudgeItem{item}, r.TZ)
	return NudgeResult{Status: status, At: l.Items[0].At, TZ: r.TZ, Message: r.Message, Nudge: l.Items[0]}
}
