package domain

import (
	"database/sql/driver"
	"time"

	"github.com/google/uuid"
)

// База знаний (итерация 4, api_contract.md «v3 — база знаний»). rag-сервис держит загруженные
// документы в памяти процесса и после рестарта их теряет, поэтому метаданные и сами байты
// файлов живут здесь: при старте backend перезаливает в rag то, чего там нет.
//
// Байты — в Postgres (KBDocumentFile, bytea), а не в каталоге data/uploads/: документов десятки,
// каждый ≤ 20 МБ; метаданные и файл пишутся одной транзакцией (нет «записи без файла»);
// бэкап базы уносит и файлы; контейнеру API не нужен отдельный том на запись.

// Статусы документа.
const (
	KBStatusReady       = "ready"        // в индексе rag, вычитывать нечего
	KBStatusNeedsReview = "needs_review" // в индексе, но есть страницы на вычитку (low_confidence_pages)
	KBStatusNotIndexed  = "not_indexed"  // файл сохранён, но в rag его сейчас нет (rag лежит или отказал при перезаливке)
)

// KBDocument — документ базы знаний: текущая версия. Имя Doc уникально — это `doc` rag-сервиса
// (sources[].doc в ответах); загрузка с тем же именем — новая версия.
type KBDocument struct {
	UUIDModel
	UpdatedAt time.Time
	Doc       string `gorm:"uniqueIndex"`
	Filename  string
	Kind      string     // pdf | pptx | xlsx | text
	Sites     StringList `gorm:"type:jsonb"` // пусто — все площадки
	Version   int
	Status    string
	Size      int64
	SHA256    string
	RAGDocID  string
	Chunks    int
	Clauses   int
	// LowConfidencePages — страницы (для PPTX — слайды), которые нужно вычитать.
	LowConfidencePages IntList `gorm:"type:jsonb"`
	UploadedBy         string
	// IndexError — почему документа нет в rag (последняя попытка перезаливки); пусто — нет ошибки.
	IndexError string
}

func (KBDocument) TableName() string { return "kb_documents" }

// KBDocumentVersion — одна загрузка документа (вкладка «Версии»). Файл версии — KBDocumentFile
// с тем же ID: список версий байты не тянет.
type KBDocumentVersion struct {
	UUIDModel
	DocumentID         uuid.UUID `gorm:"type:uuid;uniqueIndex:kb_doc_version"`
	Version            int       `gorm:"uniqueIndex:kb_doc_version"`
	Filename           string
	Kind               string
	Size               int64
	SHA256             string
	RAGDocID           string
	Chunks             int
	Clauses            int
	LowConfidencePages IntList `gorm:"type:jsonb"`
	UploadedBy         string
}

func (KBDocumentVersion) TableName() string { return "kb_document_versions" }

// KBDocumentFile — байты файла версии. ID = KBDocumentVersion.ID.
type KBDocumentFile struct {
	ID   uuid.UUID `gorm:"type:uuid;primaryKey"`
	Data []byte    `gorm:"type:bytea"`
}

func (KBDocumentFile) TableName() string { return "kb_document_files" }

// IntList — jsonb-массив целых (номера страниц).
type IntList []int

func (l IntList) Value() (driver.Value, error) {
	if l == nil {
		l = IntList{}
	}
	return jsonValue([]int(l))
}

func (l *IntList) Scan(src any) error { return jsonScan(src, (*[]int)(l)) }
