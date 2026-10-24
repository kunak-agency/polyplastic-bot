package services

import (
	"fmt"
	"sort"
	"time"

	"main/domain"
	"main/repositories"

	"github.com/google/uuid"
)

// Ранние сигналы зон v2 (п. 29 плана 24.10, FEATURE_RISK_V2; ресерч
// 2026-10-24-sentiment-feedback.md, п. 7). Жёлтая зона с причиной «ранний сигнал: …», если
// других причин нет (красная и прочие жёлтые важнее):
//   - (а) пульс упал на 3 и больше из 10 между двумя последними вехами (шкала 1–5 — ×2, как в
//     pulseValue), последняя — не старше RiskPulseWindow;
//   - (б) тон neg в 2 из 3 последних текстов новичка (ответы опросов своими словами и свободные
//     вопросы) и хотя бы в одном из них — флаг. Флаг в записи — значит уверенность модели не ниже
//     manual_review_floor таксономии (0,4 ≥ 0,3) или regex (уверенность 1): ниже порога код не
//     сохраняется (bot_flags.go), отдельного поля уверенности флага в схеме нет. Тексты — не
//     старше RiskPulseWindow.
// «Один код дважды подряд» (repeat_bump, bot_flags.go) и молчание (правило «не отвечает» выше)
// уже есть и здесь не повторяются.

const (
	earlyPulseDrop = 3.0 // падение пульса между соседними вехами, из 10
	earlyToneLast  = 3   // сколько последних текстов смотрим
	earlyToneNeg   = 2   // сколько из них neg
	earlyPrefix    = "ранний сигнал: "
)

// toneText — размеченный текст новичка без самого текста: тон, флаги, время.
type toneText struct {
	neg   bool
	flags bool
	at    time.Time
}

// toneTexts — тексты новичков с тоном (ответы опросов и свободные вопросы), по времени.
func toneTexts(rows *repositories.OverviewRows) map[uuid.UUID][]toneText {
	out := map[uuid.UUID][]toneText{}
	for _, a := range rows.Answers {
		if a.IsTest || a.Sentiment == nil {
			continue
		}
		out[a.EmployeeID] = append(out[a.EmployeeID], toneText{neg: *a.Sentiment == domain.SentimentNeg, flags: len(a.Flags) > 0, at: a.CreatedAt})
	}
	for _, q := range rows.Questions {
		if q.IsTest || q.Sentiment == nil {
			continue
		}
		out[q.EmployeeID] = append(out[q.EmployeeID], toneText{neg: *q.Sentiment == domain.SentimentNeg, flags: len(q.Flags) > 0, at: q.CreatedAt})
	}
	for id := range out {
		l := out[id]
		sort.SliceStable(l, func(i, j int) bool { return l[i].at.Before(l[j].at) })
	}
	return out
}

// earlySignal — причина «ранний сигнал: …» или "".
func earlySignal(ps []riskPulse, texts []toneText, now time.Time) string {
	ps = append([]riskPulse(nil), ps...)
	sort.SliceStable(ps, func(i, j int) bool { return ps[i].at.Before(ps[j].at) })
	if n := len(ps); n >= 2 && now.Sub(ps[n-1].at) <= RiskPulseWindow && ps[n-2].avg-ps[n-1].avg >= earlyPulseDrop {
		return fmt.Sprintf(earlyPrefix+"пульс упал с %.0f до %.0f из 10", ps[n-2].avg, ps[n-1].avg)
	}
	var fresh []toneText
	for _, t := range texts {
		if now.Sub(t.at) <= RiskPulseWindow && !t.at.After(now) {
			fresh = append(fresh, t)
		}
	}
	texts = fresh
	if len(texts) > earlyToneLast {
		texts = texts[len(texts)-earlyToneLast:]
	}
	neg, flagged := 0, false
	for _, t := range texts {
		if t.neg {
			neg++
		}
		flagged = flagged || t.flags
	}
	if neg >= earlyToneNeg && flagged {
		return fmt.Sprintf(earlyPrefix+"негативный тон в %d из %d последних ответов и тревожная отметка", neg, len(texts))
	}
	return ""
}
