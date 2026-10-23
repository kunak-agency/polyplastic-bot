package services

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"main/domain"
	"main/externalApi/max"
	"main/pkg/errs"
	"main/pkg/flags"
	"main/pkg/logger"
	"main/pkg/taxonomy"
	"main/repositories"

	"github.com/google/uuid"
)

// Ввод от новичка (шаг kind=input, ТЗ r34), личный файл из карточки (media personal, ТЗ r36) и
// команды сценария (/food, /hr; ТЗ r25, r49).

const (
	textInputThanks  = "Спасибо! Сохранил и передал HR."
	textInputNoText  = "Ответ получен (без согласия на обработку сообщений текст не сохраняется)."
	maxPersonalBytes = 20 << 20
)

// attachDeps — откуда берутся вложения: медиатека, личные файлы (БД + загрузка в MAX), задача
// HR, если личного файла нет.
type attachDeps struct {
	media   MediaAttacher
	repos   *repositories.Repos
	max     max.Client
	tickets *TicketService
}

// attachRefs — вложения по ссылкам: личные файлы карточки и медиатека. missing — личные файлы,
// которых нет (HR получает задачу).
func attachRefs(ctx context.Context, d attachDeps, e *domain.Employee, refs []domain.MediaRef, site string) (atts []max.AttachmentRef, missing []string) {
	var lib []domain.MediaRef
	for _, r := range refs {
		if r.Personal == "" {
			lib = append(lib, r)
			continue
		}
		a, ok := personalAttachment(ctx, d, e, r.Personal)
		if !ok {
			missing = append(missing, r.Personal)
			continue
		}
		atts = append(atts, a)
	}
	atts = append(atts, mediaAttachments(ctx, d.media, lib, site)...)
	return atts, missing
}

// personalAttachment — личный файл: токен из кэша или загрузка в MAX.
func personalAttachment(ctx context.Context, d attachDeps, e *domain.Employee, name string) (max.AttachmentRef, bool) {
	if !d.repos.Enabled() {
		return max.AttachmentRef{}, false
	}
	f, err := d.repos.Employees.File(ctx, e.ID, name)
	if err != nil || f == nil {
		if err != nil {
			logger.Warn("личный файл %s новичка %s не прочитан: %v", name, e.ID, err)
		}
		return max.AttachmentRef{}, false
	}
	kind := max.UploadFile
	if strings.HasPrefix(f.Mime, "image/") {
		kind = max.UploadImage
	}
	if f.MaxToken != "" {
		return max.AttachmentRef{Kind: kind, Token: f.MaxToken}, true
	}
	if d.max == nil {
		return max.AttachmentRef{}, false
	}
	tok, err := d.max.Upload(ctx, kind, bytes.NewReader(f.Data), f.Filename, f.Size)
	if err != nil {
		logger.Warn("личный файл %s новичка %s не загружен в MAX: %v", name, e.ID, err)
		return max.AttachmentRef{}, false
	}
	if err := d.repos.Employees.SetFileToken(ctx, f.ID, tok); err != nil {
		logger.Warn("токен личного файла %s не сохранён: %v", name, err)
	}
	return max.AttachmentRef{Kind: kind, Token: tok}, true
}

// personalMissingTask — личного файла нет: задача HR обращением (в HR-чат), новичку — «Плана
// ещё нет».
func personalMissingTask(ctx context.Context, t *TicketService, e *domain.Employee, name, where string) {
	if t == nil || !t.repos.Enabled() {
		return
	}
	var tk *domain.Ticket
	var created bool
	what := fmt.Sprintf("Нет личного файла «%s» (%s): загрузите его в карточку новичка — бот пришлёт.", name, where)
	err := t.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		var err error
		tk, created, err = t.open(ctx, tx, e, ticketDraft{cat: taxonomy.Question(taxonomy.RouteHR), source: domain.TicketFromOther, what: what})
		if err != nil || created {
			return err
		}
		// открытое обращение HR уже есть — задача дописывается в него
		return tx.Tickets.AddMessage(ctx, &domain.TicketMessage{TicketID: tk.ID, From: domain.TicketMsgBot, Text: what})
	})
	if err != nil {
		logger.Warn("задача HR о файле %s новичка %s не создана: %v", name, e.ID, err)
		return
	}
	if created {
		t.notify(ctx, tk, e)
	}
}

