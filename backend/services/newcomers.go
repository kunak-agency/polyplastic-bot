package services

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"main/config"
	"main/domain"
	"main/pkg/errs"
	"main/repositories"

	"github.com/google/uuid"
)

// NewcomerService — реестр новичков, запуск адаптации, таймлайн, машина времени.
type NewcomerService struct {
	repos     *repositories.Repos
	scheduler *SchedulerService
	bot       config.Bot
	now       func() time.Time
	botReady  bool // есть кому отправлять (MAX или подделка)
}

// NewcomerInput — карточка из «Запустить адаптацию», уже провалидированная слоем in.
type NewcomerInput struct {
	FullName      string
	Phone         string
	Site          string
	Department    string
	Position      string
	ManagerName   string
	MentorName    string
	RecruiterName string
	StartDate     time.Time
	Shift         *string
	ScenarioID    uuid.UUID
}

// NewcomerCard — сотрудник с вычисляемыми полями контракта.
type NewcomerCard struct {
	Employee    domain.Employee
	Day         int
	Stage       string
	InviteLink  string
	OpenTickets int // незакрытые обращения
}

// TimelineItem — событие таймлайна для кабинета.
type TimelineItem = domain.ScheduledEvent

// Сообщения отказов: их видит HR в кабинете.
const (
	msgNewcomerNotFound    = "Новичок не найден"
	msgScenarioNotFound    = "Сценарий не найден"
	msgScenarioUnpublished = "Сценарий ещё не опубликован — запустить по нему адаптацию нельзя"
	msgScenarioOtherSite   = "Сценарий не для этой площадки"
	msgNotConnected        = "Новичок ещё не подключился к боту — отправлять некому"
	msgBotOff              = "Бот выключен (нет MAX_BOT_TOKEN) — машина времени не может отправить сообщения"
)

// CodeScenarioNotPublished — код отказа 409 по контракту («Уточнения v1»).
const CodeScenarioNotPublished = "scenario_not_published"

// inviteTokenBytes — 24 случайных байта → 32 символа base64url: с префиксом inv_ это 36, в
// пределах 128 для payload deep link; ПДн в токене нет.
const inviteTokenBytes = 24

// InvitePayloadPrefix — payload deep link: inv_<token>.
const InvitePayloadPrefix = "inv_"

func newInviteToken() (string, error) {
	b := make([]byte, inviteTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// InviteLink — ссылка-приглашение по формату из окружения.
func InviteLink(b config.Bot, token string) string {
	return fmt.Sprintf(b.InviteLinkFormat, b.BotUsername(), InvitePayloadPrefix+token)
}

func (s *NewcomerService) ready() error {
	if !s.repos.Enabled() {
		return repositories.ErrNoDatabase
	}
	return nil
}

// card — вычисляемые поля: день адаптации, этап, ссылка. Машина времени день не меняет
// (контракт, «Уточнения v1»): она только досрочно отправляет события.
func (s *NewcomerService) card(e domain.Employee, inv *domain.InviteToken) NewcomerCard {
	day := DayNumber(e.StartDate, s.now(), domain.SiteOf(e.Site))
	c := NewcomerCard{Employee: e, Day: day, Stage: domain.StageForDay(day)}
	if inv != nil {
		c.InviteLink = InviteLink(s.bot, inv.Token)
	}
	return c
}

// List — реестр по фильтрам.
func (s *NewcomerService) List(ctx context.Context, f repositories.EmployeeFilter) ([]NewcomerCard, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	list, err := s.repos.Employees.List(ctx, f)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(list))
	for i := range list {
		ids[i] = list[i].ID
	}
	invites, err := s.repos.Employees.LatestInvites(ctx, ids)
	if err != nil {
		return nil, err
	}
	open, err := s.repos.Tickets.CountActive(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]NewcomerCard, len(list))
	for i, e := range list {
		inv, ok := invites[e.ID]
		if ok {
			out[i] = s.card(e, &inv)
		} else {
			out[i] = s.card(e, nil)
		}
		out[i].OpenTickets = open[e.ID]
	}
	return out, nil
}

// Get — карточка.
func (s *NewcomerService) Get(ctx context.Context, id uuid.UUID) (*NewcomerCard, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	e, err := s.repos.Employees.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, errs.NewNotFoundError(msgNewcomerNotFound)
	}
	invites, err := s.repos.Employees.LatestInvites(ctx, []uuid.UUID{id})
	if err != nil {
		return nil, err
	}
	var inv *domain.InviteToken
	if x, ok := invites[id]; ok {
		inv = &x
	}
	c := s.card(*e, inv)
	open, err := s.repos.Tickets.CountActive(ctx, []uuid.UUID{id})
	if err != nil {
		return nil, err
	}
	c.OpenTickets = open[id]
	return &c, nil
}

