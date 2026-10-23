package services

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"sync"
	"time"

	"main/domain"
	"main/externalApi/rag"
	"main/pkg/errs"
	"main/pkg/logger"
	"main/repositories"

	"github.com/google/uuid"
)

// База знаний (итерация 4, api_contract.md «v3 — база знаний»): прокси загрузки документов HR в
// rag, метаданные и версии в своей таблице, перезаливка в rag того, что он потерял при рестарте,
// проверочный вопрос и «вопросы без ответа» (documents_unanswered.go).

// DocsRAG — что базе знаний нужно от rag. Интерфейс — ради тестов на httptest-подделке и nil.
type DocsRAG interface {
	UploadDocument(ctx context.Context, doc, filename string, data []byte) (*rag.Uploaded, error)
	ListDocuments(ctx context.Context) ([]rag.Document, error)
	Ask(ctx context.Context, req rag.AskRequest) (*rag.AskResponse, error)
}

// DocsOptions — настройки базы знаний (config.Docs).
type DocsOptions struct {
	MaxUploadMB   int
	UploadTimeout time.Duration
	OperatorToken string
}

// DocumentService — база знаний.
type DocumentService struct {
	repos         *repositories.Repos
	rag           DocsRAG // nil — rag не подключён: загрузка и проверка отвечают 503
	maxBytes      int64
	uploadTimeout time.Duration
	operatorToken string
	now           func() time.Time

	// resync — одна перезаливка за раз: список кабинета может звать её на каждом открытии.
	resync sync.Mutex
}

// listTimeout — сколько список кабинета ждёт GET /documents rag: без ответа показываем то, что в базе.
const listTimeout = 3 * time.Second

func newDocumentService(r *repositories.Repos, c DocsRAG, o DocsOptions, now func() time.Time) *DocumentService {
	if o.MaxUploadMB <= 0 {
		o.MaxUploadMB = 20
	}
	if o.UploadTimeout <= 0 {
		o.UploadTimeout = 3 * time.Minute
	}
	return &DocumentService{repos: r, rag: c, maxBytes: int64(o.MaxUploadMB) << 20, uploadTimeout: o.UploadTimeout,
		operatorToken: o.OperatorToken, now: now}
}

// MaxUploadBytes — потолок файла загрузки.
func (s *DocumentService) MaxUploadBytes() int64 { return s.maxBytes }

// OperatorAllowed — заголовок X-Operator-Token совпал с KB_OPERATOR_TOKEN. Пустой токен в
// конфиге не открывает никого.
func (s *DocumentService) OperatorAllowed(token string) bool {
	return s.operatorToken != "" && subtle.ConstantTimeCompare([]byte(token), []byte(s.operatorToken)) == 1
}

// DocumentItem — строка таблицы документов: загруженный HR (ID не nil) или корпусный rag.
type DocumentItem struct {
	ID       *uuid.UUID
	RAGDocID string
	Doc      string
	Filename string
	Kind     string
	Source   string // upload | corpus
	Sites    []string
	Version  int
	Status   string
	Indexed  bool // есть в индексе rag прямо сейчас (при rag down — false)
	Chunks   int
	Clauses  int
	Pages    []int
	Size     int64
	By       string
	Updated  time.Time
	Error    string
}

// DocumentList — список и состояние rag на момент ответа.
type DocumentList struct {
	Items []DocumentItem
	RAGUp bool
}

// List — документы из базы, сверенные с индексом rag, и корпусные документы rag. rag не ответил —
// только база, статусы как сохранены. Загруженные, которых в rag нет, — not_indexed, и в фоне
// запускается перезаливка.
func (s *DocumentService) List(ctx context.Context) (*DocumentList, error) {
	var stored []domain.KBDocument
	if s.repos.Enabled() {
		var err error
		if stored, err = s.repos.Documents.List(ctx); err != nil {
			return nil, err
		}
	}
	var live []rag.Document
	ragUp := false
	if s.rag != nil {
		lctx, cancel := context.WithTimeout(ctx, listTimeout)
		var err error
		live, err = s.rag.ListDocuments(lctx)
		cancel()
		if err != nil {
			logger.Warn("база знаний: GET /documents rag: %v", err)
		} else {
			ragUp = true
		}
	}
	inRAG := map[string]rag.Document{}
	for _, d := range live {
		inRAG[d.DocID] = d
	}
	out := &DocumentList{RAGUp: ragUp, Items: make([]DocumentItem, 0, len(stored)+len(live))}
	names := map[string]bool{}
	missing := false
	for _, d := range stored {
		it := itemFromStored(d)
		if ragUp {
			_, it.Indexed = inRAG[d.RAGDocID]
			switch {
			case !it.Indexed:
				it.Status, missing = domain.KBStatusNotIndexed, true
			case len(d.LowConfidencePages) > 0:
				it.Status = domain.KBStatusNeedsReview
			default:
				it.Status = domain.KBStatusReady
			}
		}
		names[d.Doc] = true
		out.Items = append(out.Items, it)
	}
	for _, d := range live {
		// Корпус rag (синтетика из RAG_CORPUS_DIR) и загрузки мимо backend: показываем, но не
		// управляем ими. Загрузка с тем же именем заменит корпусный документ в rag.
		if names[d.Doc] {
			continue
		}
		out.Items = append(out.Items, itemFromRAG(d))
	}
	if missing && s.repos.Enabled() {
		go s.resyncOnce(context.WithoutCancel(ctx), false)
	}
	return out, nil
}

