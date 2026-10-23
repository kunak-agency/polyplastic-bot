package max

import (
	"context"

	"github.com/max-messenger/max-bot-api-client-go/v2/model"
)

// BotCommand — команда в меню бота MAX (PATCH /me/commands).
type BotCommand struct {
	Name        string // без «/»
	Description string
}

// CommandSetter — необязательная возможность клиента: задать список команд бота. Приведением
// типа, как Typer: подделки других пакетов не обязаны её реализовывать.
type CommandSetter interface {
	SetCommands(ctx context.Context, cmds []BotCommand) error
}

var (
	_ CommandSetter = (*SDKClient)(nil)
	_ CommandSetter = (*Fake)(nil)
)

// SetCommands — PATCH /me/commands.
func (c *SDKClient) SetCommands(ctx context.Context, cmds []BotCommand) error {
	list := make([]model.BotCommand, len(cmds))
	for i, x := range cmds {
		list[i] = model.BotCommand{Name: x.Name, Description: x.Description}
	}
	_, err := c.api.Bots.PatchCommands(ctx, model.BotPatchCommands{Commands: list})
	return err
}

// SetCommands — подделка: запоминает список.
func (f *Fake) SetCommands(_ context.Context, cmds []BotCommand) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commands = append([]BotCommand(nil), cmds...)
	return nil
}

// Commands — последний заданный список команд.
func (f *Fake) Commands() []BotCommand {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]BotCommand(nil), f.commands...)
}
