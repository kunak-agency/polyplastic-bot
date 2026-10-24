package services

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"main/domain"
	"main/externalApi/max"
	"main/pkg/flags"

	"github.com/google/uuid"
)

// Рендер ответа на свободный вопрос (UX-спецификация п. 2.2, 2.5) и ответа на сигнал флага
// (п. 2.7). Текст ответа и цитаты — из документов, имя — из HR-базы: всё это проходит через
// escapeMarkdown (правило 9). В ветках отказа, эскалации и флагов эмодзи нет (правило 8);
// исключение — дословный шаблон отказа из п. 2.5.

// maxTextLen — предел текста сообщения MAX.
const maxTextLen = 4000

// maxLeadLen — первая строка-суть: длиннее — значит, предложение не нашлось, берём строку.
const maxLeadLen = 200

// Payload кнопок под ответом: префикс и id записи BotQuestion (UUID, 36 символов), у «qa:» ещё
// номер источника — всё ≤ 45 байт при пределе MAX 1024.
const (
	preQMore      = "qm:" // Подробнее
	preQHelpful   = "qu:" // 👍 Помогло
	preQNotThat   = "qx:" // Не то
	preQAlt       = "qa:" // qa:<id>:<i> — другой найденный пункт
	preQHR        = "qh:" // Передать HR / Спросить человека
	preQRecruiter = "qr:" // Передать рекрутеру
	preQRephrase  = "qp:" // Переформулирую
	preQNoNeed    = "qn:" // Не нужно / Пока не надо
	preFlagSend   = "fs:" // сигнал флага: передать
	preFlagWrite  = "fw:" // сигнал флага: сначала напишу
)

// Подписи кнопок.
const (
	btnMore        = "Подробнее"
	btnHelpful     = "👍 Помогло"
	btnNotThat     = "Не то"
	btnToHR        = "Передать HR"
	btnToRecruiter = "Передать рекрутеру"
	btnRephrase    = "Переформулирую"
	btnNoNeed      = "Не нужно"
	btnNotNow      = "Пока не надо"
)

// Тексты. Пары ty/vy — там, где есть обращение; безличные — одной строкой.
const (
	textAbstain = "В регламентах я этого не нашёл, а выдумывать не буду 🐾. Могу передать вопрос HR — " +
		"обычно отвечают в течение рабочего дня."
	textAbstainExampleVy = "Попробуйте спросить иначе, например: «Когда выдают спецодежду?» или «Как получить пропуск?»"
	textAbstainExampleTy = "Попробуй спросить иначе, например: «Когда выдают спецодежду?» или «Как получить пропуск?»"
	textAbstainOffer     = "Снова не нашёл ответа в документах. Передать вопрос HR? Обычно отвечают в течение рабочего дня."
	textRouted           = "**Это вопрос для человека.**\nПо документам я тут не отвечу — лучше, чтобы ответил HR или " +
		"рекрутер. Кому передать вопрос?"
	textInjection  = "Я отвечаю только по документам компании. Спросите, что нужно узнать о правилах, графике или пропуске, — найду пункт."
	textInjectionT = "Я отвечаю только по документам компании. Спроси, что нужно узнать о правилах, графике или пропуске, — найду пункт."
	textRAGDown    = "Сейчас не могу проверить по документам: справочная база не отвечает. Могу передать вопрос " +
		"HR — обычно отвечают в течение рабочего дня."
	textNotThatOffer = "Не получилось найти нужное. Передать вопрос HR? Обычно отвечают в течение рабочего дня."
	textNotThatAltVy = "Понял, это не то. Возможно, нужен один из этих пунктов — или напишите вопрос другими словами."
	textNotThatAltTy = "Понял, это не то. Возможно, нужен один из этих пунктов — или напиши вопрос другими словами."
	textNotThatVy    = "Понял, это не то. Напишите вопрос другими словами: про какую ситуацию или документ речь?"
	textNotThatTy    = "Понял, это не то. Напиши вопрос другими словами: про какую ситуацию или документ речь?"
	textAltLead      = "**Возможно, нужен этот пункт.**"
	textQuoteLead    = "Дословно из документа:"

	textHandedHR        = "Передаю вопрос HR."
	textHandedRecruiter = "Передаю вопрос рекрутеру."
	textHandedAlready   = "Уже передал — ответ придёт сюда же."
	textRephrase        = "Хорошо, жду вопрос другими словами."
	textNoNeedVy        = "Хорошо. Если что — спрашивайте."
	textNoNeedTy        = "Хорошо. Если что — спрашивай."
	textVoiceVy         = "Голосовые я пока не разбираю. Напишите вопрос текстом, пожалуйста."
	textVoiceTy         = "Голосовые я пока не разбираю. Напиши вопрос текстом, пожалуйста."
)

