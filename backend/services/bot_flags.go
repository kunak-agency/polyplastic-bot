package services

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"main/domain"
	"main/externalApi/rag"
	"main/pkg/logger"
	"main/pkg/taxonomy"
	"main/repositories"

	"github.com/google/uuid"
)

// LLM-разметка свободного текста новичка (POST /flags, промпт flags-classify v2). Порядок: regex
// (pkg/flags) — приоритет и без LLM, эскалация по нему сразу; /flags — в фоне всегда (и при
// сработавшем regex), ответ новичку (rag, опрос, «Передаю…») её не ждёт.
//
// Сработал regex — модель только дополняет: тон (вместо neg кодом), уверенность в тоне, цитату и
// строку для HR — в запись текста и в обращение. Слияние: код — максимальная серьёзность,
// уверенность — максимальная (mergeFlags). Повторной эскалации нет: новые коды модели — только
// риск и коды в ответе опроса; исключение — F0 (угроза жизни), он идёт обычным путём. Модель
// degraded — остаётся тон neg по regex. Тон в решении об обращении не участвует нигде.
// Что делать с флагом, решает код по data/taxonomy.yaml (правило UX 6), а не модель:
//   - уверенность ниже manual_review_floor — флаг только в агрегатах (коды в ответе опроса);
//   - escalate_if (severity ≥ 2 или критический код) и уверенность ≥ auto_escalation_threshold
//     категории — обращение по существующему пути: F5/F9 — сразу, как regex («Передаю…»); F7 —
//     предложение новичку с выбором, без обращения до согласия (UX 2.7); остальные — обращение
//     категории сразу с сообщением OPEN;
//   - иначе — только отметка риска в карточке новичка (жёлтая зона, причина — код и категория).
// Повтор флага в двух пульсах подряд — severity +1 (rules.repeat_bump, не выше 3).
//
// В лог — только коды, уверенность и id; текста новичка там нет.

// RAGFlagger — POST /flags. rag.Client его реализует; бот находит его через RAGAsker.
type RAGFlagger interface {
	Flags(ctx context.Context, req rag.FlagsRequest) (*rag.FlagsResponse, error)
}

// Откуда сигнал — в сводку обращения.
const (
	flagViaRegex = "в свободном тексте (regex)"
	flagViaLLM   = "по LLM-разметке"
	// flagViaOption — сигнал варианта ответа опроса (option_flags сценария), без LLM.
	flagViaOption = "по варианту ответа"
)

// flagInput — текст на разметку и где он написан.
type flagInput struct {
	text           string
	context        string // rag.FlagsContext*
	surveyQuestion string
	eventID        *uuid.UUID // ответ на опрос: коды пишутся в ответ, повтор считается по пульсам
	questionNo     int        // номер вопроса опроса, с единицы
	where          string     // «в сообщении новичка», «в ответе на опрос»
	via            string     // откуда сигнал; пусто — LLM-разметка
	// quiet — обращение без сообщения новичку (с ним говорит ветка варианта): только карточка людям.
	quiet bool
	// mid — сообщение новичка с этим текстом: на него реплаем «Передал…» (пусто — без реплая).
	mid string
	// questionID — свободный вопрос (BotQuestion) с этим текстом: в него пишется тональность.
	// Запись создаёт ask в фоне; questionReady закрывается, когда она создана (или не будет).
	questionID    *uuid.UUID
	questionReady <-chan struct{}
	// regexCodes — коды regex, по которым эскалация уже была: модель их не эскалирует повторно.
	regexCodes []string
	// consent — новичок согласился на обработку сообщений: цитата и строка модели пишутся в
	// свободный вопрос только тогда (как и сам текст вопроса).
	consent bool
	// notes — разметка модели (заполняет llmFlags): в запись сигнала F5/F7/F9 и в обращение.
	notes *modelNotes
}

// modelNotes — то, чем модель дополняет разметку (/flags v2): тон и уверенность в нём, цитата,
// строка для HR. Тон — подсказка: в решении об обращении не участвует.
type modelNotes struct {
	tone     string // domain.Sentiment*
	toneConf *float64
	quote    string // дословный фрагмент, rag проверил подстроку и ПДн; при F10 пусто
	summary  string // ≤ 120 символов, без ПДн; при F10 пусто
}

