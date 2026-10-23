package out

import (
	"time"

	"main/domain"
	"main/externalApi/rag"
	"main/services"
)

// База знаний (api_contract.md, «v3 — база знаний»).

// Document — строка таблицы документов.
type Document struct {
	// ID — UUID загруженного документа; у корпусного документа rag — "corpus:<doc_id>".
	ID                 string   `json:"id"`
	RAGDocID           string   `json:"rag_doc_id"`
	Doc                string   `json:"doc"`
	Filename           *string  `json:"filename"`
	Kind               string   `json:"kind"`
	Source             string   `json:"source"`
	Sites              []string `json:"sites"`
	Version            int      `json:"version"`
	Status             string   `json:"status"`
	Indexed            bool     `json:"indexed"`
	Chunks             int      `json:"chunks"`
	Clauses            int      `json:"clauses"`
	LowConfidencePages []int    `json:"low_confidence_pages"`
	Size               *int64   `json:"size"`
	UploadedBy         *string  `json:"uploaded_by"`
	UpdatedAt          *string  `json:"updated_at"`
	IndexError         *string  `json:"index_error"`
}

// DocumentList — GET /api/documents.
type DocumentList struct {
	Items []Document `json:"items"`
	Total int        `json:"total"`
	RAG   string     `json:"rag"`
}

func strOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func timeOrNil(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	s := t.Format(time.RFC3339)
	return &s
}

// DocumentFrom — строка из services.DocumentItem.
func DocumentFrom(d services.DocumentItem) Document {
	out := Document{
		RAGDocID: d.RAGDocID, Doc: d.Doc, Filename: strOrNil(d.Filename), Kind: d.Kind, Source: d.Source, Sites: d.Sites,
		Version: d.Version, Status: d.Status, Indexed: d.Indexed, Chunks: d.Chunks, Clauses: d.Clauses,
		LowConfidencePages: d.Pages, UploadedBy: strOrNil(d.By), UpdatedAt: timeOrNil(d.Updated), IndexError: strOrNil(d.Error),
	}
	if d.ID != nil {
		out.ID = d.ID.String()
		size := d.Size
		out.Size = &size
	} else {
		out.ID = "corpus:" + d.RAGDocID
	}
	return out
}

// DocumentListFrom — список с состоянием rag.
func DocumentListFrom(l services.DocumentList) DocumentList {
	items := make([]Document, 0, len(l.Items))
	for _, d := range l.Items {
		items = append(items, DocumentFrom(d))
	}
	return DocumentList{Items: items, Total: len(items), RAG: OkDown(l.RAGUp)}
}

// DocumentVersion — элемент вкладки «Версии».
type DocumentVersion struct {
	Version            int     `json:"version"`
	Current            bool    `json:"current"`
	Filename           string  `json:"filename"`
	Kind               string  `json:"kind"`
	Size               int64   `json:"size"`
	SHA256             string  `json:"sha256"`
	Chunks             int     `json:"chunks"`
	Clauses            int     `json:"clauses"`
	LowConfidencePages []int   `json:"low_confidence_pages"`
	UploadedBy         *string `json:"uploaded_by"`
	UploadedAt         string  `json:"uploaded_at"`
}

// DocumentVersions — GET /api/documents/{id}/versions.
type DocumentVersions struct {
	Items []DocumentVersion `json:"items"`
	Total int               `json:"total"`
}

func DocumentVersionsFrom(d domain.KBDocument, list []domain.KBDocumentVersion) DocumentVersions {
	items := make([]DocumentVersion, 0, len(list))
	for _, v := range list {
		pages := []int(v.LowConfidencePages)
		if pages == nil {
			pages = []int{}
		}
		items = append(items, DocumentVersion{
			Version: v.Version, Current: v.Version == d.Version, Filename: v.Filename, Kind: v.Kind, Size: v.Size, SHA256: v.SHA256,
			Chunks: v.Chunks, Clauses: v.Clauses, LowConfidencePages: pages, UploadedBy: strOrNil(v.UploadedBy),
			UploadedAt: v.CreatedAt.Format(time.RFC3339),
		})
	}
	return DocumentVersions{Items: items, Total: len(items)}
}

// DocumentAsk — POST /api/documents/ask: ответ rag как есть плюс from_doc.
type DocumentAsk struct {
	Answer          *string      `json:"answer"`
	Sources         []rag.Source `json:"sources"`
	Confidence      string       `json:"confidence"`
	NeedsEscalation bool         `json:"needs_escalation"`
	Topic           string       `json:"topic"`
	AbstainReason   *string      `json:"abstain_reason"`
	// FromDoc — хотя бы одна цитата из документа, о котором спрашивали; null — документ не задан.
	FromDoc *bool `json:"from_doc"`
}

func DocumentAskFrom(r services.AskResult) DocumentAsk {
	return DocumentAsk{
		Answer: r.Answer, Sources: r.Sources, Confidence: r.Confidence, NeedsEscalation: r.NeedsEscalation,
		Topic: r.Topic, AbstainReason: r.AbstainReason, FromDoc: r.FromDoc,
	}
}

// UnansweredCluster — группа похожих вопросов без ответа.
type UnansweredCluster struct {
	Key       string         `json:"key"`
	Title     string         `json:"title"`
	Keywords  []string       `json:"keywords"`
	Count     int            `json:"count"`
	Newcomers int            `json:"newcomers"`
	Reasons   map[string]int `json:"reasons"`
	Examples  []string       `json:"examples"`
	FirstAt   string         `json:"first_at"`
	LastAt    string         `json:"last_at"`
}

// Unanswered — GET /api/knowledge/unanswered.
type Unanswered struct {
	Items     []UnansweredCluster `json:"items"`
	Total     int                 `json:"total"`
	Questions int                 `json:"questions"`
	Since     string              `json:"since"`
}

func UnansweredFrom(r services.UnansweredResult) Unanswered {
	items := make([]UnansweredCluster, 0, len(r.Clusters))
	for _, c := range r.Clusters {
		items = append(items, UnansweredCluster{
			Key: c.Key, Title: c.Title, Keywords: c.Keywords, Count: c.Count, Newcomers: c.Newcomers, Reasons: c.Reasons,
			Examples: c.Examples, FirstAt: c.FirstAt.Format(time.RFC3339), LastAt: c.LastAt.Format(time.RFC3339),
		})
	}
	return Unanswered{Items: items, Total: len(items), Questions: r.Questions, Since: r.Since.Format(time.RFC3339)}
}
