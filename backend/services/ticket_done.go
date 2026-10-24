package services

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"main/domain"
	"main/pkg/logger"
	"main/pkg/taxonomy"
	"main/repositories"
)

// Закрытие петли (волна 4, #31): при решении обращения HR пишет одну строку «что сделано».
// Строка хранится внутренней заметкой переписки с префиксом DoneNotePrefix (своего поля у
// обращения нет), адресному новичку уходит «По вашему сигналу: …», а дайджест собирает такие
// строки за неделю в блок «Сделано по темам» — по категориям, без имён.

// DoneNotePrefix — пометка заметки «что сделано» в ticket_messages (internal=true). По ней
// заметку находит дайджест (repositories.OverviewRepo.DigestDone).
const DoneNotePrefix = repositories.DoneNotePrefix

// doneNoteSilent — категории, по которым «что сделано» новичку не уходит: F5/F9 ведут HR-партнёр и
// СБ (огласка опасна), у F10 слов новичка не храним. В дайджест они тоже не идут.
var doneNoteSilent = map[string]bool{"F5": true, "F9": true, "F10": true}

// DoneNoteReachesNewcomer — уйдёт ли «что сделано» новичку: обращение адресное (не анонимное),
// не F5/F9/F10 и не скрытое (restricted).
func DoneNoteReachesNewcomer(t *domain.Ticket) bool {
	return !t.Anonymous && !t.Restricted && !doneNoteSilent[t.Category]
}

// doneNoteText — сообщение новичку: «По вашему сигналу: …» в форме ты/вы.
func doneNoteText(e *domain.Employee, note string) string {
	return "📌 " + byForm(e, "По твоему сигналу: ", "По вашему сигналу: ") + escapeFit(note, maxTextLen-100)
}

// sendDoneNote — «что сделано» новичку (реплаем на исходное сообщение при FEATURE_REPLIES) и строка
// бота в переписку. Не ушло — только в лог: обращение уже закрыто.
func (s *TicketService) sendDoneNote(ctx context.Context, e *domain.Employee, t *domain.Ticket, note string) bool {
	if s.max == nil || note == "" || !DoneNoteReachesNewcomer(t) {
		return false
	}
	to := recipientOf(e)
	if to.ChatID == 0 && to.UserID == 0 {
		return false
	}
	m := to.msg(doneNoteText(e, note), formatMarkdown, nil)
	m.ReplyTo = s.sourceMID(ctx, t)
	if _, err := sendReply(ctx, s.max, s.replies, m); err != nil {
		logger.Warn("обращение #%d: «что сделано» не ушло новичку: %v", t.Number, err)
		return false
	}
	_ = s.repos.Tickets.AddMessage(ctx, &domain.TicketMessage{TicketID: t.ID, From: domain.TicketMsgBot, Text: "Новичку ушло: «По сигналу: " + note + "»"})
	logger.Info("обращение #%d: «что сделано» ушло новичку", t.Number)
	return true
}

// normDoneNote — одна строка: переводы строк — пробелы, края обрезаны.
func normDoneNote(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// --- дайджест ---

// DigestDoneTheme — «Сделано по темам»: категория обращения, сколько закрыто с «что сделано» и
// до трёх обезличенных строк.
type DigestDoneTheme struct {
	Category string
	Label    string
	Count    int
	Notes    []string
}

// digestDoneNotesPerTheme — сколько строк темы показывать.
const digestDoneNotesPerTheme = 3

// BuildDigestDone — группировка «что сделано» за неделю по категории. Числа — код; имена новичка
// маскируется (anonymize: имя, телефон, почта, длинные номера), F5/F9/F10 не берутся. Порядок — по убыванию числа,
// затем по коду категории.
func BuildDigestDone(rows []repositories.DoneNoteRow, tax *taxonomy.Taxonomy) []DigestDoneTheme {
	by := map[string]*DigestDoneTheme{}
	for _, r := range rows {
		if doneNoteSilent[r.Category] || r.Restricted {
			continue
		}
		note := strings.TrimSpace(strings.TrimPrefix(r.Text, DoneNotePrefix))
		if note == "" {
			continue
		}
		th, ok := by[r.Category]
		if !ok {
			label := r.Category
			if tax != nil {
				if c, ok := tax.Get(r.Category); ok && c.Name != "" {
					label = c.Name
				}
			}
			if r.Category == taxonomy.CategoryQuestion {
				label = "Вопросы"
			}
			th = &DigestDoneTheme{Category: r.Category, Label: label}
			by[r.Category] = th
		}
		th.Count++
		if len(th.Notes) < digestDoneNotesPerTheme {
			th.Notes = append(th.Notes, clipRunes(anonymize(note, r.FullName), 160))
		}
	}
	out := make([]DigestDoneTheme, 0, len(by))
	for _, th := range by {
		out = append(out, *th)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Category < out[j].Category
	})
	return out
}

// digestDoneBlock — блок MAX-сообщения; пусто — ничего не закрыто с «что сделано».
func digestDoneBlock(done []DigestDoneTheme) string {
	if len(done) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Сделано по темам:\n")
	for _, th := range done {
		fmt.Fprintf(&b, "• %s — %s", th.Label, plural(th.Count, "обращение", "обращения", "обращений"))
		if len(th.Notes) > 0 {
			b.WriteString(": " + strings.Join(th.Notes, "; "))
		}
		b.WriteString("\n")
	}
	return b.String()
}