func notesFrom(r *rag.FlagsResponse) modelNotes {
	return modelNotes{tone: normSentiment(r.Sentiment), toneConf: r.ToneConfidence,
		quote: strings.TrimSpace(r.Quote), summary: strings.TrimSpace(r.Summary)}
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// sentimentWait — сколько разметка ждёт запись вопроса (ask до RAG_ASK_TIMEOUT и позднего окна).
var sentimentWait = 2 * time.Minute

// normSentiment — тон /flags в domain.Sentiment*. Неизвестное — neu (rag делает так же).
func normSentiment(s string) string {
	switch s {
	case domain.SentimentPos, domain.SentimentNeg:
		return s
	}
	return domain.SentimentNeu
}

// saveMarkup — разметка модели в запись текста: ответ опроса (eventID) или свободный вопрос
// (questionID). Тон модели заменяет neg, поставленный кодом по regex. У свободного вопроса без
// согласия — только тон и уверенность (текста вопроса там тоже нет). Обращение из этого вопроса,
// если уже есть, получает строку модели и цитату (noteQuestionTicket).
func (b *BotService) saveMarkup(ctx context.Context, in flagInput, m modelNotes) error {
	if in.eventID != nil {
		return b.repos.Transaction(ctx, func(tx *repositories.Repos) error {
			a, err := tx.Events.Answer(ctx, *in.eventID, in.questionNo)
			if err != nil || a == nil || strings.TrimSpace(a.Text) != strings.TrimSpace(in.text) {
				return err // текст уже переписан — его разметит следующий вызов
			}
			a.Sentiment, a.ToneConfidence = &m.tone, m.toneConf
			a.Quote, a.Summary = nonEmpty(m.quote), nonEmpty(m.summary)
			return tx.Events.UpsertAnswer(ctx, a)
		})
	}
	if in.questionID == nil {
		return nil // «свой вариант» ветки: текст живёт в обращении, своей записи нет
	}
	id := *in.questionID
	var quote, summary *string
	if in.consent {
		quote, summary = nonEmpty(m.quote), nonEmpty(m.summary)
	}
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		if in.questionReady != nil {
			select {
			case <-in.questionReady:
			case <-time.After(sentimentWait):
			}
		}
		if ok, err := b.repos.Questions.SetMarkup(ctx, id, m.tone, m.toneConf, quote, summary); err != nil {
			logger.Warn("бот: разметка вопроса %s не сохранена: %v", id, err)
			return
		} else if !ok {
			logger.Info("бот: разметка вопроса %s — записи нет, не сохранена", id)
			return
		}
		if err := b.noteQuestionTicket(ctx, id, summary, quote); err != nil {
			logger.Warn("бот: строка модели в обращение по вопросу %s не записана: %v", id, err)
		}
	}()
	return nil
}

// noteQuestionTicket — обращение, созданное из вопроса qid до прихода разметки (regex F5/F9):
// строка модели и цитата вместо полного текста. Полный текст остаётся в переписке обращения.
func (b *BotService) noteQuestionTicket(ctx context.Context, qid uuid.UUID, summary, quote *string) error {
	if b.tickets == nil || (summary == nil && quote == nil) {
		return nil
	}
	cur, err := b.repos.Tickets.ByQuestion(ctx, qid)
	if err != nil || cur == nil {
		return err
	}
	return b.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		t, err := tx.Tickets.ByIDForUpdate(ctx, cur.ID)
		if err != nil || t == nil {
			return err
		}
		e, err := tx.Employees.ByID(ctx, t.EmployeeID)
		if err != nil || e == nil {
			return err
		}
		if !setTicketNotes(e, b.tickets.category(t.Category, t.Route), t, summary, quote) {
			return nil
		}
		return tx.Tickets.Save(ctx, t)
	})
}

// setTicketNotes — строка модели в SummaryModel и цитата модели в Quote (вместо полного текста:
// он уже в переписке обращения). Правила те же, что для текста: согласие и store_quote категории
// (keepText); F10 — без цитаты и строки (rag их и не даёт). false — ничего не поменялось.
func setTicketNotes(e *domain.Employee, cat taxonomy.Category, t *domain.Ticket, summary, quote *string) bool {
	changed := false
	if summary != nil && *summary != "" && consented(e) && cat.StoreQuote {
		s := *summary
		t.SummaryModel, changed = &s, true
	}
	if quote != nil {
		if q := keepText(e, cat, *quote); q != "" {
			t.Quote, changed = &q, true
		}
	}
	return changed
}

// Что код делает с LLM-флагом.
const (
	flagActRisk   = "risk"   // только отметка риска
	flagActTicket = "ticket" // обращение сразу
	flagActOffer  = "offer"  // F7: предложение новичку, обращение — после согласия
)