// Create — «Запустить адаптацию»: карточка, приглашение и события таймлайна по опубликованной
// версии сценария на момент запуска — одной транзакцией.
func (s *NewcomerService) Create(ctx context.Context, in NewcomerInput) (*NewcomerCard, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	token, err := newInviteToken()
	if err != nil {
		return nil, err
	}
	var e domain.Employee
	var inv domain.InviteToken
	err = s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		sc, err := tx.Scenarios.ByID(ctx, in.ScenarioID)
		if err != nil {
			return err
		}
		if sc == nil {
			return errs.NewValidationError(msgScenarioNotFound).WithDetails(errs.FieldError{Field: "scenario_id", Code: errs.CodeInvalidFormat, Message: msgScenarioNotFound})
		}
		if sc.Version == 0 {
			return errs.NewConflictError(msgScenarioUnpublished).WithCode(CodeScenarioNotPublished)
		}
		if len(sc.Sites) > 0 && !sc.Sites.Has(in.Site) {
			return errs.NewValidationError(msgScenarioOtherSite).WithDetails(errs.FieldError{Field: "scenario_id", Code: errs.CodeOutOfRange, Message: msgScenarioOtherSite})
		}
		v, err := tx.Scenarios.Version(ctx, sc.ID, sc.Version)
		if err != nil {
			return err
		}
		if v == nil {
			return errs.NewInternalError("версия сценария не найдена")
		}
		e = domain.Employee{
			FullName: in.FullName, Phone: in.Phone, Site: in.Site, Department: in.Department,
			Position: in.Position, ManagerName: in.ManagerName, MentorName: in.MentorName,
			RecruiterName: in.RecruiterName, StartDate: in.StartDate, Shift: in.Shift,
			ScenarioID: sc.ID, ScenarioVersion: sc.Version,
			Connection: domain.ConnInvited, Risk: domain.RiskGreen,
		}
		if err := tx.Employees.Create(ctx, &e); err != nil {
			return err
		}
		inv = domain.InviteToken{EmployeeID: e.ID, Token: token, ExpiresAt: s.inviteExpiry(in.StartDate)}
		if err := tx.Employees.CreateInvite(ctx, &inv); err != nil {
			return err
		}
		return tx.Events.CreateMissing(ctx, s.scheduler.BuildEvents(&e, v, nil))
	})
	if err != nil {
		return nil, err
	}
	c := s.card(e, &inv)
	return &c, nil
}

// inviteExpiry — приглашение живёт до даты выхода плюс InviteTTL.
func (s *NewcomerService) inviteExpiry(start time.Time) time.Time {
	ttl := s.bot.InviteTTL
	if ttl <= 0 {
		ttl = 30 * 24 * time.Hour
	}
	return civilDate(start, domain.SiteOf("").Loc).Add(ttl)
}

// Reschedule — перенос даты выхода: неотправленные события пересчитываются без дублей,
// приглашение продлевается.
func (s *NewcomerService) Reschedule(ctx context.Context, id uuid.UUID, start time.Time) (*NewcomerCard, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	err := s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		e, err := tx.Employees.ByIDForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if e == nil {
			return errs.NewNotFoundError(msgNewcomerNotFound)
		}
		e.StartDate = start
		if err := tx.Employees.Save(ctx, e); err != nil {
			return err
		}
		if err := tx.Employees.ExtendInvites(ctx, e.ID, s.inviteExpiry(start)); err != nil {
			return err
		}
		return s.scheduler.Reschedule(ctx, tx, e)
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// Timeline — события новичка по времени.
func (s *NewcomerService) Timeline(ctx context.Context, id uuid.UUID) ([]TimelineItem, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	e, err := s.repos.Employees.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, errs.NewNotFoundError(msgNewcomerNotFound)
	}
	return s.repos.Events.ByEmployee(ctx, id)
}

// TimeTravel — отправить всё неотправленное по день day включительно, с пометкой [ТЕСТ].
// Без подключения к боту отправлять некому — это конфликт состояния, а не пустой успех.
func (s *NewcomerService) TimeTravel(ctx context.Context, id uuid.UUID, day int) ([]TimelineItem, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if !s.botReady {
		return nil, errs.NewConflictError(msgBotOff)
	}
	err := s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		e, err := tx.Employees.ByIDForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if e == nil {
			return errs.NewNotFoundError(msgNewcomerNotFound)
		}
		if e.Connection != domain.ConnConnected || e.MaxUserID == nil {
			return errs.NewConflictError(msgNotConnected)
		}
		if e.VirtualDay == nil || day > *e.VirtualDay {
			e.VirtualDay = &day
		}
		e.IsTest = true
		return tx.Employees.Save(ctx, e)
	})
	if err != nil {
		return nil, err
	}
	if _, err := s.scheduler.TimeTravel(ctx, id, day); err != nil {
		return nil, err
	}
	return s.repos.Events.ByEmployee(ctx, id)
}