// byForm — вариант текста по форме обращения; пока форма не выбрана — «вы».
func byForm(e *domain.Employee, ty, vy string) string {
	if e != nil && e.AddressForm != nil && *e.AddressForm == domain.AddressTy {
		return ty
	}
	return vy
}

func qPayload(prefix string, id uuid.UUID) string { return prefix + id.String() }

// splitLead — первая строка-суть и остальное. Суть — первое предложение (до «.», «!», «?» с
// пробелом и заглавной буквой после), если оно не длиннее maxLeadLen; иначе первая строка;
// иначе весь текст.
func splitLead(s string) (string, string) {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	for i, r := range runes {
		if i >= maxLeadLen {
			break
		}
		if r == '\n' {
			return strings.TrimSpace(string(runes[:i])), strings.TrimSpace(string(runes[i+1:]))
		}
		if (r == '.' || r == '!' || r == '?') && sentenceStarts(runes[i+1:]) {
			return string(runes[:i+1]), strings.TrimSpace(string(runes[i+1:]))
		}
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:])
	}
	return s, ""
}

// sentenceStarts — после знака конца идут пробел и заглавная буква (или «): так «п. 6.7.8» и
// «т. е.» не режут суть посередине.
func sentenceStarts(after []rune) bool {
	if len(after) < 2 || !unicode.IsSpace(after[0]) {
		return false
	}
	for _, r := range after[1:] {
		if unicode.IsSpace(r) {
			continue
		}
		return unicode.IsUpper(r) || r == '«' || r == '"'
	}
	return false
}

// escapeFit — escapeMarkdown(s), не длиннее limit символов после экранирования. Если не
// влезает — обрезка по последнему пробелу (если он недалеко) и «…». Режется исходный текст, а
// не экранированный: обратная черта не остаётся висеть без своего символа.
func escapeFit(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	full := escapeMarkdown(s)
	if utf8.RuneCountInString(full) <= limit {
		return full
	}
	runes := []rune(s)
	n, cut := 0, 0
	for i, r := range runes {
		w := utf8.RuneCountInString(escapeMarkdown(string(r)))
		if n+w > limit-1 { // место под «…»
			break
		}
		n += w
		cut = i + 1
	}
	head := string(runes[:cut])
	if sp := strings.LastIndexFunc(head, unicode.IsSpace); sp > 0 && utf8.RuneCountInString(head[sp:]) <= 30 {
		head = head[:sp]
	}
	return escapeMarkdown(strings.TrimRightFunc(head, unicode.IsSpace)) + "…"
}

// quoteFit — дословная цитата пункта для «Подробнее» и «Другой пункт», не длиннее limit. rag
// отдаёт чанк, и он бывает обрывком предложения («работник обязан …» с маленькой буквы).
// Текст не меняем (правило 9: цитаты дословно), а показываем, что начало опущено: «…» впереди.
func quoteFit(s string, limit int) string {
	s = strings.TrimSpace(s)
	if !midSentence(s) {
		return escapeFit(s, limit)
	}
	return "…" + escapeFit(s, limit-1)
}

// midSentence — цитата начинается с середины предложения: первая буква строчная. Пункт списка
// «а) …» и начало с цифры («1. …», «6.7.8 …») — не обрывок.
func midSentence(s string) bool {
	runes := []rune(s)
	for i, r := range runes {
		switch {
		case unicode.IsDigit(r):
			return false
		case unicode.IsLetter(r):
			if i+1 < len(runes) && runes[i+1] == ')' {
				return false
			}
			return unicode.IsLower(r)
		case r == '…':
			return false // начало уже помечено
		}
	}
	return false
}

// sourceLine — «📄 Источник: ПВТР, п. 6.7.8».
func sourceLine(s domain.QuestionSource) string {
	line := "📄 Источник: " + escapeMarkdown(s.Doc)
	if s.ClauseID != "" {
		line += ", п. " + escapeMarkdown(s.ClauseID)
	}
	return line
}

// distinctSources — источники без повторов пункта, в порядке rag.
func distinctSources(list domain.SourceList) []domain.QuestionSource {
	var out []domain.QuestionSource
	seen := map[string]bool{}
	for _, s := range list {
		k := s.Doc + "\x00" + s.ClauseID
		if seen[k] || s.Quote == "" {
			continue
		}
		seen[k] = true
		out = append(out, s)
	}
	return out
}

