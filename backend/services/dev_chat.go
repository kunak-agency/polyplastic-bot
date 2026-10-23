package services

import "main/externalApi/max"

// FakeMax — подделка MAX, если бот работает на ней (development без MAX_BOT_TOKEN); иначе nil.
// Нужна только демо-чату (GET /api/dev/chat/{id}): его журнал есть лишь у подделки.
func (s *Services) FakeMax() *max.Fake {
	if s.ext == nil {
		return nil
	}
	f, _ := s.ext.Max.(*max.Fake)
	return f
}
