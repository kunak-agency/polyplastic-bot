package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"main/domain"
	"main/externalApi/rag"
	"main/pkg/errs"
	"main/repositories"

	"github.com/google/uuid"
)

// База знаний (итерация 4): загрузка через backend в rag, версии, перезаливка после рестарта
// rag, проверочный вопрос, «вопросы без ответа». rag — httptest-подделка с индексом в памяти,
// как настоящий; Postgres — TEST_DATABASE_URL.

// kbRAG — подделка rag: документы по имени (повторная загрузка заменяет), корпус из одного
// документа, /ask отдаёт заданный ответ.
type kbRAG struct {
	srv     *httptest.Server
	mu      sync.Mutex
	docs    map[string]rag.Document
	files   map[string][]byte
	uploads int
	reject  int    // статус отказа POST /documents; 0 — принимать
	ask     string // тело ответа /ask
	down    bool
}

func newKBRAG(t *testing.T) *kbRAG {
	f := &kbRAG{docs: map[string]rag.Document{}, files: map[string][]byte{}}
	f.docs["ПВТР-синтетика"] = rag.Document{DocID: "corpus01", Doc: "ПВТР-синтетика", Kind: "corpus", Source: rag.SourceCorpus, Chunks: 10, Clauses: 15, LowConfidencePages: []int{}, UploadedAt: "2026-10-23T20:36:48+00:00"}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *kbRAG) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/documents":
		items := []rag.Document{}
		for _, d := range f.docs {
			items = append(items, d)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items, "total": len(items)})
	case r.Method == http.MethodPost && r.URL.Path == "/documents":
		if f.reject != 0 {
			w.WriteHeader(f.reject)
			_, _ = w.Write([]byte(`{"detail":"в документе не найден текст"}`))
			return
		}
		_ = r.ParseMultipartForm(32 << 20)
		file, fh, _ := r.FormFile("file")
		data, _ := io.ReadAll(file)
		doc := r.FormValue("doc")
		sum := sha256.Sum256(append([]byte(doc), data...))
		id := hex.EncodeToString(sum[:])[:16]
		name := fh.Filename
		pages := []int{}
		if strings.Contains(string(data), "скан") {
			pages = []int{2}
		}
		f.docs[doc] = rag.Document{DocID: id, Doc: doc, Filename: &name, Kind: "pdf", Source: rag.SourceUpload, Chunks: 2, Clauses: 3, LowConfidencePages: pages}
		f.files[doc] = data
		f.uploads++
		_ = json.NewEncoder(w).Encode(map[string]any{"doc_id": id, "chunks": 2, "clauses": 3, "low_confidence_pages": pages})
	case r.Method == http.MethodPost && r.URL.Path == "/ask":
		_, _ = w.Write([]byte(f.ask))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// restart — rag перезапущен: загрузки пропали, корпус остался.
func (f *kbRAG) restart() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for k, d := range f.docs {
		if d.Source == rag.SourceUpload {
			delete(f.docs, k)
			delete(f.files, k)
		}
	}
}

func (f *kbRAG) set(fn func(*kbRAG)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *kbRAG) file(doc string) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.files[doc]
}

type kbEnv struct {
	repos *repositories.Repos
	rag   *kbRAG
	svc   *DocumentService
	now   time.Time
}