// attachments — вложения шага (планировщик).
func (s *SchedulerService) attachFor(ctx context.Context, e *domain.Employee, refs domain.MediaRefList, title string) ([]max.AttachmentRef, bool) {
	atts, missing := attachRefs(ctx, attachDeps{media: s.media, repos: s.repos, max: s.max, tickets: s.tickets}, e, refs, e.Site)
	for _, name := range missing {
		personalMissingTask(ctx, s.tickets, e, name, "шаг «"+title+"»")
	}
	return atts, len(missing) > 0
}

// attach — вложения сообщения ветки (бот).
func (b *BotService) attach(ctx context.Context, e *domain.Employee, refs domain.MediaRefList) []max.AttachmentRef {
	atts, missing := attachRefs(ctx, attachDeps{media: b.media, repos: b.repos, max: b.max, tickets: b.tickets}, e, refs, e.Site)
	for _, name := range missing {
		personalMissingTask(ctx, b.tickets, e, name, "ветка")
		b.send(ctx, recipientOf(e).msg(textPlanMissing, "", nil))
	}
	return atts
}

// UploadNewcomerFile — POST /api/newcomers/{id}/files: личный файл в карточку (name —
// adaptation_plan). Тот же name — замена.
func (s *Services) UploadNewcomerFile(ctx context.Context, id uuid.UUID, name, filename, mime string, data []byte) (*domain.NewcomerFile, error) {
	if !s.repos.Enabled() {
		return nil, repositories.ErrNoDatabase
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = domain.PersonalAdaptationPlan
	}
	if len(data) == 0 || len(data) > maxPersonalBytes || utf8.RuneCountInString(name) > 64 {
		return nil, errs.NewValidationError("Файл пустой, больше 20 МБ или имя длиннее 64 символов")
	}
	e, err := s.repos.Employees.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, errs.NewNotFoundError("Новичок не найден")
	}
	f := &domain.NewcomerFile{EmployeeID: id, Name: name, Filename: filename, Mime: mime, Size: int64(len(data)), Data: data}
	if err := s.repos.Employees.UpsertFile(ctx, f); err != nil {
		return nil, err
	}
	logger.Info("личный файл %s новичка %s загружен (%d байт)", name, id, len(data))
	f.Data = nil
	return f, nil
}

// SetNewcomerFlag — POST /api/newcomers/{id}/flags: флаг новичка («по отмашке рекрутера»).
// Включение отпускает шаги flag_required, если их время пришло.
func (s *Services) SetNewcomerFlag(ctx context.Context, id uuid.UUID, flag string, value bool) (domain.BoolMap, error) {
	if !s.repos.Enabled() {
		return nil, repositories.ErrNoDatabase
	}
	flag = strings.TrimSpace(flag)
	if flag == "" || utf8.RuneCountInString(flag) > 64 {
		return nil, errs.NewValidationError("Флаг: 1–64 символа")
	}
	var out domain.BoolMap
	err := s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		e, err := tx.Employees.ByIDForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if e == nil {
			return errs.NewNotFoundError("Новичок не найден")
		}
		if e.Flags == nil {
			e.Flags = domain.BoolMap{}
		}
		e.Flags[flag] = value
		out = e.Flags
		return tx.Employees.Save(ctx, e)
	})
	if err != nil {
		return nil, err
	}
	logger.Info("флаг %s=%v новичка %s", flag, value, id)
	if value {
		if err := s.Scheduler.FlagChanged(ctx, id); err != nil {
			return out, err
		}
	}
	return out, nil
}

