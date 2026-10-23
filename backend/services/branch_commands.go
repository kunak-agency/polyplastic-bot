package services

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"main/domain"
	"main/externalApi/max"
	"main/pkg/errs"
	"main/pkg/logger"
	"main/repositories"

	"github.com/google/uuid"
)

// Команды и глобальные правила сценария (api_contract.md v4): PATCH /api/scenarios/{id}
// {commands?, rules?} меняет черновик; публикация кладёт их в снимок версии; бот исполняет
// команды из снимка версии новичка; меню бота в MAX — PATCH /me/commands.

// builtinCommands — свои команды бота: в меню всегда, сценарий их не переопределяет.
var builtinCommands = []max.BotCommand{
	{Name: "start", Description: "Начать"},
	{Name: "help", Description: "Что умеет бот"},
	{Name: "menu", Description: "Меню"},
	{Name: "human", Description: "Связаться с человеком"},
}

var reBotCommand = regexp.MustCompile(`^/[a-z0-9_]{1,32}$`)

var actionTypes = []string{domain.ActionEscalate, domain.ActionHandoff, domain.ActionSetField, domain.ActionSetFlag, domain.ActionSendFile, domain.ActionOpenFreeText}

// normalizeCommands — «/Food» → «/food»; проверка имён и действий.
func normalizeCommands(list domain.CommandList) (domain.CommandList, error) {
	var bad []errs.FieldError
	seen := map[string]bool{}
	out := domain.CommandList{}
	for i, c := range list {
		name := strings.ToLower(strings.TrimSpace(c.Name))
		if !strings.HasPrefix(name, "/") {
			name = "/" + name
		}
		field := "commands[" + itoa(i) + "]"
		switch {
		case !reBotCommand.MatchString(name):
			bad = append(bad, errs.FieldError{Field: field + ".name", Code: errs.CodeInvalidFormat, Message: "Команда: «/» и 1–32 латинских букв, цифр или _"})
		case seen[name] || isBuiltin(name):
			bad = append(bad, errs.FieldError{Field: field + ".name", Code: errs.CodeOutOfRange, Message: "Команда повторяется или занята ботом (/start, /help, /menu, /human)"})
		}
		for j, a := range c.Actions {
			if !containsStr(actionTypes, a.Type) {
				bad = append(bad, errs.FieldError{Field: field + ".actions[" + itoa(j) + "].type", Code: errs.CodeOutOfRange, Message: "Действие: " + strings.Join(actionTypes, ", ")})
			}
		}
		if utf8.RuneCountInString(c.TextTy) > 4000 || utf8.RuneCountInString(c.TextVy) > 4000 {
			bad = append(bad, errs.FieldError{Field: field + ".text_vy", Code: errs.CodeOutOfRange, Message: "Текст не длиннее 4000 символов"})
		}
		seen[name] = true
		c.Name = name
		if c.Actions == nil {
			c.Actions = []domain.Action{}
		}
		out = append(out, c)
	}
	if len(bad) > 0 {
		return nil, errs.NewValidationError("Команды сценария с ошибками").WithDetails(bad...)
	}
	return out, nil
}

func isBuiltin(name string) bool {
	for _, b := range builtinCommands {
		if "/"+b.Name == name {
			return true
		}
	}
	return false
}

func containsStr(l []string, v string) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}

func itoa(i int) string { return strconv.Itoa(i) }

// Patch — PATCH /api/scenarios/{id}: команды и/или правила черновика (nil — не менять).
func (s *ScenarioService) Patch(ctx context.Context, id uuid.UUID, commands *domain.CommandList, rules *domain.RuleList) (*ScenarioDetail, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	var cmds domain.CommandList
	if commands != nil {
		var err error
		if cmds, err = normalizeCommands(*commands); err != nil {
			return nil, err
		}
	}
	err := s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		sc, err := tx.Scenarios.ByID(ctx, id)
		if err != nil {
			return err
		}
		if sc == nil {
			return errs.NewNotFoundError(msgScenarioNotFound)
		}
		if commands != nil {
			sc.Commands = cmds
		}
		if rules != nil {
			sc.Rules = *rules
		}
		sc.Status = domain.ScenarioDraft
		return tx.Scenarios.Save(ctx, sc)
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// SyncBotCommands — меню бота в MAX: свои команды + команды опубликованных версий сценариев.
// Ошибка MAX — только строка в журнал.
func (s *Services) SyncBotCommands(ctx context.Context) {
	setter, ok := s.ext.Max.(max.CommandSetter)
	if !ok || s.ext.Max == nil || !s.repos.Enabled() {
		return
	}
	list := append([]max.BotCommand(nil), builtinCommands...)
	seen := map[string]bool{}
	scs, err := s.repos.Scenarios.List(ctx)
	if err != nil {
		logger.Warn("команды бота: сценарии не прочитаны: %v", err)
		return
	}
	var extra []max.BotCommand
	for _, sc := range scs {
		if sc.Version == 0 {
			continue
		}
		v, err := s.repos.Scenarios.Version(ctx, sc.ID, sc.Version)
		if err != nil || v == nil {
			continue
		}
		for _, c := range v.Commands {
			name := strings.TrimPrefix(c.Name, "/")
			if seen[name] || isBuiltin(c.Name) {
				continue
			}
			seen[name] = true
			extra = append(extra, max.BotCommand{Name: name, Description: commandDescription(c)})
		}
	}
	sort.Slice(extra, func(i, j int) bool { return extra[i].Name < extra[j].Name })
	list = append(list, extra...)
	if err := setter.SetCommands(ctx, list); err != nil {
		logger.Warn("команды бота: PATCH /me/commands не принят: %v", err)
		return
	}
	logger.Info("команды бота: в меню %d команд", len(list))
}

// commandDescription — подпись в меню: по первому действию.
func commandDescription(c domain.Command) string {
	for _, a := range c.Actions {
		switch a.Type {
		case domain.ActionSendFile:
			return a.Media
		case domain.ActionHandoff:
			return "Связаться: " + routeOr(a.To, "HR")
		case domain.ActionEscalate:
			return "Передать вопрос"
		}
	}
	return "Команда сценария"
}

// scenarioCommandsOf — команды из снимка версии новичка.
func scenarioCommandsOf(ctx context.Context, r *repositories.Repos, e *domain.Employee) (domain.CommandList, error) {
	v, err := r.Scenarios.Version(ctx, e.ScenarioID, e.ScenarioVersion)
	if err != nil || v == nil {
		return nil, err
	}
	return v.Commands, nil
}