// flagDecision — флаг после правил таксономии.
type flagDecision struct {
	code       string
	severity   int
	confidence float64
	repeated   bool
	cat        taxonomy.Category
	act        string
}

// signalCodes — коды с текстами сигнала по правилу трёх шагов (bot_ask_render.go): идут тем же
// путём, что regex.
var signalCodes = []string{"F5", "F7", "F9"}

// decideFlags — правила таксономии над разметкой. prev — коды прошлого пульса (повтор → +1).
// Порядок: сначала обращения, затем по severity и уверенности.
func decideFlags(tax *taxonomy.Taxonomy, in []rag.Flag, prev domain.StringList) []flagDecision {
	var out []flagDecision
	for _, f := range in {
		cat, ok := tax.Get(f.Code)
		if !ok || f.Confidence < tax.ManualReviewFloor || f.Severity < 1 || f.Severity > 3 {
			continue
		}
		d := flagDecision{code: f.Code, severity: f.Severity, confidence: f.Confidence, cat: cat, act: flagActRisk}
		if prev.Has(f.Code) {
			d.repeated, d.severity = true, min(3, d.severity+1)
		}
		if d.confidence >= cat.AutoThreshold && tax.Escalates(d.code, d.severity) {
			d.act = flagActTicket
			if d.code == "F7" {
				d.act = flagActOffer
			}
		}
		out = append(out, d)
	}
	slices.SortStableFunc(out, func(a, b flagDecision) int {
		if (a.act == flagActRisk) != (b.act == flagActRisk) {
			if a.act == flagActRisk {
				return 1
			}
			return -1
		}
		if a.severity != b.severity {
			return b.severity - a.severity
		}
		switch {
		case a.confidence > b.confidence:
			return -1
		case a.confidence < b.confidence:
			return 1
		}
		return 0
	})
	return out
}

// flagger — клиент /flags, если rag подключён.
func (b *BotService) flagger() RAGFlagger {
	if b.rag == nil || b.tickets == nil {
		return nil
	}
	f, _ := b.rag.(RAGFlagger)
	return f
}

// classifyAsync — /flags в фоне. Ошибка и degraded — только строка в лог: regex уже отработал.
func (b *BotService) classifyAsync(ctx context.Context, emp *domain.Employee, in flagInput) {
	f := b.flagger()
	if f == nil || strings.TrimSpace(in.text) == "" {
		return
	}
	in.consent = consented(emp)
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		if err := b.llmFlags(context.WithoutCancel(ctx), f, emp.ID, in); err != nil {
			logger.Warn("бот: LLM-флаги новичка %s не обработаны: %v", emp.ID, err)
		}
	}()
}

func (b *BotService) llmFlags(ctx context.Context, f RAGFlagger, empID uuid.UUID, in flagInput) error {
	req := rag.FlagsRequest{Text: in.text, Context: in.context}
	if in.surveyQuestion != "" {
		sq := in.surveyQuestion
		req.SurveyQuestion = &sq
	}
	resp, err := f.Flags(ctx, req)
	if err != nil {
		logger.Warn("бот: rag /flags не ответил (новичок %s): %v", empID, err)
		return nil
	}
	if resp.Degraded {
		// при regex тон уже neg кодом — так и остаётся
		logger.Warn("бот: rag /flags без LLM (degraded), новичок %s: разметки нет, только regex", empID)
		return nil
	}
	m := notesFrom(resp)
	in.notes = &m
	if err := b.saveMarkup(ctx, in, m); err != nil {
		logger.Warn("бот: разметка новичка %s не сохранена: %v", empID, err)
	}
	model := resp.Flags
	if len(in.regexCodes) > 0 {
		merged := mergeFlags(in.regexCodes, resp.Flags)
		logger.Info("бот: regex %v + модель → %s (новичок %s)", in.regexCodes, flagsLog(merged), empID)
		model = extraFlags(merged, in.regexCodes)
	}
	var prev domain.StringList
	if in.eventID != nil {
		if prev, err = b.prevPulseCodes(ctx, empID, *in.eventID); err != nil {
			return err
		}
	}
	ds := decideFlags(b.tickets.tax, model, prev)
	if len(in.regexCodes) > 0 {
		ds = noReescalation(ds)
	}
	for _, d := range ds {
		logger.Info("бот: LLM-флаг %s sev %d conf %.2f повтор %v → %s (новичок %s)", d.code, d.severity, d.confidence, d.repeated, d.act, empID)
	}
	if len(ds) > 0 {
		if err := b.applyFlagDecisions(ctx, empID, in, ds); err != nil {
			return err
		}
	}
	b.maybeClarify(ctx, empID, in, resp)
	return nil
}