// answerText — ответ: суть жирным, остальное, строка источника. expanded — ниже дословные
// цитаты всех найденных пунктов; всё вместе ≤ maxTextLen, цитаты режутся последними.
func answerText(q *domain.BotQuestion, expanded bool) string {
	srcs := distinctSources(q.Sources)
	lead, rest := splitLead(q.Answer)
	var b strings.Builder
	b.WriteString("**" + escapeFit(lead, maxLeadLen+20) + "**")
	if rest != "" {
		b.WriteString("\n" + escapeFit(rest, maxTextLen/2))
	}
	if !expanded {
		if len(srcs) > 0 {
			b.WriteString("\n" + sourceLine(srcs[0]))
		}
		return b.String()
	}
	b.WriteString("\n\n" + textQuoteLead)
	used := utf8.RuneCountInString(b.String())
	for i, s := range srcs {
		line := sourceLine(s)
		// «\n\n«» + цитата + »\n» + строка источника
		budget := maxTextLen - used - utf8.RuneCountInString(line) - 5
		if budget < 60 {
			if i == 0 { // первую цитату показываем хоть сколько-то
				budget = 1
			} else {
				break
			}
		}
		block := "\n\n«" + quoteFit(s.Quote, budget) + "»\n" + line
		b.WriteString(block)
		used += utf8.RuneCountInString(block)
	}
	return b.String()
}

// answerMessage — ответ по документам с кнопками п. 2.5. «Открыть документ» не рисуем: ссылки
// на документ в ответе rag пока нет.
func answerMessage(to recipient, q *domain.BotQuestion, expanded bool) max.OutMessage {
	var rows [][]max.Button
	if !expanded {
		rows = append(rows, []max.Button{{Text: btnMore, Payload: qPayload(preQMore, q.ID)}})
	}
	rows = append(rows,
		[]max.Button{{Text: btnHelpful, Payload: qPayload(preQHelpful, q.ID)}, {Text: btnNotThat, Payload: qPayload(preQNotThat, q.ID)}},
		[]max.Button{{Text: btnHuman, Payload: qPayload(preQHR, q.ID)}},
	)
	return to.msg(answerText(q, expanded), formatMarkdown, rows)
}

// altMessage — другой найденный пункт после «Не то»: дословная цитата и источник.
func altMessage(to recipient, q *domain.BotQuestion, s domain.QuestionSource) max.OutMessage {
	line := sourceLine(s)
	budget := maxTextLen - utf8.RuneCountInString(textAltLead+line) - 4
	text := textAltLead + "\n«" + quoteFit(s.Quote, budget) + "»\n" + line
	return to.msg(text, formatMarkdown, [][]max.Button{
		{{Text: btnHelpful, Payload: qPayload(preQHelpful, q.ID)}, {Text: btnNotThat, Payload: qPayload(preQNotThat, q.ID)}},
		{{Text: btnHuman, Payload: qPayload(preQHR, q.ID)}},
	})
}

// altLabel — подпись кнопки другого пункта: «ПВТР, п. 6.7.9», до 32 символов.
func altLabel(s domain.QuestionSource) string {
	l := s.Doc
	if s.ClauseID != "" {
		l += ", п. " + s.ClauseID
	}
	if r := []rune(l); len(r) > 32 {
		l = string(r[:31]) + "…"
	}
	return l
}

// notThatMessage — первое «Не то»: ответ не повторяем. Есть другие найденные пункты — кнопки
// на них (до трёх), иначе просьба переформулировать.
func notThatMessage(to recipient, e *domain.Employee, q *domain.BotQuestion) max.OutMessage {
	srcs := distinctSources(q.Sources)
	var rows [][]max.Button
	for i := 1; i < len(srcs) && i <= 3; i++ {
		rows = append(rows, []max.Button{{Text: altLabel(srcs[i]), Payload: qPayload(preQAlt, q.ID) + ":" + strconv.Itoa(i)}})
	}
	text := byForm(e, textNotThatTy, textNotThatVy)
	if len(rows) > 0 {
		text = byForm(e, textNotThatAltTy, textNotThatAltVy)
	}
	rows = append(rows, []max.Button{{Text: btnRephrase, Payload: qPayload(preQRephrase, q.ID)}}, []max.Button{{Text: btnHuman, Payload: qPayload(preQHR, q.ID)}})
	return to.msg(text, "", rows)
}

// offerHRMessage — «Передать HR?» с подтверждением: второе «Не то», третий отказ подряд.
func offerHRMessage(to recipient, q *domain.BotQuestion, text string) max.OutMessage {
	return to.msg(text, "", [][]max.Button{
		{{Text: btnToHR, Payload: qPayload(preQHR, q.ID)}, {Text: btnNoNeed, Payload: qPayload(preQNoNeed, q.ID)}},
	})
}

