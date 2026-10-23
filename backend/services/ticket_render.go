package services

import (
	"fmt"
	"strings"
	"time"

	"main/domain"
	"main/externalApi/max"
	"main/pkg/taxonomy"

	"github.com/google/uuid"
)

// Тексты обращения для новичка (UX-спецификация п. 2.7) и карточка для HR-чата. Текст HR и
// имя исполнителя проходят через escapeMarkdown. Кнопки «без моего имени» нет: анонимность
// HR-процессом не обеспечена (п. 2.7, вопрос 16).

// Payload кнопок обращения: префикс и id обращения (UUID) — 39 байт.
const (
	preTicketDetails = "td:" // Добавить детали
	preTicketCancel  = "tx:" // Отменить
	preTicketReply   = "tr:" // Ответить
	preTicketClose   = "tc:" // Вопрос закрыт
	preTicketUp      = "tu:" // 👍 Помогли
	preTicketDown    = "tn:" // 👎 Не помогли
	preAccessReason  = "ar:" // ar:<event>:<i> — причина «Чего-то нет»
)

func isTicketPayload(p string) bool {
	for _, pre := range []string{preTicketDetails, preTicketCancel, preTicketReply, preTicketClose, preTicketUp, preTicketDown} {
		if strings.HasPrefix(p, pre) {
			return true
		}
	}
	return false
}

// accessReasons — кнопки после «Чего-то нет» (UX 2.3, D0 12:00).
var accessReasons = []string{"Техника", "Учётка", "Пропуск", "Другое"}

// accessMissing — кнопка «Чего-то нет» шага «Проверка доступов»: по action "access" или подписи.
func accessMissing(b domain.StepButton) bool {
	return b.Action == "access" || strings.EqualFold(strings.TrimSpace(b.Label), "Чего-то нет")
}

const (
	btnTicketDetails = "Добавить детали"
	btnTicketCancel  = "Отменить"
	btnTicketReply   = "Ответить"
	btnTicketClose   = "Вопрос закрыт"
	btnTicketUp      = "👍 Помогли"
	btnTicketDown    = "👎 Не помогли"
	btnFlagMore      = "Напишу подробнее"

	textAccessWhich  = "Чего именно нет?"
	textTicketCancel = "Хорошо, отменил обращение #%d. Если что — спрашивайте."
	textTicketCancT  = "Хорошо, отменил обращение #%d. Если что — спрашивай."
	textTicketGone   = "Обращение уже закрыто"
	textReplyHintVy  = "Напишите ответ сообщением — передам."
	textReplyHintTy  = "Напиши ответ сообщением — передам."
	textDetailsVy    = "Напишите одним сообщением, что добавить. Я приложу его к обращению #%d."
	textDetailsTy    = "Напиши одним сообщением, что добавить. Я приложу его к обращению #%d."
	textAttached     = "Спасибо, приложил к обращению #%d."
	textForwarded    = "Передал — ответ придёт сюда же (обращение #%d)."
	textTicketExists = "Обращение #%d уже передано — ответ придёт сюда же."
)

// ticketTo — кому передано, в дательном падеже: «Передал ваш вопрос HR / рекрутеру / …».
func ticketTo(cat taxonomy.Category) string {
	switch {
	case cat.Route == taxonomy.RouteSecurity:
		return "в службу безопасности"
	case cat.PartnerOnly:
		return "HR-партнёру"
	case cat.Route == taxonomy.RouteRecruiter:
		return "рекрутеру"
	case cat.Route == taxonomy.RouteDuty:
		return "дежурному площадки и HR"
	}
	return "HR"
}

// staffRole — подпись роли в ответе и статусе: «Анна Смирнова, HR».
func staffRole(cat taxonomy.Category) string {
	switch {
	case cat.Route == taxonomy.RouteSecurity:
		return "служба безопасности"
	case cat.PartnerOnly:
		return "HR-партнёр"
	case cat.Route == taxonomy.RouteRecruiter:
		return "рекрутер"
	}
	return "HR"
}

// ticketOpenText — OPEN (п. 2.7). offHours — вне рабочего времени площадки. lead — строка перед
// статусом (например, «Спасибо, записал.»).
func ticketOpenText(e *domain.Employee, t *domain.Ticket, cat taxonomy.Category, offHours bool, lead string) string {
	var b strings.Builder
	if lead != "" {
		b.WriteString(escapeMarkdown(lead) + "\n")
	}
	to := ticketTo(cat)
	switch {
	case cat.SecurityChat || cat.PartnerOnly:
		who := "он(а)"
		if cat.Route == taxonomy.RouteSecurity {
			who = "только её сотрудник"
		} else {
			who = "только " + who
		}
		fmt.Fprintf(&b, "🕓 **Передаю %s.** Эту переписку видит %s, руководителю копия не уходит.", to, who)
	case cat.Code == "F2":
		fmt.Fprintf(&b, "🕓 **Передал %s.** %s", to, byForm(e, "Пока уточни у руководителя, когда будет готово.", "Пока уточните у руководителя, когда будет готово."))
	default:
		fmt.Fprintf(&b, "🕓 **Передал %s вопрос %s.** Переписку они видят, повторять не нужно.", byForm(e, "твой", "ваш"), to)
	}
	b.WriteString(" ")
	if offHours {
		who := "HR"
		if cat.Route == taxonomy.RouteRecruiter {
			who = "Рекрутер"
		}
		b.WriteString(who + " на связи в будни 9:00–18:00, ответят в ближайший рабочий день.")
	} else {
		b.WriteString("Обычно отвечают в течение рабочего дня (до 18:00).")
	}
	fmt.Fprintf(&b, " Обращение #%d.", t.Number)
	return b.String()
}