// regexSeverity, regexConfidence — regex-кандидат при слиянии: основа слова названа прямо
// (уверенность 1), серьёзность «мешает» — модель может поднять до 3.
const (
	regexSeverity   = 2
	regexConfidence = 1.0
)

// mergeFlags — regex и модель в одну разметку: по коду — максимальная серьёзность и
// максимальная уверенность; порядок — по серьёзности, затем уверенности (первый — итоговый код).
func mergeFlags(regex []string, model []rag.Flag) []rag.Flag {
	var out []rag.Flag
	idx := map[string]int{}
	add := func(f rag.Flag) {
		if i, ok := idx[f.Code]; ok {
			out[i].Severity = max(out[i].Severity, f.Severity)
			out[i].Confidence = max(out[i].Confidence, f.Confidence)
			return
		}
		idx[f.Code] = len(out)
		out = append(out, f)
	}
	for _, c := range regex {
		add(rag.Flag{Code: c, Severity: regexSeverity, Confidence: regexConfidence})
	}
	for _, f := range model {
		add(f)
	}
	slices.SortStableFunc(out, func(a, b rag.Flag) int {
		if a.Severity != b.Severity {
			return b.Severity - a.Severity
		}
		switch {
		case a.Confidence > b.Confidence:
			return -1
		case a.Confidence < b.Confidence:
			return 1
		}
		return 0
	})
	return out
}

// extraFlags — флаги слияния, которых не было в regex: только их код ещё обрабатывает.
func extraFlags(merged []rag.Flag, regex []string) []rag.Flag {
	var out []rag.Flag
	for _, f := range merged {
		if !slices.Contains(regex, f.Code) {
			out = append(out, f)
		}
	}
	return out
}

// noReescalation — regex уже эскалировал: новые коды модели — только риск и коды в ответе опроса,
// без второго обращения и второго «Передаю…». Кроме F0: угрозу жизни не глушим.
func noReescalation(ds []flagDecision) []flagDecision {
	for i := range ds {
		if ds[i].act != flagActRisk && ds[i].code != "F0" {
			ds[i].act = flagActRisk
		}
	}
	return ds
}

func flagsLog(fs []rag.Flag) string {
	parts := make([]string, 0, len(fs))
	for _, f := range fs {
		parts = append(parts, fmt.Sprintf("%s/%d/%.2f", f.Code, f.Severity, f.Confidence))
	}
	return "[" + strings.Join(parts, " ") + "]"
}

// Уточняющий вопрос по подсказке модели (clarify.go, FEATURE_CLARIFY): уверенность флага этого
// кода в окне 0,3–0,6 — проблема угадывается, но неясна. Только ответы опроса и не при regex.
const (
	clarifyConfMin = 0.3
	clarifyConfMax = 0.6
)

func (b *BotService) maybeClarify(ctx context.Context, empID uuid.UUID, in flagInput, resp *rag.FlagsResponse) {
	hint := resp.Hint()
	if hint == "" || in.eventID == nil || len(in.regexCodes) > 0 || in.questionNo < 1 {
		return
	}
	for _, f := range resp.Flags {
		if f.Code == hint && f.Confidence >= clarifyConfMin && f.Confidence <= clarifyConfMax {
			if b.offerClarify(ctx, empID, *in.eventID, in.questionNo-1, hint) {
				logger.Info("бот: уточнение %s по подсказке модели (новичок %s, вопрос %d)", hint, empID, in.questionNo)
			}
			return
		}
	}
}