// inputAnswer — ответ на шаг kind=input: текст (при согласии) и фото → в событие и обращение
// HR («показать HR»), в RAG не уходит; затем поток блока.
func (b *BotService) inputAnswer(ctx context.Context, emp *domain.Employee, u max.Update) error {
	text := strings.TrimSpace(u.Text)
	var w *domain.FreeTextWait
	var e *domain.Employee
	var ev *domain.ScheduledEvent
	var step domain.ScenarioStep
	release := false
	err := b.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		var err error
		if e, err = tx.Employees.ByIDForUpdate(ctx, emp.ID); err != nil || e == nil || e.AwaitFreeText == nil {
			return err
		}
		w = e.AwaitFreeText
		e.AwaitFreeText = nil
		now := b.now()
		e.LastActivityAt = &now
		if codes := flags.Codes(text); codes != nil {
			markRisk(e, domain.RiskRed, "сигнал "+strings.Join(codes, ", ")+" в ответе на шаг")
		}
		if err := tx.Employees.Save(ctx, e); err != nil {
			return err
		}
		x, _, st, err := b.loadEvent(ctx, tx, w.EventID, e.ID)
		if err != nil || x == nil {
			return err
		}
		ev, step = x, st
		ans := textInputNoText
		if consented(e) && text != "" {
			ans = text
			if utf8.RuneCountInString(ans) > maxQuoteLen {
				ans = string([]rune(ans)[:maxQuoteLen-1]) + "…"
			}
		}
		if len(u.Images) > 0 {
			ans += fmt.Sprintf(" [+ фото: %d]", len(u.Images))
		}
		x.Status, x.Answer = domain.EventAnswered, &ans
		if x.Waiting != "" {
			x.Waiting, release = "", true
		}
		return tx.Events.Save(ctx, x)
	})
	if err != nil || w == nil || e == nil {
		return err
	}
	logger.Info("бот: ответ новичка %s на шаг-ввод %s: текст %v, фото %d (в rag не отправлено)", e.ID, w.EventID, text != "", len(u.Images))
	if ev != nil && b.tickets != nil {
		var res handoffResult
		err := b.repos.Transaction(ctx, func(tx *repositories.Repos) error {
			x, err := tx.Employees.ByIDForUpdate(ctx, e.ID)
			if err != nil || x == nil {
				return err
			}
			evID := ev.ID
			cat := taxonomy.Question(taxonomy.RouteHR)
			t, created, err := b.tickets.open(ctx, tx, x, ticketDraft{cat: cat, source: domain.TicketFromAnswer, eventID: &evID, quote: text,
				what: fmt.Sprintf("Новичок ответил на шаг «%s» (текст: %v, фото: %d).", step.Title, text != "", len(u.Images))})
			if err != nil {
				return err
			}
			if consented(x) {
				for _, img := range u.Images {
					if img.URL == "" {
						continue
					}
					if err := tx.Tickets.AddMessage(ctx, &domain.TicketMessage{TicketID: t.ID, From: domain.TicketMsgNewcomer, Text: "Фото: " + img.URL}); err != nil {
						return err
					}
				}
			}
			res = handoffResult{ticket: t, created: created}
			return nil
		})
		if err != nil {
			logger.Warn("бот: ответ на шаг-ввод не передан HR: %v", err)
		} else if res.ticket != nil && res.created {
			b.tickets.notify(ctx, res.ticket, e)
		}
	}
	b.send(ctx, recipientOf(e).msg(textInputThanks, "", nil))
	if release {
		b.applyThen(ctx, e.ID, w.EventID, nil)
	}
	return nil
}

// scenarioCommand — команда из scenario.commands (/food → send_file per_tag, /hr → handoff).
// false — у сценария такой команды нет.
func (b *BotService) scenarioCommand(ctx context.Context, emp *domain.Employee, cmd string) (bool, error) {
	cmds, err := scenarioCommandsOf(ctx, b.repos, emp) // из снимка опубликованной версии
	if err != nil {
		return false, err
	}
	for _, c := range cmds {
		name := strings.ToLower(strings.TrimSpace(c.Name))
		if !strings.HasPrefix(name, "/") {
			name = "/" + name
		}
		if name != cmd {
			continue
		}
		logger.Info("бот: команда сценария %s от новичка %s", cmd, emp.ID)
		if strings.TrimSpace(c.TextTy+c.TextVy) != "" {
			b.send(ctx, recipientOf(emp).msg(stepText(domain.ScenarioStep{TextTy: c.TextTy, TextVy: c.TextVy}, emp, b.now()), "", nil))
		}
		b.runPlan(ctx, &pressPlan{emp: emp, ev: domain.ScheduledEvent{}, step: domain.ScenarioStep{Title: "команда " + cmd},
			acts: c.Actions, label: cmd})
		return true, nil
	}
	return false, nil
}