// abstainBase — текст отказа без кнопок; attempt — номер отказа подряд (лестница ошибок п. 2.5).
func abstainBase(e *domain.Employee, attempt int) string {
	switch {
	case attempt >= 3:
		return textAbstainOffer
	case attempt == 2:
		return textAbstain + "\n\n" + byForm(e, textAbstainExampleTy, textAbstainExampleVy)
	}
	return textAbstain
}

// baseText — текст сообщения с ответом бота без клавиатуры: в него правится сообщение после
// «Передать HR», «Переформулирую», «Не нужно».
func baseText(e *domain.Employee, q *domain.BotQuestion) (string, string) {
	switch q.Kind {
	case domain.QuestionAnswer:
		return answerText(q, q.Expanded), formatMarkdown
	case domain.QuestionAbstain:
		if q.Answer != "" { // текст правила сценария r69
			return q.Answer, ""
		}
		return abstainBase(e, q.Attempt), ""
	case domain.QuestionRouted:
		if q.Topic == "security" {
			return byForm(e, textInjectionT, textInjection), ""
		}
		return textRouted, formatMarkdown
	case domain.QuestionError:
		if q.Answer != "" {
			return q.Answer, ""
		}
		return textRAGDown, ""
	case domain.QuestionFlag:
		text := flagText(e, q.Flags)
		if q.TicketNumber > 0 && q.TicketID != nil && flagCopyFor(q.Flags).code != flags.CodeQuit {
			text += fmt.Sprintf(" Обращение #%d.", q.TicketNumber)
		}
		return text, formatMarkdown
	}
	return "", ""
}

// questionMessage — первое сообщение бота на вопрос по виду записи.
func questionMessage(to recipient, e *domain.Employee, q *domain.BotQuestion) max.OutMessage {
	text, format := baseText(e, q)
	id := q.ID
	switch q.Kind {
	case domain.QuestionAnswer:
		return answerMessage(to, q, false)
	case domain.QuestionAbstain:
		if q.Attempt >= 3 {
			return offerHRMessage(to, q, text)
		}
		return to.msg(text, format, [][]max.Button{
			{{Text: btnToHR, Payload: qPayload(preQHR, id)}, {Text: btnRephrase, Payload: qPayload(preQRephrase, id)}},
			{{Text: btnNoNeed, Payload: qPayload(preQNoNeed, id)}},
		})
	case domain.QuestionRouted:
		if q.Topic == "security" {
			return to.msg(text, format, [][]max.Button{
				{{Text: btnRephrase, Payload: qPayload(preQRephrase, id)}},
				{{Text: btnHuman, Payload: qPayload(preQHR, id)}},
			})
		}
		return to.msg(text, format, [][]max.Button{
			{{Text: btnToHR, Payload: qPayload(preQHR, id)}, {Text: btnToRecruiter, Payload: qPayload(preQRecruiter, id)}},
			{{Text: btnNoNeed, Payload: qPayload(preQNoNeed, id)}},
		})
	case domain.QuestionError:
		return to.msg(text, format, [][]max.Button{
			{{Text: btnToHR, Payload: qPayload(preQHR, id)}, {Text: btnNoNeed, Payload: qPayload(preQNoNeed, id)}},
		})
	case domain.QuestionFlag:
		f := flagCopyFor(q.Flags)
		if q.TicketID != nil { // F5/F9: уже передано — дописать подробности или отменить
			return to.msg(text, format, [][]max.Button{
				{{Text: btnFlagMore, Payload: qPayload(preFlagWrite, id)}, {Text: btnTicketCancel, Payload: preTicketCancel + q.TicketID.String()}},
			})
		}
		return to.msg(text, format, [][]max.Button{
			{{Text: f.send, Payload: qPayload(preFlagSend, id)}, {Text: f.write, Payload: qPayload(preFlagWrite, id)}},
			{{Text: btnNotNow, Payload: qPayload(preQNoNeed, id)}},
		})
	}
	return to.msg(text, format, nil)
}

// --- сигналы флагов (п. 2.7) ---

// flagCopy — тексты ответа на сигнал: правило трёх шагов (признать без оценки → действие с
// выбором → что будет), без эмодзи. {name} — имя, {you} — «вас»/«тебя» и т. п. по форме.
type flagCopy struct {
	code     string
	target   string
	vy, ty   string
	send     string // кнопка «передать»
	write    string // кнопка «сначала напишу»
	handedVy string
	handedTy string
}

