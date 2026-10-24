package main

import "main/domain"

// SyntheticTone — тон синтетического ответа без текста (детерминированно, без LLM): есть код
// сигнала F0–F9 → neg; высокая оценка (10-балльная ≥ 9, 5-балльная = 5, первый вариант выбора
// «всё хорошо») → pos; иначе neu. То же правило — в cmd/tone-backfill для уже засеянных строк.
// rng генератора не трогает: остальные данные потока от этого не меняются.
func SyntheticTone(a domain.SurveyAnswer) *string {
	if a.Skipped || a.Value == nil {
		return nil
	}
	s := domain.SentimentNeu
	v := *a.Value
	switch {
	case hasNegFlag(a.Flags):
		s = domain.SentimentNeg
	case a.Scale == domain.Scale10 && v >= 9, a.Scale == domain.Scale5 && v == 5, a.Scale == domain.ScaleChoice && v == 1:
		s = domain.SentimentPos
	}
	return &s
}

// hasNegFlag — среди кодов есть F0–F9 (F10 «личное и здоровье» тон не задаёт).
func hasNegFlag(codes domain.StringList) bool {
	for _, c := range codes {
		switch c {
		case "F0", "F1", "F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9":
			return true
		}
	}
	return false
}