// applyFlagDecisions — риск и коды в ответ опроса, затем обращения и сигналы.
func (b *BotService) applyFlagDecisions(ctx context.Context, empID uuid.UUID, in flagInput, ds []flagDecision) error {
	var emp *domain.Employee
	err := b.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		e, err := tx.Employees.ByIDForUpdate(ctx, empID)
		if err != nil || e == nil {
			return err
		}
		emp = e
		var risk []string
		level := domain.RiskYellow
		for _, d := range ds {
			if d.act == flagActRisk {
				risk = append(risk, riskLabel(d))
				if in.via == flagViaOption && d.severity >= 3 {
					level = domain.RiskRed // «задумываюсь об уходе»: красная зона без обращения
				}
			}
		}
		switch {
		case len(risk) > 0 && in.via == flagViaOption:
			// сигнал варианта ответа: обращение — по кнопке или тексту новичка в ветке
			markRisk(e, level, fmt.Sprintf("сигнал %s %s (%s)", strings.Join(risk, ", "), in.where, flagViaOption))
			if err := tx.Employees.Save(ctx, e); err != nil {
				return err
			}
		case len(risk) > 0:
			markRisk(e, domain.RiskYellow, fmt.Sprintf("возможный сигнал %s %s (%s, на ручной просмотр HR)", strings.Join(risk, ", "), in.where, orDefault(in.via, "LLM-разметка")))
			if err := tx.Employees.Save(ctx, e); err != nil {
				return err
			}
		}
		if in.eventID == nil {
			return nil
		}
		a, err := tx.Events.Answer(ctx, *in.eventID, in.questionNo)
		if err != nil || a == nil {
			return err
		}
		changed := false
		for _, d := range ds {
			if !a.Flags.Has(d.code) {
				a.Flags, changed = append(a.Flags, d.code), true
			}
		}
		if !changed {
			return nil
		}
		return tx.Events.UpsertAnswer(ctx, a)
	})
	if err != nil || emp == nil {
		return err
	}

	var signal []string
	for _, d := range ds {
		switch {
		case d.act == flagActRisk:
		case slices.Contains(signalCodes, d.code):
			signal = append(signal, d.code)
		default:
			if err := b.flagTicket(ctx, emp, in, d); err != nil {
				return err
			}
		}
	}
	if len(signal) == 0 {
		return nil
	}
	q := &domain.BotQuestion{EmployeeID: emp.ID, TraceID: newTraceID(), Flags: domain.StringList{}, Sources: domain.SourceList{}, IsTest: emp.IsTest}
	q.CreatedAt = b.now()
	q.SourceMID = midOf(in.mid)
	if n := in.notes; n != nil {
		q.Sentiment, q.ToneConfidence = strPtr(n.tone), n.toneConf
		if consented(emp) {
			q.Quote, q.Summary = nonEmpty(n.quote), nonEmpty(n.summary)
		}
	}
	if consented(emp) {
		q.Question = strings.TrimSpace(in.text)
	}
	return b.flagSignal(ctx, recipientOf(emp), emp, q, signal, orDefault(in.via, flagViaLLM)+" "+in.where)
}

// riskLabel — «F6 «Перегрузка, выгорание»», с пометкой повтора.
func riskLabel(d flagDecision) string {
	s := fmt.Sprintf("%s «%s»", d.code, d.cat.Name)
	if d.repeated {
		s += " повторно"
	}
	return s
}

// flagTicket — обращение категории сразу (F0–F4, F6, F8, F10): риск, HumanRequest, OPEN новичку.
func (b *BotService) flagTicket(ctx context.Context, emp *domain.Employee, in flagInput, d flagDecision) error {
	var res handoffResult
	var e *domain.Employee
	var merged int64
	err := b.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		var err error
		if e, err = tx.Employees.ByIDForUpdate(ctx, emp.ID); err != nil || e == nil {
			return err
		}
		now := b.now()
		e.LastActivityAt = &now
		markRisk(e, domain.RiskRed, fmt.Sprintf("сигнал %s «%s» %s (%s)", d.code, d.cat.Name, in.where, orDefault(in.via, "LLM-разметка")))
		if err := tx.Employees.Save(ctx, e); err != nil {
			return err
		}
		// Ответ на опрос (свой вариант, комментарий) при открытом обращении той же категории: текст
		// принадлежит опросу — в обращение он не дописывается, «уже передано» новичку не пишем.
		if in.eventID != nil {
			cur, err := tx.Tickets.Active(ctx, e.ID, d.cat.Code)
			if err != nil {
				return err
			}
			if cur != nil {
				merged = cur.Number
				return nil
			}
		}
		h := domain.HumanRequest{EmployeeID: e.ID, EventID: in.eventID, Source: domain.HumanFromFreeText, Flags: domain.StringList{d.code}, Target: categoryTarget(d.cat)}
		if err := tx.Employees.AddHumanRequest(ctx, &h); err != nil {
			return err
		}
		what := fmt.Sprintf("Сигнал %s %s %s (уверенность %.2f, серьёзность %d", d.code, orDefault(in.via, flagViaLLM), in.where, d.confidence, d.severity)
		if d.repeated {
			what += ", повтор во втором пульсе подряд"
		}
		t, created, err := b.tickets.open(ctx, tx, e, ticketDraft{
			cat: d.cat, source: domain.TicketFromFlag, quote: in.text, humanRequestID: &h.ID, eventID: in.eventID,
			what: what + "), передано сразу.",
		})
		if err != nil {
			return err
		}
		// строка модели и её цитата — в карточку; полный текст open уже положил в переписку
		if n := in.notes; created && n != nil && setTicketNotes(e, d.cat, t, nonEmpty(n.summary), nonEmpty(n.quote)) {
			if err := tx.Tickets.Save(ctx, t); err != nil {
				return err
			}
		}
		res = handoffResult{ticket: t, created: created, lead: flagLead(e, d.code), replyTo: in.mid}
		return nil
	})
	if err != nil || e == nil {
		return err
	}
	if merged > 0 {
		logger.Info("бот: флаг %s новичка %s в ответе опроса — уже открыто обращение #%d, только риск", d.code, emp.ID, merged)
		return nil
	}
	logger.Info("бот: LLM-флаг %s новичка %s → обращение #%d", d.code, emp.ID, res.ticket.Number)
	if in.quiet {
		if res.created {
			b.tickets.notify(ctx, res.ticket, e)
		}
		return nil
	}
	b.afterHandoff(ctx, e, &res)
	return nil
}