func itemFromStored(d domain.KBDocument) DocumentItem {
	id := d.ID
	return DocumentItem{
		ID: &id, RAGDocID: d.RAGDocID, Doc: d.Doc, Filename: d.Filename, Kind: d.Kind, Source: rag.SourceUpload,
		Sites: nonNil(d.Sites), Version: d.Version, Status: d.Status, Chunks: d.Chunks, Clauses: d.Clauses,
		Pages: nonNilInts(d.LowConfidencePages), Size: d.Size, By: d.UploadedBy, Updated: d.UpdatedAt, Error: d.IndexError,
	}
}

func itemFromRAG(d rag.Document) DocumentItem {
	it := DocumentItem{
		RAGDocID: d.DocID, Doc: d.Doc, Kind: d.Kind, Source: d.Source, Sites: []string{}, Version: 1,
		Status: domain.KBStatusReady, Indexed: true, Chunks: d.Chunks, Clauses: d.Clauses, Pages: nonNilInts(d.LowConfidencePages),
	}
	if d.Filename != nil {
		it.Filename = *d.Filename
	}
	if len(it.Pages) > 0 {
		it.Status = domain.KBStatusNeedsReview
	}
	if t, err := time.Parse(time.RFC3339, d.UploadedAt); err == nil {
		it.Updated = t
	}
	return it
}

func nonNil(l []string) []string {
	if l == nil {
		return []string{}
	}
	return l
}

func nonNilInts(l []int) []int {
	if l == nil {
		return []int{}
	}
	return l
}

// UploadInput — проверенная загрузка (in.DocumentUploadValid) и байты файла.
type UploadInput struct {
	Doc        string
	Kind       string
	Filename   string
	Sites      []string
	UploadedBy string
	Data       []byte
}

// Тексты отказов rag по файлу: свои, тело rag наружу не идёт.
const (
	msgRAGFormat  = "Формат файла не поддерживается"
	msgRAGNoText  = "В файле не найден текст: он пустой, повреждён или это скан, который не распознался"
	msgRAGTooBig  = "Файл больше, чем принимает сервис поиска"
	msgRAGOffline = "Сервис поиска по документам сейчас недоступен — файл не загружен. Повторите позже"
)

// Upload — файл в rag, затем новая версия документа в базе. Порядок такой: если rag отказал,
// в базе ничего не меняется; если упала база после rag, в индексе новая версия, а в кабинете
// старая — следующая загрузка того же файла это выправит (rag заменяет по имени).
func (s *DocumentService) Upload(ctx context.Context, u UploadInput) (*DocumentItem, error) {
	if !s.repos.Enabled() {
		return nil, errs.NewInternalError("База знаний без базы данных не сохраняет документы").WithCause(repositories.ErrNoDatabase)
	}
	if s.rag == nil {
		return nil, ragUnavailable(errors.New("rag: клиент не подключён"))
	}
	uctx, cancel := context.WithTimeout(ctx, s.uploadTimeout)
	res, err := s.rag.UploadDocument(uctx, u.Doc, u.Filename, u.Data)
	cancel()
	if err != nil {
		return nil, uploadError(err)
	}
	sum := sha256.Sum256(u.Data)
	var saved domain.KBDocument
	err = s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		d, err := tx.Documents.ByDocForUpdate(ctx, u.Doc)
		if err != nil {
			return err
		}
		if d == nil {
			d = &domain.KBDocument{Doc: u.Doc}
		}
		d.Version++
		d.Filename, d.Kind, d.Sites, d.Size, d.SHA256 = u.Filename, u.Kind, u.Sites, int64(len(u.Data)), hex.EncodeToString(sum[:])
		d.RAGDocID, d.Chunks, d.Clauses, d.LowConfidencePages = res.DocID, res.Chunks, res.Clauses, res.LowConfidencePages
		d.UploadedBy, d.IndexError, d.Status = u.UploadedBy, "", statusOf(res.LowConfidencePages)
		d.UpdatedAt = s.now()
		if err := tx.Documents.Save(ctx, d); err != nil {
			return err
		}
		v := &domain.KBDocumentVersion{
			DocumentID: d.ID, Version: d.Version, Filename: d.Filename, Kind: d.Kind, Size: d.Size, SHA256: d.SHA256,
			RAGDocID: d.RAGDocID, Chunks: d.Chunks, Clauses: d.Clauses, LowConfidencePages: d.LowConfidencePages, UploadedBy: d.UploadedBy,
		}
		v.CreatedAt = d.UpdatedAt
		if err := tx.Documents.AddVersion(ctx, v, u.Data); err != nil {
			return err
		}
		saved = *d
		return nil
	})
	if repositories.IsUniqueViolation(err) {
		// Две первые загрузки одного имени одновременно: вторая проиграла гонку за имя.
		return nil, errs.NewConflictError("Документ с таким названием только что загрузили. Обновите список и загрузите ещё раз").WithCause(err)
	}
	if err != nil {
		return nil, err
	}
	logger.Info("база знаний: документ %s v%d в индексе rag (%s): чанков %d, пунктов %d, страниц на вычитку %d",
		saved.ID, saved.Version, saved.RAGDocID, saved.Chunks, saved.Clauses, len(saved.LowConfidencePages))
	it := itemFromStored(saved)
	it.Indexed = true
	return &it, nil
}