func newKBEnv(t *testing.T) *kbEnv {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL не задан")
	}
	repos, err := repositories.InitRepos(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := repos.MigrateDB(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := repos.Truncate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := repos.Documents.TruncateKB(ctx); err != nil {
		t.Fatal(err)
	}
	e := &kbEnv{repos: repos, rag: newKBRAG(t), now: msk(2026, 11, 2, 12, 0)}
	e.svc = newDocumentService(repos, rag.New(e.rag.srv.URL, time.Second), DocsOptions{MaxUploadMB: 1, UploadTimeout: time.Second}, func() time.Time { return e.now })
	return e
}

func (e *kbEnv) upload(t *testing.T, doc string, data string) *DocumentItem {
	t.Helper()
	it, err := e.svc.Upload(context.Background(), UploadInput{Doc: doc, Kind: "pdf", Filename: "memo.pdf", Sites: []string{"GTP"}, UploadedBy: "Анна (синтетика)", Data: []byte(data)})
	if err != nil {
		t.Fatal(err)
	}
	return it
}

func findDoc(l *DocumentList, doc string) *DocumentItem {
	for i := range l.Items {
		if l.Items[i].Doc == doc {
			return &l.Items[i]
		}
	}
	return nil
}

// Загрузка → v1 в базе и в rag; повтор того же имени → v2, история из двух версий; список
// показывает и корпус rag.
func TestDocumentUploadVersions(t *testing.T) {
	e := newKBEnv(t)
	ctx := context.Background()
	first := e.upload(t, "Памятка (синтетика)", "%PDF-1.4 версия 1")
	if first.Version != 1 || first.Status != domain.KBStatusReady || !first.Indexed || first.Chunks != 2 || first.By != "Анна (синтетика)" {
		t.Fatalf("v1 %+v", first)
	}
	second := e.upload(t, "Памятка (синтетика)", "%PDF-1.4 версия 2 скан")
	if second.Version != 2 || *second.ID != *first.ID || second.Status != domain.KBStatusNeedsReview || len(second.Pages) != 1 {
		t.Fatalf("v2 %+v", second)
	}
	l, err := e.svc.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !l.RAGUp || len(l.Items) != 2 {
		t.Fatalf("список %+v", l)
	}
	if c := findDoc(l, "ПВТР-синтетика"); c == nil || c.ID != nil || c.Source != rag.SourceCorpus || c.Clauses != 15 {
		t.Fatalf("корпус %+v", c)
	}
	if d := findDoc(l, "Памятка (синтетика)"); d == nil || d.Status != domain.KBStatusNeedsReview || !d.Indexed || d.Sites[0] != "GTP" {
		t.Fatalf("документ %+v", d)
	}
	doc, versions, err := e.svc.Versions(ctx, *first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Version != 2 || len(versions) != 2 || versions[0].Version != 2 || versions[1].Version != 1 || versions[1].Size != int64(len("%PDF-1.4 версия 1")) {
		t.Fatalf("версии %+v", versions)
	}
	data, err := e.repos.Documents.FileData(ctx, versions[1].ID)
	if err != nil || string(data) != "%PDF-1.4 версия 1" {
		t.Fatalf("файл v1 %q %v", data, err)
	}
	if _, _, err := e.svc.Versions(ctx, uuid.New()); !isType(err, errs.ErrTypeNotFound) {
		t.Fatalf("чужой id: %v", err)
	}
}

func isType(err error, typ errs.Type) bool {
	got, _ := appCode(err)
	return got == typ
}

// rag отказал по файлу → 422, в базе ничего; rag лежит → 503 (внешний сбой), в базе ничего.
func TestDocumentUploadRejected(t *testing.T) {
	e := newKBEnv(t)
	ctx := context.Background()
	e.rag.set(func(f *kbRAG) { f.reject = http.StatusUnprocessableEntity })
	_, err := e.svc.Upload(ctx, UploadInput{Doc: "Скан", Kind: "pdf", Filename: "scan.pdf", Data: []byte("%PDF-")})
	if !isType(err, errs.ErrTypeValidation) {
		t.Fatalf("отказ rag: %v", err)
	}
	e.rag.set(func(f *kbRAG) { f.reject, f.down = 0, true })
	_, err = e.svc.Upload(ctx, UploadInput{Doc: "Скан", Kind: "pdf", Filename: "scan.pdf", Data: []byte("%PDF-")})
	if x := errs.FromExternal(err); x == nil || x.HTTPStatus() != http.StatusBadGateway {
		t.Fatalf("rag 503 по HTTP — ответ не по договорённости, ждём 502: %v", err)
	}
	e.svc.rag = rag.New("http://127.0.0.1:1", time.Second) // никто не слушает
	_, err = e.svc.Upload(ctx, UploadInput{Doc: "Скан", Kind: "pdf", Filename: "scan.pdf", Data: []byte("%PDF-")})
	if x := errs.FromExternal(err); x == nil || x.HTTPStatus() != http.StatusServiceUnavailable {
		t.Fatalf("rag не слушает — 503: %v", err)
	}
	stored, _ := e.repos.Documents.List(ctx)
	if len(stored) != 0 {
		t.Fatalf("после отказов в базе %d документов", len(stored))
	}
}

// rag перезапустился и забыл загрузки: список показывает not_indexed, Resync перезаливает
// сохранённые байты текущей версии, статус снова ready.
func TestDocumentResync(t *testing.T) {
	e := newKBEnv(t)
	ctx := context.Background()
	e.upload(t, "Памятка (синтетика)", "%PDF-1.4 версия 1")
	e.upload(t, "Памятка (синтетика)", "%PDF-1.4 версия 2")
	e.upload(t, "Охрана труда (синтетика)", "%PDF-1.4 охрана")
	stored, _ := e.repos.Documents.List(ctx)
	updated := map[string]time.Time{}
	for _, d := range stored {
		updated[d.Doc] = d.UpdatedAt
	}
	e.rag.restart()

	e.svc.resync.Lock() // фоновая перезаливка из List подождёт: проверяем статус до неё
	l, err := e.svc.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if d := findDoc(l, "Памятка (синтетика)"); d == nil || d.Status != domain.KBStatusNotIndexed || d.Indexed {
		t.Fatalf("после рестарта rag %+v", d)
	}
	e.svc.resync.Unlock()

	rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	e.svc.Resync(rctx)
	if got := string(e.rag.file("Памятка (синтетика)")); got != "%PDF-1.4 версия 2" {
		t.Fatalf("перезалита не текущая версия: %q", got)
	}
	if e.rag.file("Охрана труда (синтетика)") == nil {
		t.Fatal("второй документ не перезалит")
	}
	l, err = e.svc.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range l.Items {
		if it.Status != domain.KBStatusReady || !it.Indexed {
			t.Fatalf("после перезаливки %+v", it)
		}
		if it.ID != nil && !it.Updated.Equal(updated[it.Doc]) {
			t.Fatalf("перезаливка сдвинула «Обновлён»: %v → %v", updated[it.Doc], it.Updated)
		}
	}
	// Всё на месте — повторный проход ничего не шлёт.
	before := e.rag.uploads
	e.svc.resyncOnce(ctx, true)
	if e.rag.uploads != before {
		t.Fatal("лишняя перезаливка")
	}
}

// Проверочный вопрос: ответ с цитатами; from_doc — есть ли цитата из выбранного документа.
// В bot_questions не пишется.
func TestDocumentAsk(t *testing.T) {
	e := newKBEnv(t)
	ctx := context.Background()
	e.rag.set(func(f *kbRAG) { f.ask = ragAnswer })
	doc := "ПВТР"
	r, err := e.svc.Ask(ctx, "когда можно в отпуск", &doc, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Answer == nil || len(r.Sources) != 2 || r.Sources[0].ClauseID != "6.7.8" || r.FromDoc == nil || !*r.FromDoc {
		t.Fatalf("ответ %+v", r)
	}
	other := "Охрана труда"
	if r, err = e.svc.Ask(ctx, "когда можно в отпуск", &other, nil); err != nil || *r.FromDoc {
		t.Fatalf("цитаты не из этого документа: %+v %v", r, err)
	}
	e.rag.set(func(f *kbRAG) { f.ask = ragAbstain })
	if r, err = e.svc.Ask(ctx, "есть ли ДМС", nil, nil); err != nil || !r.NeedsEscalation || r.FromDoc != nil || r.Sources == nil {
		t.Fatalf("отказ: %+v %v", r, err)
	}
	e.rag.set(func(f *kbRAG) {
		f.ask = `{"answer":"без источника","sources":[],"confidence":"high","needs_escalation":false,"topic":"hr","abstain_reason":null}`
	})
	if _, err = e.svc.Ask(ctx, "есть ли ДМС", nil, nil); errs.FromExternal(err) == nil {
		t.Fatalf("ответ без источника — сбой rag: %v", err)
	}
	var n int64
	if counts, err := e.repos.DemoCounts(ctx); err != nil || counts["bot_questions"] != n {
		t.Fatalf("проверка HR попала в bot_questions: %v %v", counts, err)
	}
	if !sameDoc("Штатка (синтетика) / Лист1", "Штатка (синтетика)") || sameDoc("ПВТР-2", "ПВТР") {
		t.Fatal("sameDoc")
	}
}

// Вопросы без ответа: только отказы no_source/below_threshold, только при согласии (сейчас),
// без тестовых; похожие — в одну группу; тексты обезличены.
func TestUnanswered(t *testing.T) {
	e := newKBEnv(t)
	ctx := context.Background()
	given, declined := domain.ConsentGiven, domain.ConsentDeclined
	mk := func(name, site string, consent *string, test bool) uuid.UUID {
		emp := &domain.Employee{FullName: name, Site: site, Consent: consent, IsTest: test, StartDate: e.now}
		if err := e.repos.Employees.Create(ctx, emp); err != nil {
			t.Fatal(err)
		}
		return emp.ID
	}
	petrov := mk("Петров Иван (синтетика)", "GTP", &given, false)
	sidorova := mk("Сидорова Анна (синтетика)", "GrPP", &given, false)
	revoked := mk("Козлов Олег (синтетика)", "GTP", &declined, false)
	tester := mk("Тестов Тест (синтетика)", "GTP", &given, true)
	at := e.now.Add(-48 * time.Hour)
	ask := func(emp uuid.UUID, text, kind, reason string, test bool) {
		at = at.Add(time.Minute)
		q := &domain.BotQuestion{EmployeeID: emp, Question: text, Kind: kind, AbstainReason: reason, IsTest: test, Flags: domain.StringList{}, Sources: domain.SourceList{}}
		q.CreatedAt = at
		if err := e.repos.Questions.Create(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	ask(petrov, "Можно оформить пропуск для родственника?", domain.QuestionAbstain, rag.AbstainNoSource, false)
	ask(sidorova, "Как сделать пропуск родственнику на завод", domain.QuestionAbstain, rag.AbstainBelowThreshold, false)
	ask(petrov, "Я Петров, мой телефон +7 999 123-45-67, пропуск для родственника нужен", domain.QuestionAbstain, rag.AbstainNoSource, false)
	ask(sidorova, "Есть ли ДМС?", domain.QuestionAbstain, rag.AbstainNoSource, false)
	ask(petrov, "а дмс есть", domain.QuestionAbstain, rag.AbstainNoSource, false)
	ask(petrov, "Сколько платят Смирновой?", domain.QuestionAbstain, rag.AbstainRoutedTopic, false) // намеренная передача
	ask(petrov, "Когда отпуск", domain.QuestionAnswer, "", false)                                   // ответ есть
	ask(sidorova, "", domain.QuestionAbstain, rag.AbstainNoSource, false)                           // без согласия текста нет
	ask(revoked, "пропуск для родственника", domain.QuestionAbstain, rag.AbstainNoSource, false)    // согласие отозвано
	ask(tester, "пропуск для родственника", domain.QuestionAbstain, rag.AbstainNoSource, true)      // тестовый

	r, err := e.svc.Unanswered(ctx, UnansweredFilter{Days: 30})
	if err != nil {
		t.Fatal(err)
	}
	if r.Questions != 5 || len(r.Clusters) != 2 {
		t.Fatalf("вопросов %d, групп %d: %+v", r.Questions, len(r.Clusters), r.Clusters)
	}
	pass := r.Clusters[0]
	if pass.Count != 3 || pass.Newcomers != 2 || pass.Reasons[rag.AbstainNoSource] != 2 || pass.Reasons[rag.AbstainBelowThreshold] != 1 {
		t.Fatalf("группа пропуска %+v", pass)
	}
	if !strings.Contains(strings.Join(pass.Keywords, " "), "пропуск") {
		t.Fatalf("ключевые слова %v", pass.Keywords)
	}
	all := pass.Title + " | " + strings.Join(pass.Examples, " | ")
	if strings.Contains(all, "Петров") || strings.Contains(all, "999") || !strings.Contains(all, "[имя]") || !strings.Contains(all, "[телефон]") {
		t.Fatalf("не обезличено: %s", all)
	}
	if dms := r.Clusters[1]; dms.Count != 2 || dms.Keywords[0] != "дмс" {
		t.Fatalf("группа ДМС %+v", dms)
	}

	r, _ = e.svc.Unanswered(ctx, UnansweredFilter{Days: 30, Site: "GrPP"})
	if r.Questions != 2 {
		t.Fatalf("по площадке GrPP %d", r.Questions)
	}
	r, _ = e.svc.Unanswered(ctx, UnansweredFilter{Days: 30, IncludeTest: true})
	if r.Questions != 6 || r.Clusters[0].Count != 4 {
		t.Fatalf("с тестовыми %d %+v", r.Questions, r.Clusters)
	}
	r, _ = e.svc.Unanswered(ctx, UnansweredFilter{Days: 1})
	if r.Questions != 0 || r.Clusters == nil {
		t.Fatalf("за сутки %+v", r)
	}
}

// Обезличивание без базы.
func TestAnonymize(t *testing.T) {
	cases := []struct{ in, name, want string }{
		{"Пропуск для мамы", "Петров Иван", "Пропуск для мамы"},
		{"Иван Петров спрашивает: где столовая?", "Петров Иван (синтетика)", "[имя] [имя] спрашивает: где столовая?"},
		{"Можно ли пропуск для Марии Ивановой", "", "Можно ли пропуск для [имя] [имя]"},
		{"пишите на ivan@example.com или 89991234567", "", "пишите на [почта] или [телефон]"},
		{"табельный 123456, ДМС есть? Работаю в Лужники", "", "табельный [номер], ДМС есть? Работаю в Лужники"},
		{"Excel на Wi-Fi не работает", "", "Excel на Wi-Fi не работает"},
		{"у Петровой спросить?", "Петрова Анна", "у [имя] спросить?"},
	}
	for _, c := range cases {
		if got := anonymize(c.in, c.name); got != c.want {
			t.Errorf("anonymize(%q) = %q, ждём %q", c.in, got, c.want)
		}
	}
}
