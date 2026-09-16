package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"file-cleaner/internal/cleanup"
	"file-cleaner/internal/task"
)

func (s *Server) handleTaskList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"tasks": s.tasks.List()})
}

func (s *Server) handleTaskGet(w http.ResponseWriter, r *http.Request) {
	t, ok := s.tasks.Get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "任务不存在")
		return
	}
	writeJSON(w, http.StatusOK, t.Snapshot())
}

func (s *Server) handleTaskCancel(w http.ResponseWriter, r *http.Request) {
	if err := s.tasks.Cancel(r.PathValue("id")); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleTaskRemove(w http.ResponseWriter, r *http.Request) {
	s.tasks.Remove(r.PathValue("id"))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleTaskResults serves paginated cleanup results.
func (s *Server) handleTaskResults(w http.ResponseWriter, r *http.Request) {
	t, ok := s.tasks.Get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "任务不存在")
		return
	}
	res, ok := t.Result().(*cleanup.Result)
	if !ok {
		writeError(w, http.StatusConflict, "任务尚未产生结果")
		return
	}

	page := qi(r, "page", 1)
	size := qi(r, "pageSize", 100)
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 2000 {
		size = 100
	}

	items, total, totalSize, truncated := res.Page(page, size)
	writeJSON(w, http.StatusOK, map[string]any{
		"total":     total,
		"page":      page,
		"pageSize":  size,
		"items":     items,
		"totalSize": totalSize,
		"truncated": truncated,
	})
}

// handleTaskEvents streams task progress as Server-Sent Events.
func (s *Server) handleTaskEvents(w http.ResponseWriter, r *http.Request) {
	t, ok := s.tasks.Get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "任务不存在")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "当前服务器不支持流式推送")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	events, unsub := t.Subscribe()
	defer unsub()

	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			if err := writeSSE(w, ev); err != nil {
				return
			}
			flusher.Flush()
			if ev.Type == "done" {
				return
			}
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, ev task.Event) error {
	payload, err := json.Marshal(ev.Data)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, payload)
	return err
}