// categoryTarget — HumanRequest.Target по маршруту категории.
func categoryTarget(cat taxonomy.Category) string {
	switch {
	case cat.Route == taxonomy.RouteSecurity:
		return domain.TargetSecurity
	case cat.PartnerOnly:
		return domain.TargetHRPartner
	case cat.Route == taxonomy.RouteRecruiter:
		return domain.TargetRecruiter
	}
	return domain.TargetHR
}

// flagLeads — первая строка перед OPEN: признать без оценки (правило трёх шагов, UX 2.7). F0 —
// сразу экстренный номер, без обещаний «мы приедем».
var flagLeads = map[string][2]string{ // [ты, вы]
	"F0":  {"Если есть угроза жизни или здоровью прямо сейчас — звони 112.", "Если есть угроза жизни или здоровью прямо сейчас — звоните 112."},
	"F1":  {"Понял: с задачами пока не всё ясно.", "Понял: с задачами пока не всё ясно."},
	"F2":  {"Понял: для работы чего-то не хватает.", "Понял: для работы чего-то не хватает."},
	"F3":  {"Понял: не хватает связи с руководителем.", "Понял: не хватает связи с руководителем."},
	"F4":  {"Понял: с наставником или коллегами сейчас непросто.", "Понял: с наставником или коллегами сейчас непросто."},
	"F6":  {"Понял: нагрузка сейчас большая.", "Понял: нагрузка сейчас большая."},
	"F8":  {"Понял: условия не совпали с тем, что обещали.", "Понял: условия не совпали с тем, что обещали."},
	"F10": {"Спасибо, что предупредил(а).", "Спасибо, что предупредили."},
}

func flagLead(e *domain.Employee, code string) string {
	l, ok := flagLeads[code]
	if !ok {
		return ""
	}
	return byForm(e, l[0], l[1])
}

// prevPulseCodes — коды флагов в ответах прошлого опроса новичка (перед событием cur): для
// повтора «в двух пульсах подряд». Прошлый опрос без флагов — пусто.
func (b *BotService) prevPulseCodes(ctx context.Context, empID, cur uuid.UUID) (domain.StringList, error) {
	evs, err := b.repos.Events.ByEmployee(ctx, empID)
	if err != nil {
		return nil, err
	}
	var current *domain.ScheduledEvent
	for i := range evs {
		if evs[i].ID == cur {
			current = &evs[i]
		}
	}
	if current == nil {
		return nil, nil
	}
	var prev *domain.ScheduledEvent
	for i := range evs {
		ev := &evs[i]
		if ev.ID == cur || ev.Survey == nil || !ev.FireAt.Before(current.FireAt) {
			continue
		}
		if ev.Status != domain.EventAnswered && ev.Status != domain.EventSent {
			continue
		}
		if prev == nil || ev.FireAt.After(prev.FireAt) {
			prev = ev
		}
	}
	if prev == nil {
		return nil, nil
	}
	answers, err := b.repos.Events.Answers(ctx, prev.ID)
	if err != nil {
		return nil, err
	}
	var codes domain.StringList
	for _, a := range answers {
		for _, c := range a.Flags {
			if !codes.Has(c) {
				codes = append(codes, c)
			}
		}
	}
	return codes, nil
}
