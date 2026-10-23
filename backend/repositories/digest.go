package repositories

import (
	"context"
	"time"

	"main/domain"

	"github.com/google/uuid"
)

// Тексты для кластеризации дайджеста (services/digest_cluster.go). Отдельно от OverviewRows:
// обзор текстов не видит никогда, дайджест берёт их только чтобы сгруппировать по ключевым
// словам и (группа ≥ 5 человек) показать обезличенные примеры в кабинете.

// DigestText — причина, текст или сигнал ответа опроса либо обращения за период.
type DigestText struct {
	EmployeeID uuid.UUID
	FullName   string // для обезличивания: части ФИО самого новичка заменяются заглушкой
	Site       string
	Source     string // answer | ticket
	Reason     string // кнопка причины ответа
	Text       string // текст новичка (ответ «своими словами» или цитата обращения)
	Codes      domain.StringList
	CreatedAt  time.Time
}

// DigestTexts — ответы опросов с причиной, текстом или флагом и обращения за [from, to).
// Тестовые (машина времени) не берутся.
func (r *OverviewRepo) DigestTexts(ctx context.Context, site string, from, to time.Time) ([]DigestText, error) {
	if r == nil {
		return nil, ErrNoDatabase
	}
	db := r.db.WithContext(ctx)
	var answers []DigestText
	q := db.Table("survey_answers a").Joins("JOIN employees e ON e.id = a.employee_id").
		Select("a.employee_id, e.full_name, e.site, 'answer' AS source, a.reason, a.text, a.flags AS codes, a.created_at").
		Where("a.is_test = false AND a.created_at >= ? AND a.created_at < ?", from, to).
		Where("(a.reason <> '' OR a.text <> '' OR jsonb_array_length(COALESCE(a.flags, '[]'::jsonb)) > 0)")
	if site != "" {
		q = q.Where("e.site = ?", site)
	}
	if err := q.Order("a.created_at").Scan(&answers).Error; err != nil {
		return nil, err
	}
	type ticketRow struct {
		EmployeeID uuid.UUID
		FullName   string
		Site       string
		Category   string
		Quote      string
		Restricted bool
		CreatedAt  time.Time
	}
	var tickets []ticketRow
	tq := db.Table("tickets t").Joins("JOIN employees e ON e.id = t.employee_id").
		Select("t.employee_id, e.full_name, e.site, t.category, t.quote, t.restricted, t.created_at").
		Where("t.is_test = false AND t.created_at >= ? AND t.created_at < ?", from, to)
	if site != "" {
		tq = tq.Where("e.site = ?", site)
	}
	if err := tq.Order("t.created_at").Scan(&tickets).Error; err != nil {
		return nil, err
	}
	for _, t := range tickets {
		quote := t.Quote
		if t.Restricted {
			quote = "" // F5/F9: текст в аналитику не идёт вовсе
		}
		answers = append(answers, DigestText{
			EmployeeID: t.EmployeeID, FullName: t.FullName, Site: t.Site, Source: "ticket",
			Text: quote, Codes: domain.StringList{t.Category}, CreatedAt: t.CreatedAt,
		})
	}
	return answers, nil
}
