package services

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"main/config"
	"main/domain"
	"main/externalApi"
	"main/externalApi/max"
	"main/externalApi/rag"
	"main/repositories"

	"github.com/google/uuid"
)

// TestTZConformance — прогон настоящего Excel-ТЗ заказчика (файл конфиденциален, в репозиторий
// не кладём) от D−3 до конца ИС на подделке MAX: импорт → публикация → по новичку на каждую
// площадку × форму обращения × манеру ответа («основной», «проблемный», «молчун»).
//
//	TZ_XLSX=/путь/к/ТЗ.xlsx CONFORM_DATABASE_URL=postgres://…/polyplastic_conform?sslmode=disable \
//	TZ_JOURNAL=/tmp/conform go test ./services -run TestTZConformance -v -timeout 30m
//
// Без TZ_XLSX или базы пропускается. Журнал ленты каждого новичка (с текстами ТЗ) пишется в
// TZ_JOURNAL — вне репозитория. В лог теста — только сводка без текстов.
func TestTZConformance(t *testing.T) {
	path := os.Getenv("TZ_XLSX")
	if path == "" {
		t.Skip("TZ_XLSX не задан")
	}
	dsn := os.Getenv("CONFORM_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("TEST_DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("CONFORM_DATABASE_URL / TEST_DATABASE_URL не заданы")
	}
	outDir := os.Getenv("TZ_JOURNAL")
	if outDir == "" {
		outDir = t.TempDir()
	}
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		t.Fatal(err)
	}
	c := newConformRun(t, dsn, path)
	c.run(t)
	c.report(t, outDir)
}

// tracingMax — подделка MAX, которая помнит имя загруженного файла по токену: в журнале видно,
// файл какой площадки ушёл.
type tracingMax struct {
	*max.Fake
	mu    sync.Mutex
	names map[string]string
}

func (m *tracingMax) Upload(ctx context.Context, kind max.UploadKind, r io.Reader, name string, size int64) (string, error) {
	tok, err := m.Fake.Upload(ctx, kind, r, name, size)
	if err == nil {
		m.mu.Lock()
		m.names[tok] = name
		m.mu.Unlock()
	}
	return tok, err
}

func (m *tracingMax) nameOf(tok string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n, ok := m.names[tok]; ok {
		return n
	}
	return tok
}

type cPersona struct {
	name, site, form, mode string
	uid                    int64
	card                   *NewcomerCard
	acted                  map[string]bool
	at                     map[int]time.Time // seq записи журнала → виртуальное время
	notes                  []string          // что сделал «новичок» помимо кнопок
	said                   map[string]bool   // разовые действия (команды, свободный вопрос)
}

type conformRun struct {
	t        *testing.T
	ctx      context.Context
	repos    *repositories.Repos
	svc      *Services
	mx       *tracingMax
	clock    *time.Time
	start    time.Time
	report_  ImportReport
	scenario uuid.UUID
	ps       []*cPersona
}

