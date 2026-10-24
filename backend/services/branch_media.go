package services

import (
	"context"
	"strings"

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
	s.Scenarios.onImport = func(ctx context.Context, imp *tzImport) {
		names := imp.mediaNames()
		if len(names) == 0 {
			return
		}
		if _, err := m.Ensure(ctx, names, "", "импорт ТЗ"); err != nil {
			logger.Warn("импорт: заглушки медиатеки не созданы: %v", err)
		}
	}
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

// mediaNames — все имена медиатеки импорта: вложения шагов, веток кнопок и вариантов опроса,
// правил после опроса, send_file, команд и глобальных правил (личные файлы — нет). Импорт
// заводит по ним заглушки: в кабинете «нет файла», а не «нет в медиатеке».
func (imp *tzImport) mediaNames() []string {
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		if name == "" || seen[name] || strings.HasPrefix(name, "personal:") {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	refs := func(l domain.MediaRefList) {
		for _, r := range l {
			if r.Personal == "" {
				add(r.Name)
			}
		}
	}
	acts := func(l []domain.Action) {
		for _, a := range l {
			if a.Type == domain.ActionSendFile {
				add(a.Media)
			}
		}
	}
	var branch func(br *domain.Branch)
	buttons := func(l []domain.StepButton) {
		for _, b := range l {
			if b.Branch != nil {
				branch(b.Branch)
			}
		}
	}
	branch = func(br *domain.Branch) {
		acts(br.Actions)
		for _, m := range br.Messages {
			refs(m.Media)
			buttons(m.Buttons)
		}
	}
	for _, st := range imp.steps {
		refs(st.Media)
		buttons(st.Buttons)
		if st.Survey != nil {
			for _, q := range st.Survey.Questions {
				for _, k := range sortedKeys(q.Branches) {
					branch(q.Branches[k])
				}
			}
			for _, o := range st.Survey.Outcomes {
				branch(&domain.Branch{Messages: o.Messages, Actions: o.Actions})
			}
		}
	}
	for _, c := range imp.commands {
		acts(c.Actions)
	}
	for _, r := range imp.rules {
		refs(r.Media)
		buttons(r.Buttons)
	}
	return out
}
