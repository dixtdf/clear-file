// Package api exposes the REST + SSE surface consumed by the Vue 3 web UI.
package api

import (
	"net/http"

	"file-cleaner/internal/config"
	"file-cleaner/internal/task"
)

// Server wires configuration and the task manager into an HTTP handler.
type Server struct {
	cfg   config.Config
	tasks *task.Manager
}

// New builds the API server.
func New(cfg config.Config, tasks *task.Manager) *Server {
	return &Server{cfg: cfg, tasks: tasks}
}

// Handler returns the root HTTP handler (API + SPA).
func (s *Server) Handler(static http.Handler) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/system/info", s.handleSystemInfo)

	mux.HandleFunc("GET /api/v1/files", s.handleListFiles)
	mux.HandleFunc("POST /api/v1/files/delete/preview", s.handleDeletePreview)
	mux.HandleFunc("POST /api/v1/files/delete", s.handleDelete)

	mux.HandleFunc("POST /api/v1/cleanup/scan", s.handleCleanupScan)

	mux.HandleFunc("POST /api/v1/duplicate/scan", s.handleDuplicateScan)
	mux.HandleFunc("GET /api/v1/duplicate/{id}/groups", s.handleDuplicateGroups)

	mux.HandleFunc("POST /api/v1/disk/scan", s.handleDiskScan)
	mux.HandleFunc("GET /api/v1/disk/{id}/tree", s.handleDiskTree)
	mux.HandleFunc("GET /api/v1/disk/{id}/treemap", s.handleDiskTreemap)

	mux.HandleFunc("GET /api/v1/tasks", s.handleTaskList)
	mux.HandleFunc("GET /api/v1/tasks/{id}", s.handleTaskGet)
	mux.HandleFunc("GET /api/v1/tasks/{id}/results", s.handleTaskResults)
	mux.HandleFunc("GET /api/v1/tasks/{id}/events", s.handleTaskEvents)
	mux.HandleFunc("POST /api/v1/tasks/{id}/cancel", s.handleTaskCancel)
	mux.HandleFunc("DELETE /api/v1/tasks/{id}", s.handleTaskRemove)

	if static != nil {
		mux.Handle("/", static)
	}
	return withMiddleware(mux)
}

// withMiddleware adds CORS (for the Vite dev server) and panic recovery.
func withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Header.Get("Origin") != "" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
		defer func() {
			if rec := recover(); rec != nil {
				writeError(w, http.StatusInternalServerError, "内部错误")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
