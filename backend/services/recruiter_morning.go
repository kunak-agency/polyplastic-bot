package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"main/domain"
	"main/externalApi/max"
	"main/pkg/logger"
	"main/pkg/taxonomy"
	"main/repositories"

	"github.com/google/uuid"
)

// Утренняя сводка рекрутеру (FEATURE_RISK_V2; ресерч 2026-10-24-role-dashboards.md, раздел 5): в
// 9:00 по поясу рекрутера (User.TZ, иначе Москва), в будни, если среди его новичков
// (Employee.RecruiterID) есть красные зоны v2 — одно сообщение в MAX «Доброе утро! Сегодня: … — N»
// с кнопкой в кабинет. Нужна привязка users.max_user_id; без неё — ничего. Окно 9:00–12:00: после
// рестарта днём сводка не догоняет. Один раз в день — Key в manager_nudges (журнал push).

const (
	morningHour    = 9
	morningUntil   = 12
	morningTickDur = 5 * time.Minute
)

// RecruiterMorningService — утренняя сводка рекрутеру.
type RecruiterMorningService struct {
	repos   *repositories.Repos
	max     max.Client
	tax     *taxonomy.Taxonomy
	cab     string
	now     func() time.Time
	enabled bool
}

// RecruiterMorning — сервис сводки (по образцу Manager: без поля в Services).
func (s *Services) RecruiterMorning() *RecruiterMorningService {
	o := s.Overview()
	m := &RecruiterMorningService{repos: s.repos, tax: o.tax, now: o.now, cab: cabinetBase(s.digestOpts.CabinetURL), enabled: s.features.RiskV2}
	if s.ext != nil {
		m.max = s.ext.Max
	}
	return m
}

// MorningSent — сводка, отправленная тактом.
type MorningSent struct {
	UserID uuid.UUID
	Text   string
}

// Tick — сводки тем, у кого сейчас 9:00–12:00 будня и есть красные (force — без окна, dev).
func (s *RecruiterMorningService) Tick(ctx context.Context, force bool) ([]MorningSent, error) {
	if !s.enabled || s.max == nil {
		return nil, nil
	}
	if !s.repos.Enabled() {
		return nil, repositories.ErrNoDatabase
	}
	users, err := s.repos.Users.WithMax(ctx, domain.RoleRecruiter)
	if err != nil || len(users) == 0 {
		return nil, err
	}
	now := s.now()
	type due struct {
		u   domain.User
		key string
	}
	var todo []due
	sent, err := s.repos.Manager().NudgeKeys(ctx)
	if err != nil {
		return nil, err
	}
	for _, u := range users {
		local := now.In(userLoc(u))
		if !force && (local.Weekday() == time.Saturday || local.Weekday() == time.Sunday ||
			local.Hour() < morningHour || local.Hour() >= morningUntil) {
			continue
		}
		key := domain.NudgeRecruiterMorning + ":" + u.ID.String() + ":" + local.Format("2006-01-02")
		if !sent[key] {
			todo = append(todo, due{u, key})
		}
	}
	if len(todo) == 0 {
		return nil, nil
	}
	rows, err := s.repos.Overview().Load(ctx, "", now.Add(-60*24*time.Hour))
	if err != nil {
		return nil, err
	}
	owner := map[uuid.UUID]*uuid.UUID{}
	for _, e := range rows.Employees {
		owner[e.ID] = e.RecruiterID
	}
	zones := RiskZonesV2(rows, now, s.tax)
	var out []MorningSent
	for _, d := range todo {
		var reds []RiskZone
		for _, z := range zones {
			if r := owner[z.NewcomerID]; z.Zone == domain.RiskRed && r != nil && *r == d.u.ID {
				reds = append(reds, z)
			}
		}
		if len(reds) == 0 {
			continue // красных нет — сводки нет; появятся до полудня — уйдёт
		}
		text := RecruiterMorningText(reds)
		n := &domain.ManagerNudge{ManagerUserID: *d.u.MaxUserID, Kind: domain.NudgeRecruiterMorning, Key: d.key, Text: text, Status: domain.NudgeMet}
		n.CreatedAt = now
		created, err := s.repos.Manager().CreateNudge(ctx, n)
		if err != nil {
			return out, err
		}
		if !created {
			continue
		}
		mid, err := s.max.Send(ctx, max.OutMessage{UserID: *d.u.MaxUserID, Text: text,
			Buttons: [][]max.Button{{{Text: "Открыть кабинет", URL: s.cab + "/dashboard"}}}})
		if err != nil {
			logger.Warn("утренняя сводка: рекрутеру %s не ушла: %v", d.u.ID, err)
			continue
		}
		t := s.now()
		n.MessageID, n.SentAt = mid, &t
		if err := s.repos.Manager().SaveNudge(ctx, n); err != nil {
			logger.Warn("утренняя сводка %s не сохранена: %v", n.ID, err)
		}
		out = append(out, MorningSent{UserID: d.u.ID, Text: text})
	}
	return out, nil
}

// Run — такт сводки; без флага сразу выходит.
func (s *RecruiterMorningService) Run(ctx context.Context) {
	if !s.enabled {
		return
	}
	logger.Info("утренняя сводка рекрутеру: такт %v", morningTickDur)
	t := time.NewTicker(morningTickDur)
	defer t.Stop()
	for {
		if list, err := s.Tick(ctx, false); err != nil && ctx.Err() == nil {
			logger.Warn("утренняя сводка: такт не удался: %v", err)
		} else if len(list) > 0 {
			logger.Info("утренняя сводка: отправлено %d", len(list))
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// userLoc — пояс пользователя; пусто или неизвестный — Москва.
func userLoc(u domain.User) *time.Location {
	if u.TZ != nil && *u.TZ != "" {
		if loc, err := time.LoadLocation(*u.TZ); err == nil {
			return loc
		}
	}
	return domain.SiteOf("").Loc
}

// morningGroups — группы главных причин красной зоны в порядке сводки.
var morningGroups = []struct{ prefix, label string }{
	{"срочное обращение", "срочные обращения"},
	{"не открыл(а) приглашение", "не открыли приглашение"},
	{"не отвечает", "не отвечают после напоминаний"},
	{"два низких пульса", "два низких пульса подряд"},
}

// RecruiterMorningText — «Доброе утро! Сегодня: срочные обращения — 1, сигналы в ответах — 2.»
// Только счётчики по группам: без имён и текстов, в MAX это видит любой с телефоном рекрутера.
func RecruiterMorningText(reds []RiskZone) string {
	counts := map[string]int{}
	other := 0
	for _, z := range reds {
		reason := ""
		if len(z.Reasons) > 0 {
			reason = z.Reasons[0]
		}
		hit := false
		for _, g := range morningGroups {
			if strings.HasPrefix(reason, g.prefix) {
				counts[g.label]++
				hit = true
				break
			}
		}
		if !hit {
			other++
		}
	}
	var parts []string
	for _, g := range morningGroups {
		if n := counts[g.label]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s — %d", g.label, n))
		}
	}
	if other > 0 {
		parts = append(parts, fmt.Sprintf("сигналы в ответах — %d", other))
	}
	return "Доброе утро! Сегодня: " + strings.Join(parts, ", ") + "."
}
