package repositories

import (
	"context"
	"time"

	"main/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// DocumentRepo — база знаний: документы, их версии и байты файлов (domain/documents.go), плюс
// чтение отказов rag из bot_questions для «Вопросов без ответа».
type DocumentRepo struct {
	db *gorm.DB
}

// List — все документы, свежие первыми.
func (r *DocumentRepo) List(ctx context.Context) ([]domain.KBDocument, error) {
	var out []domain.KBDocument
	err := r.db.WithContext(ctx).Order("updated_at DESC").Find(&out).Error
	return out, err
}

func (r *DocumentRepo) ByID(ctx context.Context, id uuid.UUID) (*domain.KBDocument, error) {
	var d domain.KBDocument
	return ignoreNotFound(&d, r.db.WithContext(ctx).First(&d, "id = ?", id).Error)
}

// ByDocForUpdate — документ по имени под блокировкой: две одновременные загрузки одного имени
// не получат одинаковый номер версии.
func (r *DocumentRepo) ByDocForUpdate(ctx context.Context, doc string) (*domain.KBDocument, error) {
	var d domain.KBDocument
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&d, "doc = ?", doc).Error
	return ignoreNotFound(&d, err)
}

func (r *DocumentRepo) Save(ctx context.Context, d *domain.KBDocument) error {
	return r.db.WithContext(ctx).Save(d).Error
}

// UpdateIndex — состояние индекса документа (перезаливка): без перезаписи остальных полей и без
// updated_at — «Обновлён» в кабинете значит новую версию, а не перезаливку.
func (r *DocumentRepo) UpdateIndex(ctx context.Context, id uuid.UUID, status, ragDocID, indexErr string, chunks, clauses int, pages domain.IntList) error {
	return r.db.WithContext(ctx).Model(&domain.KBDocument{}).Where("id = ?", id).UpdateColumns(map[string]any{
		"status": status, "rag_doc_id": ragDocID, "index_error": indexErr,
		"chunks": chunks, "clauses": clauses, "low_confidence_pages": pages,
	}).Error
}

// AddVersion — версия и её файл одной записью.
func (r *DocumentRepo) AddVersion(ctx context.Context, v *domain.KBDocumentVersion, data []byte) error {
	if err := r.db.WithContext(ctx).Create(v).Error; err != nil {
		return err
	}
	return r.db.WithContext(ctx).Create(&domain.KBDocumentFile{ID: v.ID, Data: data}).Error
}

// Versions — история документа, новые первыми.
func (r *DocumentRepo) Versions(ctx context.Context, docID uuid.UUID) ([]domain.KBDocumentVersion, error) {
	var out []domain.KBDocumentVersion
	err := r.db.WithContext(ctx).Where("document_id = ?", docID).Order("version DESC").Find(&out).Error
	return out, err
}

// Version — версия документа по номеру.
func (r *DocumentRepo) Version(ctx context.Context, docID uuid.UUID, version int) (*domain.KBDocumentVersion, error) {
	var v domain.KBDocumentVersion
	return ignoreNotFound(&v, r.db.WithContext(ctx).First(&v, "document_id = ? AND version = ?", docID, version).Error)
}

// FileData — байты файла версии; nil — файла нет.
func (r *DocumentRepo) FileData(ctx context.Context, versionID uuid.UUID) ([]byte, error) {
	var f domain.KBDocumentFile
	got, err := ignoreNotFound(&f, r.db.WithContext(ctx).First(&f, "id = ?", versionID).Error)
	if err != nil || got == nil {
		return nil, err
	}
	return got.Data, nil
}

// TruncateKB — очистить таблицы базы знаний. Только для тестов.
func (r *DocumentRepo) TruncateKB(ctx context.Context) error {
	return r.db.WithContext(ctx).Exec(`TRUNCATE kb_documents, kb_document_versions, kb_document_files`).Error
}

// UnansweredRow — отказ rag на вопрос новичка: текст (только при согласии) и чей он — имя нужно,
// чтобы вычистить его из текста.
type UnansweredRow struct {
	ID            uuid.UUID
	EmployeeID    uuid.UUID
	Question      string
	AbstainReason string
	CreatedAt     time.Time
	IsTest        bool
	FullName      string
	Site          string
}

// UnansweredFilter — какие отказы читать.
type UnansweredFilter struct {
	Since       time.Time
	Site        string // пусто — все площадки
	IncludeTest bool
	Reasons     []string // abstain_reason, которые означают «нет в документах»
}

// Unanswered — отказы rag с текстом вопроса: kind=abstain, вопрос сохранён (а значит, новичок
// дал согласие) и согласие не отозвано, причина из f.Reasons. Старые первыми.
func (r *DocumentRepo) Unanswered(ctx context.Context, f UnansweredFilter) ([]UnansweredRow, error) {
	q := r.db.WithContext(ctx).Table("bot_questions AS q").
		Select("q.id, q.employee_id, q.question, q.abstain_reason, q.created_at, q.is_test, e.full_name, e.site").
		Joins("JOIN employees e ON e.id = q.employee_id").
		Where("q.kind = ? AND q.question <> '' AND q.created_at >= ?", domain.QuestionAbstain, f.Since).
		// Согласие проверяется и сейчас: отозвал — его вопросы из разбора уходят.
		Where("e.consent = ?", domain.ConsentGiven)
	if len(f.Reasons) > 0 {
		q = q.Where("q.abstain_reason IN ?", f.Reasons)
	}
	if f.Site != "" {
		q = q.Where("e.site = ?", f.Site)
	}
	if !f.IncludeTest {
		q = q.Where("NOT q.is_test")
	}
	var out []UnansweredRow
	err := q.Order("q.created_at ASC").Scan(&out).Error
	return out, err
}