func newConformRun(t *testing.T, dsn, path string) *conformRun {
	t.Helper()
	repos, err := repositories.InitRepos(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := repos.MigrateDB(); err != nil {
		t.Fatal(err)
	}
	if err := repos.Truncate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := repos.Media.TruncateMedia(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Понедельник 9 ноября 2026 — выход; D−3 — пятница 6-го.
	start := date(2026, 11, 9)
	clock := msk(2026, 11, 6, 9, 0)
	ragSrv := newFakeRAG(t)
	ragSrv.set(ragAbstain)
	mx := &tracingMax{Fake: max.NewFake(), names: map[string]string{}}
	svc := NewServices(repos, &externalApi.ExternalAPI{Max: mx, RAG: rag.New(ragSrv.srv.URL, 5*time.Second)}, Options{
		Bot: config.Bot{Username: "korsik_conform_bot", InviteLinkFormat: config.DefaultInviteLinkFormat},
		Now: func() time.Time { return clock },
	})
	c := &conformRun{t: t, ctx: context.Background(), repos: repos, svc: svc, mx: mx, clock: &clock, start: start}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	res, err := svc.Scenarios.ImportExcel(c.ctx, "ТЗ заказчика (сверка)", f)
	if err != nil {
		t.Fatal(err)
	}
	c.report_, c.scenario = res.Report, res.Detail.Scenario.ID
	c.uploadMedia(t, res.Report.MediaNameList)
	if _, err := svc.Scenarios.Publish(c.ctx, c.scenario, "сверка", ApplyNew); err != nil {
		t.Fatal(err)
	}

	uid := int64(910000)
	for _, site := range []string{domain.SiteGrPP, domain.SiteLuzhniki, domain.SiteGTP} {
		for _, mode := range []string{"main", "problem", "silent"} {
			for _, form := range []string{domain.AddressTy, domain.AddressVy} {
				uid++
				p := &cPersona{name: fmt.Sprintf("%s-%s-%s", site, mode, form), site: site, form: form, mode: mode, uid: uid,
					acted: map[string]bool{}, at: map[int]time.Time{}, said: map[string]bool{}}
				card, err := svc.Newcomers.Create(c.ctx, NewcomerInput{
					FullName: "Сверкин Иван (синтетика " + p.name + ")", Site: site, StartDate: start, ScenarioID: c.scenario,
				})
				if err != nil {
					t.Fatal(err)
				}
				p.card = card
				if mode == "main" {
					if _, err := svc.UploadNewcomerFile(c.ctx, card.Employee.ID, "adaptation_plan", "plan.pdf", "application/pdf", []byte("%PDF-1.4 plan")); err != nil {
						t.Fatal(err)
					}
				}
				c.ps = append(c.ps, p)
			}
		}
	}
	return c
}

// uploadMedia — файлы для всех имён медиатеки: по файлу на площадку, имя файла «<имя>@<площадка>».
func (c *conformRun) uploadMedia(t *testing.T, names []string) {
	t.Helper()
	all := append([]string{}, names...)
	for _, cmd := range c.mustScenario(t).Commands {
		for _, a := range cmd.Actions {
			if a.Media != "" {
				all = append(all, a.Media)
			}
		}
	}
	res, err := c.svc.Media.Ensure(c.ctx, all, "", "сверка")
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range res.Items {
		for _, site := range []string{domain.SiteGrPP, domain.SiteLuzhniki, domain.SiteGTP} {
			u := MediaUpload{Site: site, Kind: it.Media.Kind, By: "сверка"}
			if it.Media.Kind == domain.MediaKindFile {
				u.Filename, u.Mime, u.Data = it.Media.Name+"@"+site+".pdf", "application/pdf", []byte("%PDF-1.4 "+site)
			} else {
				u.Filename, u.Mime, u.Data = it.Media.Name+"@"+site+".png", "image/png", []byte("\x89PNG\r\n\x1a\n"+site)
			}
			if _, err := c.svc.Media.PutFile(c.ctx, it.Media.ID, u); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func (c *conformRun) mustScenario(t *testing.T) domain.Scenario {
	t.Helper()
	d, err := c.svc.Scenarios.Get(c.ctx, c.scenario)
	if err != nil {
		t.Fatal(err)
	}
	return d.Scenario
}

func (c *conformRun) emp(p *cPersona) *domain.Employee {
	e, err := c.repos.Employees.ByID(c.ctx, p.card.Employee.ID)
	if err != nil || e == nil {
		c.t.Fatalf("сотрудник %s: %v", p.name, err)
	}
	return e
}

// stamp — виртуальное время новым записям журнала.
func (c *conformRun) stamp(p *cPersona) {
	entries, _ := c.mx.Chat(p.uid, p.uid)
	for _, e := range entries {
		if _, ok := p.at[e.Seq]; !ok {
			p.at[e.Seq] = *c.clock
		}
	}
}

func (c *conformRun) press(p *cPersona, payload string) {
	cb := c.mx.JournalPress(p.uid, p.uid, "", payload)
	c.svc.Bot.Handle(c.ctx, max.Update{Type: max.UpdateMessageCallback, UserID: p.uid, ChatID: p.uid, Payload: payload, CallbackID: cb})
	c.svc.Bot.Wait()
	c.stamp(p)
}

func (c *conformRun) say(p *cPersona, text string, images int) {
	shown := text
	if images > 0 {
		shown += fmt.Sprintf(" [+фото ×%d]", images)
	}
	c.mx.JournalIncoming(p.uid, p.uid, max.JournalKindText, shown)
	u := max.Update{Type: max.UpdateMessageCreated, UserID: p.uid, ChatID: p.uid, Text: text}
	for i := 0; i < images; i++ {
		u.Images = append(u.Images, max.Image{URL: "https://example.invalid/photo" + strconv.Itoa(i) + ".jpg", Token: "img-tok-" + strconv.Itoa(i)})
	}
	c.svc.Bot.Handle(c.ctx, u)
	c.svc.Bot.Wait()
	c.stamp(p)
}

func (c *conformRun) connect(p *cPersona) {
	tok := p.card.InviteLink[strings.Index(p.card.InviteLink, "start=")+len("start="):]
	c.mx.JournalIncoming(p.uid, p.uid, max.JournalKindStart, "Начать")
	c.svc.Bot.Handle(c.ctx, max.Update{Type: max.UpdateBotStarted, UserID: p.uid, ChatID: p.uid, Payload: tok})
	c.svc.Bot.Wait()
	c.press(p, "a:"+p.form)
	c.press(p, payConsentYes)
}

// run — машина времени шагом 15 минут от D−3 09:00 до D+92.
func (c *conformRun) run(t *testing.T) {
	for _, p := range c.ps {
		c.connect(p)
	}
	end := c.start.AddDate(0, 0, 93)
	for ; c.clock.Before(end); *c.clock = c.clock.Add(15 * time.Minute) {
		if _, err := c.svc.Scheduler.Tick(c.ctx); err != nil {
			t.Fatalf("такт %v: %v", *c.clock, err)
		}
		c.svc.Bot.Wait()
		for _, p := range c.ps {
			c.stamp(p)
			c.recruiter(p)
			c.initiative(p)
			c.react(p)
		}
	}
}

// day — день адаптации по календарю (как D в ТЗ).
func (c *conformRun) day() int {
	h := c.clock.Sub(time.Date(c.start.Year(), c.start.Month(), c.start.Day(), 0, 0, 0, 0, c.clock.Location())).Hours()
	return int(math.Floor(h / 24))
}

// recruiter — отмашка рекрутера (r34, r36): утром D+1 у «основного» и «проблемного».
func (c *conformRun) recruiter(p *cPersona) {
	if p.mode == "silent" || p.said["flag"] || c.day() < 1 || c.clock.Hour() < 9 {
		return
	}
	p.said["flag"] = true
	if _, err := c.svc.SetNewcomerFlag(c.ctx, p.card.Employee.ID, "recruiter_enabled", true); err != nil {
		c.t.Fatal(err)
	}
	c.svc.Bot.Wait()
	c.stamp(p)
	p.notes = append(p.notes, c.clock.Format("02.01 15:04")+" рекрутер включил recruiter_enabled")
}

// initiative — «проблемный» сам пишет: /food, свободный вопрос (r69), /hr; «основной» —
// свободный вопрос на D+4 (у него нет открытой переписки с HR, отвечает правило r69).
func (c *conformRun) initiative(p *cPersona) {
	if p.mode == "silent" || c.clock.Hour() != 16 || c.clock.Minute() != 0 {
		return
	}
	if p.mode == "main" {
		if c.day() == 4 && !p.said["free"] {
			p.said["free"] = true
			c.say(p, "Можно ли приносить на работу свою кофемашину?", 0)
			c.react(p)
		}
		return
	}
	d := c.day()
	do := func(key, text string) {
		if p.said[key] {
			return
		}
		p.said[key] = true
		c.say(p, text, 0)
		c.react(p)
	}
	switch d {
	case 0:
		do("food", "/Food")
	case 5:
		do("free", "Сколько стоит парковка у офиса для сотрудников?")
	case 31:
		do("hr", "/HR")
	}
}

type screen struct {
	key     string
	text    string
	buttons [][]max.JournalButton
}

// screens — сообщения бота с callback-кнопками (кроме одной «Спросить человека»), новые первыми.
func (c *conformRun) screens(p *cPersona) []screen {
	entries, _ := c.mx.Chat(p.uid, p.uid)
	var out []screen
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.From != max.JournalFromBot {
			continue
		}
		var cb int
		var sig []string
		for _, row := range e.Buttons {
			for _, b := range row {
				if b.Type == max.ButtonTypeCallback && b.Text != btnHuman {
					cb++
				}
				sig = append(sig, b.Payload)
			}
		}
		if cb == 0 {
			continue
		}
		h := sha1.Sum([]byte(e.ID + "\x00" + e.Text + "\x00" + strings.Join(sig, "|")))
		out = append(out, screen{key: hex.EncodeToString(h[:8]), text: e.Text, buttons: e.Buttons})
	}
	return out
}

func flatButtons(s screen) []max.JournalButton {
	var out []max.JournalButton
	for _, row := range s.buttons {
		for _, b := range row {
			if b.Type == max.ButtonTypeCallback {
				out = append(out, b)
			}
		}
	}
	return out
}

func byLabel(bs []max.JournalButton, label string) (string, bool) {
	for _, b := range bs {
		if b.Text == label {
			return b.Payload, true
		}
	}
	return "", false
}

// react — «новичок» отвечает на самый свежий экран, пока бот присылает новые.
func (c *conformRun) react(p *cPersona) {
	if p.mode == "silent" {
		return
	}
	for guard := 0; guard < 40; guard++ {
		if c.answerText(p) {
			continue
		}
		ss := c.screens(p)
		pay := ""
		// свежие экраны по очереди: сообщение об обращении (флаг варианта) может прийти после
		// вопроса опроса — новичок отвечает на вопрос, а не застревает на нём
		for i := 0; i < len(ss) && i < 4 && pay == ""; i++ {
			if p.acted[ss[i].key] {
				continue
			}
			p.acted[ss[i].key] = true
			pay = c.choose(p, ss[i])
		}
		if pay == "" {
			return
		}
		c.press(p, pay)
	}
}

// answerText — бот ждёт текст (ввод, «свой ответ», причина): ответить.
func (c *conformRun) answerText(p *cPersona) bool {
	e := c.emp(p)
	switch {
	case e.AwaitFreeText != nil && e.AwaitFreeText.Input:
		if p.mode == "main" {
			c.say(p, "Опыт: 5 лет в логистике, навыки: SAP, Excel. Достижения: сократил простои на 15%.", 2)
		} else {
			c.say(p, "Пока не готов писать о себе.", 0)
		}
		return true
	case e.AwaitFreeText != nil:
		c.say(p, "Проблема: пропуск не выдали, документы не успел прочитать, задачи никто не объяснил.", 0)
		return true
	case e.AwaitEventID != nil:
		c.say(p, "Причина: руководитель занят, ответов на вопросы приходится ждать по несколько дней.", 0)
		return true
	}
	return false
}

// choose — какую кнопку нажать на экране по манере новичка.
func (c *conformRun) choose(p *cPersona, s screen) string {
	bs := flatButtons(s)
	var step, answers, terminal []max.JournalButton
	var cont, done, other, comment string
	for _, b := range bs {
		switch {
		case b.Text == btnHuman, b.Text == btnRemind, b.Text == btnSkip, b.Text == btnWriteOwn:
		case strings.HasPrefix(b.Payload, preContinue):
			cont = b.Payload
		case strings.HasPrefix(b.Payload, preStepButton):
			step = append(step, b)
		case strings.HasPrefix(b.Payload, preTerminal):
			terminal = append(terminal, b)
		case strings.HasPrefix(b.Payload, preAnswer):
			answers = append(answers, b)
		case strings.HasPrefix(b.Payload, preDone):
			done = b.Payload
		case b.Text == btnOther:
			other = b.Payload
		case b.Text == btnAddComment:
			comment = b.Payload
		}
	}
	_ = comment
	problem := p.mode == "problem"
	switch {
	case len(step) > 0:
		// «Как мы будем общаться?» — по своей форме
		if pay, ok := byLabel(step, map[string]string{domain.AddressTy: "На ТЫ", domain.AddressVy: "На ВЫ"}[p.form]); ok {
			return pay
		}
		if problem {
			return step[len(step)-1].Payload
		}
		return step[0].Payload
	case len(terminal) > 0:
		if !problem {
			return ""
		}
		if pay, ok := byLabel(terminal, "Чат с HR"); ok {
			return pay
		}
		return terminal[0].Payload
	case strings.HasPrefix(firstPayload(bs), preReason):
		return firstPayload(bs)
	case len(answers) > 0:
		multi := strings.HasPrefix(firstPayload(answers), preAnswer) && done != ""
		if multi {
			// «Кто помогает» — основной: первый вариант; проблемный: последний («Никто!»)
			pick := answers[0]
			if problem {
				pick = answers[len(answers)-1]
			}
			if strings.HasPrefix(pick.Text, "✓") {
				return done
			}
			return pick.Payload
		}
		if problem && other != "" {
			return other
		}
		numeric := len(answers) >= 5
		if numeric {
			if problem {
				if pay, ok := byLabel(answers, "5"); ok {
					return pay
				}
			}
			return answers[len(answers)-1].Payload
		}
		if problem {
			return answers[len(answers)-1].Payload
		}
		return answers[0].Payload
	case done != "":
		return done
	case cont != "":
		return cont
	}
	// экраны вне сценария (ответ RAG, обращения): «проблемный» передаёт рекрутеру
	if problem {
		for _, b := range bs {
			if strings.HasPrefix(b.Payload, preQRecruiter) {
				return b.Payload // r69: «передать через Корсика» — вопрос уходит рекрутеру
			}
		}
		if pay, ok := byLabel(bs, btnToRecruiter); ok {
			return pay
		}
		if pay, ok := byLabel(bs, "Передать рекрутеру"); ok {
			return pay
		}
	}
	return ""
}

func firstPayload(bs []max.JournalButton) string {
	if len(bs) == 0 {
		return ""
	}
	return bs[0].Payload
}

// report — журнал каждого новичка (TZ_JOURNAL/<имя>.txt), события, обращения; сводка в лог.
func (c *conformRun) report(t *testing.T, dir string) {
	var sum []string
	sum = append(sum, fmt.Sprintf("импорт: блоков %d, сообщений %d, вопросов %d, веток %d, опросов %d, правил %d, команд %d, предупреждений %d",
		c.report_.Blocks, c.report_.Messages, c.report_.Questions, c.report_.Branches, c.report_.Surveys, c.report_.Rules, c.report_.Commands, len(c.report_.Warnings)))
	for _, p := range c.ps {
		var b strings.Builder
		entries, _ := c.mx.Chat(p.uid, p.uid)
		msgs, btns, atts, edits := 0, 0, 0, 0
		for _, e := range entries {
			at := p.at[e.Seq]
			day := int(at.Sub(time.Date(c.start.Year(), c.start.Month(), c.start.Day(), 0, 0, 0, 0, at.Location())).Hours()) / 24
			if at.Before(c.start) {
				day = -int(c.start.Sub(at).Hours()/24) - 1
			}
			fmt.Fprintf(&b, "#%d D%+d %s [%s", e.Seq, day, at.Format("02.01 15:04"), e.From)
			if e.Kind != "" {
				b.WriteString("/" + e.Kind)
			}
			b.WriteString("]")
			if e.Edited {
				b.WriteString(" (правлено)")
				edits++
			}
			b.WriteString("\n")
			if e.From == max.JournalFromBot {
				msgs++
			}
			for _, line := range strings.Split(e.Text, "\n") {
				b.WriteString("    " + line + "\n")
			}
			for _, a := range e.Attachments {
				atts++
				fmt.Fprintf(&b, "    📎 %s: %s\n", a.Type, c.mx.nameOf(a.URL))
			}
			for _, row := range e.Buttons {
				var ls []string
				for _, x := range row {
					ls = append(ls, "["+x.Text+"]")
					if x.Type == max.ButtonTypeCallback && x.Text != btnHuman {
						btns++
					}
				}
				b.WriteString("    " + strings.Join(ls, " ") + "\n")
			}
		}
		b.WriteString("\n=== события\n")
		evs, err := c.repos.Events.ByEmployee(c.ctx, p.card.Employee.ID)
		if err != nil {
			t.Fatal(err)
		}
		sort.SliceStable(evs, func(i, j int) bool { return evs[i].FireAt.Before(evs[j].FireAt) })
		status := map[string]int{}
		for _, ev := range evs {
			status[ev.Status]++
			fmt.Fprintf(&b, "%-14s %-9s %-10s wait=%-9s %s | %s\n", ev.Block, ev.Status, ev.FireAt.Format("02.01 15:04"), ev.Waiting, truncateRunes(ev.Title, 50), deref(ev.Answer))
		}
		b.WriteString("\n=== обращения\n")
		tks, err := c.repos.Tickets.List(c.ctx, repositories.TicketFilter{Newcomer: &p.card.Employee.ID})
		if err != nil {
			t.Fatal(err)
		}
		for i := len(tks) - 1; i >= 0; i-- {
			tk := tks[i]
			fmt.Fprintf(&b, "#%d день %d %s %s route=%s status=%s src=%s | %s\n", tk.Number, tk.Day, tk.Category, tk.Priority, tk.Route, tk.Status, tk.Source, truncateRunes(tk.Summary, 120))
		}
		e := c.emp(p)
		fmt.Fprintf(&b, "\n=== карточка: форма=%s риск=%s (%s) флаги=%v\n", deref(e.AddressForm), e.Risk, deref(e.RiskReason), e.Flags)
		for _, n := range p.notes {
			b.WriteString("заметка: " + n + "\n")
		}
		if err := os.WriteFile(filepath.Join(dir, p.name+".txt"), []byte(b.String()), 0o600); err != nil {
			t.Fatal(err)
		}
		sum = append(sum, fmt.Sprintf("%-26s сообщений %3d, кнопок %3d, вложений %3d, правок %3d, обращений %2d, события %v, риск %s",
			p.name, msgs, btns, atts, edits, len(tks), status, e.Risk))
	}
	t.Log("\n" + strings.Join(sum, "\n"))
	_ = os.WriteFile(filepath.Join(dir, "summary.txt"), []byte(strings.Join(sum, "\n")+"\n"), 0o600)
}
