package max

import (
	"context"
	"fmt"

	"github.com/max-messenger/max-bot-api-client-go/v2/model"
)

// Typer — необязательная возможность клиента: показать «печатает» в чате (POST
// /chats/{chatId}/actions, typing_on). Отдельный интерфейс, а не метод Client: бизнес-код
// спрашивает его приведением типа, и подделки других пакетов не обязаны его реализовывать.
type Typer interface {
	Typing(ctx context.Context, chatID int64) error
}

var (
	_ Typer = (*SDKClient)(nil)
	_ Typer = (*Fake)(nil)
)

// Typing — «печатает». Не идёт через очередь отправки: это не сообщение.
func (c *SDKClient) Typing(ctx context.Context, chatID int64) error {
	if chatID == 0 {
		return errNoRecipient
	}
	res, err := c.api.Chats.SendAction(ctx, chatID, model.ActionTypingOn)
	if err != nil {
		return err
	}
	if !res.Success {
		return fmt.Errorf("max: typing_on не принят: %s", res.Message)
	}
	return nil
}

// Typing — подделка: запоминает чат.
func (f *Fake) Typing(_ context.Context, chatID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.typing = append(f.typing, chatID)
	f.journalTyping(chatID)
	return nil
}

// TypingChats — чаты, в которые показывали «печатает».
func (f *Fake) TypingChats() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int64(nil), f.typing...)
}
