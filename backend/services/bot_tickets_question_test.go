package services

import (
	"testing"

	"main/domain"
)

func TestLooksLikeQuestion(t *testing.T) {
	yes := []string{"А как мне отпуск попросить?", "а как взять отпуск", "Когда аванс", "подскажите, где столовая"}
	no := []string{"Хорошо", "спасибо", "Я на втором этаже", "Транспорт нужен от метро"}
	for _, s := range yes {
		if !looksLikeQuestion(s) {
			t.Errorf("%q: ждали вопрос", s)
		}
	}
	for _, s := range no {
		if looksLikeQuestion(s) {
			t.Errorf("%q: не вопрос", s)
		}
	}
}

func TestStaffJoined(t *testing.T) {
	if staffJoined([]domain.TicketMessage{{From: domain.TicketMsgNewcomer}, {From: domain.TicketMsgBot}}) {
		t.Fatal("HR не писал")
	}
	if staffJoined([]domain.TicketMessage{{From: domain.TicketMsgStaff, Internal: true}}) {
		t.Fatal("внутренняя заметка не в счёт")
	}
	if !staffJoined([]domain.TicketMessage{{From: domain.TicketMsgStaff}}) {
		t.Fatal("HR ответил")
	}
}

func TestRepeatOf(t *testing.T) {
	docs := "Необходимые документы для Вашего трудоустройства:\n1. Бумажная трудовая книжка;\n2. Паспорт;\n3. СНИЛС;\n4. ИНН;"
	first := "Для того, чтобы всё было готово в свой первый рабочий день, ознакомься со списком.\n\n" + docs
	again := "Для того, чтобы всё было готово в первый рабочий день, напоминаю про список.\n\n" + docs
	if !repeatOf(contentLines(again), contentLines(first)) {
		t.Fatal("напоминание с тем же списком — повтор")
	}
	other := "Корпоративный транспорт ходит от метро.\nОстановка у проходной №2.\nПервый рейс в 7:10."
	if repeatOf(contentLines(other), contentLines(first)) {
		t.Fatal("другой текст — не повтор")
	}
}