func ticketOpenButtons(t *domain.Ticket) [][]max.Button {
	return [][]max.Button{{
		{Text: btnTicketDetails, Payload: preTicketDetails + t.ID.String()},
		{Text: btnTicketCancel, Payload: preTicketCancel + t.ID.String()},
	}}
}

// ticketAssignedText — ASSIGNED: «👀 Вашим вопросом занимается Анна, HR».
func ticketAssignedText(e *domain.Employee, t *domain.Ticket, cat taxonomy.Category) string {
	name := "команда"
	if t.AssigneeName != nil {
		name = *t.AssigneeName
	}
	return fmt.Sprintf("👀 **%s вопросом занимается %s, %s.** Обращение #%d.",
		byForm(e, "Твоим", "Вашим"), escapeMarkdown(name), staffRole(cat), t.Number)
}

// staffSignature — «**Анна Смирнова, HR** (по обращению #1042):».
func staffSignature(t *domain.Ticket, cat taxonomy.Category) string {
	role := staffRole(cat)
	head := role
	if t.AssigneeName != nil && strings.TrimSpace(*t.AssigneeName) != "" {
		head = escapeMarkdown(*t.AssigneeName) + ", " + role
	}
	return fmt.Sprintf("**%s** (по обращению #%d):", head, t.Number)
}

// staffReplyMessage — HUMAN_ACTIVE: ответ человека отдельным сообщением с подписью.
func staffReplyMessage(to recipient, t *domain.Ticket, cat taxonomy.Category, text string) max.OutMessage {
	body := staffSignature(t, cat) + "\n" + escapeFit(text, maxTextLen-200)
	return to.msg(body, formatMarkdown, [][]max.Button{{
		{Text: btnTicketReply, Payload: preTicketReply + t.ID.String()},
		{Text: btnTicketClose, Payload: preTicketClose + t.ID.String()},
	}})
}

// ticketResolvedMessage — RESOLVED (п. 2.7).
func ticketResolvedMessage(to recipient, e *domain.Employee, t *domain.Ticket) max.OutMessage {
	text := "✅ **Вопрос закрыт.** Вернулся 🐾 — если что, " + byForm(e, "спрашивай.", "спрашивайте.")
	return to.msg(text, formatMarkdown, [][]max.Button{{
		{Text: btnTicketUp, Payload: preTicketUp + t.ID.String()},
		{Text: btnTicketDown, Payload: preTicketDown + t.ID.String()},
	}})
}

// accessReasonsMessage — шаг «Проверка доступов» после «Чего-то нет»: кнопки причин.
func accessReasonsMessage(to recipient, s domain.ScenarioStep, e *domain.Employee, ev *domain.ScheduledEvent, at time.Time) max.OutMessage {
	text := stepText(s, e, at)
	if ev.IsTest {
		text = testPrefix + text
	}
	var rows [][]max.Button
	for i := 0; i < len(accessReasons); i += 2 {
		row := []max.Button{}
		for j := i; j < i+2 && j < len(accessReasons); j++ {
			row = append(row, max.Button{Text: accessReasons[j], Payload: fmt.Sprintf("%s%s:%d", preAccessReason, ev.ID, j)})
		}
		rows = append(rows, row)
	}
	rows = append(rows, humanRow(&ev.ID))
	return to.msg(text+"\n\n"+textAccessWhich, "", rows)
}

// dayLabel — «D+0», «D−1», «D+12».
func dayLabel(day int) string {
	switch {
	case day < 0:
		return fmt.Sprintf("D−%d", -day)
	}
	return fmt.Sprintf("D+%d", day)
}

// categoryLabel — «F2 · Нет доступов, техники, СИЗ, пропуска» или «Вопрос человеку».
func categoryLabel(cat taxonomy.Category) string {
	if cat.Code == taxonomy.CategoryQuestion {
		return cat.Name
	}
	return cat.Code + " · " + cat.Name
}

// ticketSummary — «Сводка от Корсика»: кодом, без текста новичка (цитата — отдельным полем).
func ticketSummary(cat taxonomy.Category, what string, day int, context int) string {
	s := fmt.Sprintf("%s. %s День адаптации %s.", categoryLabel(cat), what, dayLabel(day))
	if context > 0 {
		s += fmt.Sprintf(" В переписке — последние сообщения новичка боту (%d).", context)
	}
	return s
}

// ticketCard — карточка в HR-чат или чат СБ. Без цитаты и переписки: они только в кабинете.
// Закрытая категория в HR-чате — без имени, категории и сводки.
func ticketCard(t *domain.Ticket, cat taxonomy.Category, e *domain.Employee, site domain.Site) string {
	due := ""
	if t.SLADueAt != nil {
		due = ", ответить до " + t.SLADueAt.In(site.Loc).Format("02.01 15:04")
	}
	if cat.ManagerHidden && !cat.SecurityChat {
		return fmt.Sprintf("Новое обращение #%d · конфиденциальное · %s%s. Подробности — только в кабинете, раздел «Обращения».",
			t.Number, t.Priority, due)
	}
	card := fmt.Sprintf("Новое обращение #%d · %s · %s%s\n%s · %s · %s\n%s",
		t.Number, categoryLabel(cat), t.Priority, due, e.FullName, e.Site, dayLabel(t.Day), t.Summary)
	if t.NotifyManager {
		card += "\nРуководителю — уведомление без текста."
	}
	return card + "\nОткрыть: кабинет → «Обращения»."
}

func ticketPayloadID(p string) (uuid.UUID, bool) {
	if len(p) < 3 {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(p[3:])
	return id, err == nil
}