func statusOf(pages []int) string {
	if len(pages) > 0 {
		return domain.KBStatusNeedsReview
	}
	return domain.KBStatusReady
}

// ragStatusErr — сбой rag как внешний: 503 (не отвечает) или 504 (не дождались).
type ragStatusErr struct {
	status int
	err    error
}

func (e *ragStatusErr) Error() string       { return e.err.Error() }
func (e *ragStatusErr) Unwrap() error       { return e.err }
func (e *ragStatusErr) ExternalStatus() int { return e.status }

func ragUnavailable(err error) error {
	return errs.NewInternalError(msgRAGOffline).WithCause(&ragStatusErr{status: http.StatusServiceUnavailable, err: err})
}

// uploadError — отказ rag по файлу → 422 с нашим текстом; срок → 504; ответ не 2xx → 502;
// сеть → 503.
func uploadError(err error) error {
	var rej *rag.RejectedError
	if errors.As(err, &rej) {
		msg := msgRAGNoText
		switch rej.Status {
		case http.StatusUnsupportedMediaType:
			msg = msgRAGFormat
		case http.StatusRequestEntityTooLarge:
			msg = msgRAGTooBig
		}
		return errs.NewValidationError(msg).
			WithDetails(errs.FieldError{Field: "file", Code: errs.CodeInvalidFormat, Message: msg}).WithCause(err)
	}
	return ragFailure(err)
}

// ragFailure — сбой вызова rag как внешний сбой нужного класса.
func ragFailure(err error) error {
	var re *rag.Error
	switch {
	case errors.As(err, &re):
		return errs.NewInternalError("rag").WithCause(err) // ErrorHandler отдаст 502
	case errors.Is(err, context.DeadlineExceeded):
		return errs.NewInternalError("rag").WithCause(&ragStatusErr{status: http.StatusGatewayTimeout, err: err})
	}
	return ragUnavailable(err)
}

