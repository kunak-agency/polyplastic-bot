package handlers

import (
	"main/services"
)

// Handler — HTTP-слой (Fiber): in/* → service → out/*. В БД мимо services не лезет.
type Handler struct {
	services *services.Services
	origins  map[string]bool // ORIGINS: с каких страниц можно открыть WebSocket звонка (SetOrigins)
}

func NewHandlerManager(s *services.Services) *Handler {
	return &Handler{services: s}
}