var flagCopies = []flagCopy{
	{
		code: flags.CodeViolence, target: domain.TargetHRPartner,
		vy: "Понял вас{name}: речь о том, что кто-то применил силу. Так быть не должно. Я бот и сам это не решу, " +
			"поэтому передаю HR-партнёру — эту переписку видит только он(а), руководителю копия не уходит. " +
			"Обычно отвечают в течение рабочего дня. Если хотите, напишите подробнее — приложу.",
		ty: "Понял тебя{name}: речь о том, что кто-то применил силу. Так быть не должно. Я бот и сам это не решу, " +
			"поэтому передаю HR-партнёру — эту переписку видит только он(а), руководителю копия не уходит. " +
			"Обычно отвечают в течение рабочего дня. Если хочешь, напиши подробнее — приложу.",
		send: "Передать HR-партнёру", write: "Сначала напишу подробнее",
		handedVy: "Записал: передам HR-партнёру. Переписку увидит только он(а). Обычно отвечают в течение рабочего дня.",
		handedTy: "Записал: передам HR-партнёру. Переписку увидит только он(а). Обычно отвечают в течение рабочего дня.",
	},
	{
		code: flags.CodeCorruption, target: domain.TargetSecurity,
		vy: "Понял вас{name}. Спасибо, что сообщили, — это важно. Я бот и сам это не решу, поэтому передаю в " +
			"службу безопасности: эту переписку видит только её сотрудник, руководителю копия не уходит. " +
			"Обычно отвечают в течение рабочего дня. Если хотите, напишите подробнее — приложу.",
		ty: "Понял тебя{name}. Спасибо, что сообщил(а), — это важно. Я бот и сам это не решу, поэтому передаю в " +
			"службу безопасности: эту переписку видит только её сотрудник, руководителю копия не уходит. " +
			"Обычно отвечают в течение рабочего дня. Если хочешь, напиши подробнее — приложу.",
		send: "Передать в СБ", write: "Сначала напишу подробнее",
		handedVy: "Записал: передам в службу безопасности. Переписку увидит только её сотрудник. Обычно отвечают в течение рабочего дня.",
		handedTy: "Записал: передам в службу безопасности. Переписку увидит только её сотрудник. Обычно отвечают в течение рабочего дня.",
	},
	{
		code: flags.CodeQuit, target: domain.TargetRecruiter,
		vy: "Слышу вас{name}. Спасибо, что сказали прямо, — это важно. Я бот и не решаю такие вопросы, но могу " +
			"передать рекрутеру, чтобы с вами связались и обсудили, что происходит. Как поступим?",
		ty: "Слышу тебя{name}. Спасибо, что сказал(а) прямо, — это важно. Я бот и не решаю такие вопросы, но могу " +
			"передать рекрутеру, чтобы с тобой связались и обсудили, что происходит. Как поступим?",
		send: "Передать рекрутеру", write: "Сначала напишу, что случилось",
		handedVy: "Записал: передам рекрутеру, с вами свяжутся. Обычно отвечают в течение рабочего дня.",
		handedTy: "Записал: передам рекрутеру, с тобой свяжутся. Обычно отвечают в течение рабочего дня.",
	},
}

const (
	textFlagWriteVy = "Напишите одним сообщением, что случилось. Я приложу его к обращению."
	textFlagWriteTy = "Напиши одним сообщением, что случилось. Я приложу его к обращению."
	textFlagLaterVy = "Хорошо. Если передумаете — нажмите «Спросить человека» в любом сообщении или напишите мне."
	textFlagLaterTy = "Хорошо. Если передумаешь — нажми «Спросить человека» в любом сообщении или напиши мне."
	textFlagDetails = "Спасибо, записал."
)

// flagCopyFor — тексты самого серьёзного сработавшего флага: F5, затем F9, затем F7.
func flagCopyFor(codes domain.StringList) flagCopy {
	for _, c := range flagCopies {
		if codes.Has(c.code) {
			return c
		}
	}
	return flagCopies[len(flagCopies)-1]
}

// flagTarget — кому передать сигнал.
func flagTarget(codes domain.StringList) string { return flagCopyFor(codes).target }

// flagText — ответ на сигнал с именем.
func flagText(e *domain.Employee, codes domain.StringList) string {
	c := flagCopyFor(codes)
	name := ""
	if n := FirstName(e.FullName); n != "" {
		name = ", " + escapeMarkdown(n)
	}
	return strings.ReplaceAll(byForm(e, c.ty, c.vy), "{name}", name)
}

// flagHandedText — итог после «Передать» или после присланных деталей.
func flagHandedText(e *domain.Employee, codes domain.StringList) string {
	c := flagCopyFor(codes)
	return byForm(e, c.handedTy, c.handedVy)
}
