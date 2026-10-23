package services

import (
	"context"
	"sync"
	"time"
)

// Pinger, RAGHealth — что health нужно от базы и rag. Интерфейсы здесь, чтобы тест обходился
// без Postgres и без rag.
type Pinger interface {
	Ping(ctx context.Context) error
}

type RAGHealth interface {
	Health(ctx context.Context, timeout time.Duration) error
}

// HealthService — готовность зависимостей для GET /api/health.
type HealthService struct {
	db         Pinger
	rag        RAGHealth
	ragTimeout time.Duration
}

// Readiness — состояние зависимостей: true — отвечает.
type Readiness struct {
	DB  bool
	RAG bool
}

func NewHealthService(db Pinger, rag RAGHealth, ragTimeout time.Duration) *HealthService {
	if ragTimeout <= 0 {
		ragTimeout = 2 * time.Second
	}
	return &HealthService{db: db, rag: rag, ragTimeout: ragTimeout}
}

// Check — база и rag параллельно: health отвечает за время самой медленной проверки, а не за
// сумму. Причины отказов сюда не возвращаются — наружу идёт только ok/down.
func (h *HealthService) Check(ctx context.Context) Readiness {
	var r Readiness
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if h.db == nil {
			return
		}
		ctx, cancel := context.WithTimeout(ctx, h.ragTimeout)
		defer cancel()
		r.DB = h.db.Ping(ctx) == nil
	}()
	go func() {
		defer wg.Done()
		if h.rag == nil {
			return
		}
		r.RAG = h.rag.Health(ctx, h.ragTimeout) == nil
	}()
	wg.Wait()
	return r
}
