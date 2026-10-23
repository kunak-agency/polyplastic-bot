package services

import (
	"context"

	"main/domain"
	"main/externalApi/max"
	"main/pkg/logger"
)

// Вложения шагов и веток через медиатеку (MediaAttacher — media_max.go).

// SetMedia — подключить медиатеку (и обращения — задачи HR о недостающих личных файлах) к
// боту и планировщику.
func (s *Services) SetMedia(m *MediaService) {
	s.Scheduler.tickets = s.Tickets
	s.Scenarios.onPublish = s.SyncBotCommands
	if m == nil {
		return
	}
	s.Scheduler.media = m
	if s.Bot != nil {
		s.Bot.media = m
	}
}

func mediaAttachments(ctx context.Context, m MediaAttacher, refs []domain.MediaRef, site string) []max.AttachmentRef {
	if len(refs) == 0 {
		return nil
	}
	if m == nil {
		logger.Warn("вложения: медиатека не подключена — %d вложений не отправлено", len(refs))
		return nil
	}
	out, warns := m.Attachments(ctx, refs, site)
	for _, w := range warns {
		logger.Warn("вложения: %s", w)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