// Versions — история документа, новые первыми. nil, nil — документа нет.
func (s *DocumentService) Versions(ctx context.Context, id uuid.UUID) (*domain.KBDocument, []domain.KBDocumentVersion, error) {
	if !s.repos.Enabled() {
		return nil, nil, errs.NewNotFoundError("Документ не найден")
	}
	d, err := s.repos.Documents.ByID(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if d == nil {
		return nil, nil, errs.NewNotFoundError("Документ не найден")
	}
	list, err := s.repos.Documents.Versions(ctx, id)
	return d, list, err
}

// AskResult — ответ rag на проверочный вопрос HR и попали ли цитаты в выбранный документ.
type AskResult struct {
	*rag.AskResponse
	FromDoc *bool // nil — документ не выбран
}

// Ask — проверочный вопрос HR через тот же конвейер rag, что у новичка (обращение на «вы»). В
// bot_questions не пишется: это не вопрос новичка и не должен попадать в «вопросы без ответа».
// rag пока не умеет искать в одном документе — ищем по всей базе и говорим, откуда цитаты.
func (s *DocumentService) Ask(ctx context.Context, question string, doc, site *string) (*AskResult, error) {
	if s.rag == nil {
		return nil, ragUnavailable(errors.New("rag: клиент не подключён"))
	}
	vy := domain.AddressVy
	trace := newTraceID()
	res, err := s.rag.Ask(rag.WithTraceID(ctx, trace), rag.AskRequest{Question: question, Site: site, AddressForm: &vy, TraceID: trace})
	if errors.Is(err, rag.ErrBadAnswer) {
		return nil, errs.NewInternalError("rag").WithCause(&rag.Error{Status: http.StatusBadGateway, Op: "POST /ask"})
	}
	if err != nil {
		return nil, ragFailure(err)
	}
	if res.Sources == nil {
		res.Sources = []rag.Source{}
	}
	out := &AskResult{AskResponse: res}
	if doc != nil {
		hit := false
		for _, src := range res.Sources {
			if sameDoc(src.Doc, *doc) {
				hit = true
				break
			}
		}
		out.FromDoc = &hit
	}
	return out, nil
}

// sameDoc — цитата из документа doc. У XLSX с несколькими листами rag пишет «имя / лист».
func sameDoc(source, doc string) bool {
	return source == doc || (len(source) > len(doc)+3 && source[:len(doc)+3] == doc+" / ")
}

// Resync — при старте backend: дождаться rag и перезалить в него сохранённые документы, которых
// там нет (rag держит загрузки в памяти и после рестарта помнит только корпус). Ждёт rag до
// ctx.Done с паузами до 30 с: с моделями rag стартует десятки секунд.
func (s *DocumentService) Resync(ctx context.Context) {
	if s.rag == nil || !s.repos.Enabled() {
		return
	}
	wait := 2 * time.Second
	for {
		lctx, cancel := context.WithTimeout(ctx, listTimeout)
		_, err := s.rag.ListDocuments(lctx)
		cancel()
		if err == nil {
			s.resyncOnce(ctx, true)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if wait < 30*time.Second {
			wait *= 2
		}
	}
}

// resyncOnce — один проход перезаливки. wait=false (фон из списка): занято другим проходом —
// выходим, он сделает то же. wait=true (старт): ждём и проверяем ещё раз.
func (s *DocumentService) resyncOnce(ctx context.Context, wait bool) {
	if wait {
		s.resync.Lock()
	} else if !s.resync.TryLock() {
		return
	}
	defer s.resync.Unlock()
	lctx, cancel := context.WithTimeout(ctx, listTimeout)
	live, err := s.rag.ListDocuments(lctx)
	cancel()
	if err != nil {
		logger.Warn("база знаний: перезаливка отложена, rag не ответил: %v", err)
		return
	}
	inRAG := map[string]bool{}
	for _, d := range live {
		inRAG[d.DocID] = true
	}
	stored, err := s.repos.Documents.List(ctx)
	if err != nil {
		logger.Warn("база знаний: перезаливка: %v", err)
		return
	}
	restored := 0
	for _, d := range stored {
		if d.RAGDocID != "" && inRAG[d.RAGDocID] {
			continue
		}
		if err := s.reindex(ctx, d); err != nil {
			logger.Warn("база знаний: документ %s v%d не перезалит в rag: %v", d.ID, d.Version, err)
			continue
		}
		restored++
	}
	if restored > 0 {
		logger.Info("база знаний: перезалито в rag документов: %d", restored)
	}
}

// reindex — текущая версия документа заново в rag; итог — в статус документа.
func (s *DocumentService) reindex(ctx context.Context, d domain.KBDocument) error {
	v, err := s.repos.Documents.Version(ctx, d.ID, d.Version)
	if err != nil {
		return err
	}
	var data []byte
	if v != nil {
		if data, err = s.repos.Documents.FileData(ctx, v.ID); err != nil {
			return err
		}
	}
	if len(data) == 0 {
		_ = s.repos.Documents.UpdateIndex(ctx, d.ID, domain.KBStatusNotIndexed, d.RAGDocID, "файл версии не найден", d.Chunks, d.Clauses, d.LowConfidencePages)
		return errors.New("файл версии не найден")
	}
	uctx, cancel := context.WithTimeout(ctx, s.uploadTimeout)
	res, err := s.rag.UploadDocument(uctx, d.Doc, d.Filename, data)
	cancel()
	if err != nil {
		_ = s.repos.Documents.UpdateIndex(ctx, d.ID, domain.KBStatusNotIndexed, d.RAGDocID, "rag не принял файл при перезаливке", d.Chunks, d.Clauses, d.LowConfidencePages)
		return err
	}
	return s.repos.Documents.UpdateIndex(ctx, d.ID, statusOf(res.LowConfidencePages), res.DocID, "", res.Chunks, res.Clauses, res.LowConfidencePages)
}
