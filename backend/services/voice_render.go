package services

import (
	"strings"
	"unicode/utf8"

	"main/externalApi/rag"
)

// Тексты звонка Корсику (handlers/call.go). Их произносит синтез, поэтому без эмодзи и разметки:
// это не bot_ask_render.go, где ответ — сообщение в MAX.
const (
	VoiceHint     = "Говорите — я слушаю. Спросите про отпуск, график, испытательный срок или удалённую работу."
	VoiceGreeting = "Здравствуйте! Я Корсик, помощник по адаптации. Спросите меня про правила компании: отпуск, зарплату, пропуск, график."
	VoiceFiller   = "Секунду, смотрю в регламентах."
	VoiceAbstain  = "В регламентах этого нет, а выдумывать я не буду. Напишите мне в MAX — передам вопрос HR."
	VoiceError    = "Не получилось найти ответ, попробуйте спросить по-другому."
	VoiceSilence  = "Вы меня слышите?"
	VoiceBye      = "Звоните, если будут вопросы. До свидания!"
	VoiceTimeUp   = "Время звонка вышло. Звоните, если будут вопросы. До свидания!"
)

// Виды ответа звонка.
const (
	VoiceKindAnswer  = "answer"
	VoiceKindAbstain = "abstain"
	VoiceKindError   = "error"
)

// VoiceSource — источник ответа для экрана звонка: чип «ПВТР 6.7.4».
type VoiceSource struct {
	Doc      string `json:"doc"`
	ClauseID string `json:"clause_id"`
	Page     *int   `json:"page"`
	Label    string `json:"label"`
}

// docSpoken — как документ звучит в «Это пункт 6.7.4 …»: родительный падеж по началу имени из
// rag (у корпуса имя с номером редакции: «ПВТР П 3.11-07»).
var docSpoken = []struct{ prefix, short, genitive string }{
	{"ПВТР", "ПВТР", "правил внутреннего трудового распорядка"},
	{"Правила внутреннего трудового распорядка", "ПВТР", "правил внутреннего трудового распорядка"},
	{"Положение о коммерческой тайне", "", "положения о коммерческой тайне"},
	{"Антикоррупционная политика", "", "антикоррупционной политики"},
	{"Политика в области качества и экологии", "", "политики в области качества и экологии"},
}

// docBase — имя документа без листа XLSX («имя / лист») и пробелов по краям.
func docBase(doc string) string {
	if i := strings.Index(doc, " / "); i > 0 {
		doc = doc[:i]
	}
	return strings.TrimSpace(doc)
}

// docGenitive — «правил внутреннего трудового распорядка» или «документа «Имя»».
func docGenitive(doc string) string {
	d := docBase(doc)
	for _, k := range docSpoken {
		if strings.HasPrefix(d, k.prefix) {
			return k.genitive
		}
	}
	return "документа «" + d + "»"
}

// sourceLabel — короткая подпись источника: «ПВТР 6.7.4», «Антикоррупционная политика 3.1».
func sourceLabel(doc, clause string) string {
	d := docBase(doc)
	short := d
	for _, k := range docSpoken {
		if strings.HasPrefix(d, k.prefix) && k.short != "" {
			short = k.short
			break
		}
	}
	if r := []rune(short); len(r) > 28 {
		short = string(r[:27]) + "…"
	}
	if clause == "" {
		return short
	}
	return short + " " + clause
}

// voiceSources — источники без повторов пункта, в порядке rag.
func voiceSources(list []rag.Source) []VoiceSource {
	var out []VoiceSource
	seen := map[string]bool{}
	for _, s := range list {
		k := s.Doc + "\x00" + s.ClauseID
		if s.Doc == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, VoiceSource{Doc: s.Doc, ClauseID: s.ClauseID, Page: s.Page, Label: sourceLabel(s.Doc, s.ClauseID)})
	}
	return out
}

// spokenAnswer — ответ rag для синтеза: без разметки, с точкой в конце.
func spokenAnswer(answer string) string {
	a := strings.TrimSpace(strings.NewReplacer("**", "", "__", "", "`", "").Replace(answer))
	if a == "" {
		return ""
	}
	last, _ := utf8.DecodeLastRuneInString(a)
	if !strings.ContainsRune(".!?…", last) {
		a += "."
	}
	return a
}

// VoiceAnswerText — что Корсик говорит в звонке по ответу rag: ответ и пункт («Это пункт 6.7.4
// правил внутреннего трудового распорядка.»), отказ без выдумок или просьба переспросить.
func VoiceAnswerText(resp *rag.AskResponse, err error) (kind, text string, sources []VoiceSource) {
	switch {
	case err != nil || resp == nil:
		return VoiceKindError, VoiceError, nil
	case resp.NeedsEscalation || resp.Answer == nil || strings.TrimSpace(*resp.Answer) == "":
		return VoiceKindAbstain, VoiceAbstain, nil
	}
	sources = voiceSources(resp.Sources)
	text = spokenAnswer(*resp.Answer)
	if len(sources) > 0 {
		s := sources[0]
		if s.ClauseID != "" {
			text += " Это пункт " + s.ClauseID + " " + docGenitive(s.Doc) + "."
		} else {
			text += " Это из " + docGenitive(s.Doc) + "."
		}
	}
	return VoiceKindAnswer, text, sources
}
