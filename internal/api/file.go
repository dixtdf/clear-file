package api

import (
	"net/http"

	"file-cleaner/internal/disk"
	"file-cleaner/internal/filesystem"
	"file-cleaner/internal/version"
)

func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	total, free, avail, _ := disk.Usage(s.cfg.Root)

	var used uint64
	if total > free {
		used = total - free
	}
	var pct float64
	if total > 0 {
		pct = float64(used) / float64(total) * 100
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"version":   version.String(),
		"root":      s.cfg.Root,
		"trashMode": s.cfg.TrashMode,
		"trashDir":  s.cfg.TrashDir(),
		"disk": map[string]any{
			"total":       total,
			"used":        used,
			"free":        free,
			"avail":       avail,
			"usedPercent": pct,
		},
	})
}

func (s *Server) handleListFiles(w http.ResponseWriter, r *http.Request) {
	// A negative bound means "not set"; the query strings use <0 for that.
	minSize := qi64(r, "minSize", -1)
	maxSize := qi64(r, "maxSize", -1)

	opt := filesystem.ListOptions{
		Path:      qs(r, "path", s.cfg.Root),
		DirsOnly:  qbool(r, "dirsOnly", false),
		Page:      qi(r, "page", 1),
		PageSize:  qi(r, "pageSize", 200),
		Sort:      qs(r, "sort", "name"),
		Order:     qs(r, "order", "asc"),
		Name:      qs(r, "name", ""),
		Exts:      splitCSV(r.URL.Query().Get("ext")),
		MinSize:   minSize,
		MaxSize:   maxSize,
		MTimeFrom: qi64(r, "mtimeFrom", 0),
		MTimeTo:   qi64(r, "mtimeTo", 0),
	}

	res, err := filesystem.List(s.cfg.Root, opt)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

type deleteReq struct {
	Paths     []string `json:"paths"`
	KeepRoots []string `json:"keepRoots"`
	// TaskID, when set, prunes the deleted paths out of that task's results
	// so a refreshed page no longer lists them.
	TaskID string `json:"taskId"`
}

func (s *Server) handleDeletePreview(w http.ResponseWriter, r *http.Request) {
	var req deleteReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	preview, err := filesystem.PreviewDelete(s.cfg.Root, req.Paths, req.KeepRoots)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	var req deleteReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.Paths) == 0 {
		writeError(w, http.StatusBadRequest, "未选择任何文件")
		return
	}
	opt := filesystem.DeleteOptions{KeepRoots: req.KeepRoots}
	if s.cfg.TrashMode {
		opt.TrashDir = s.cfg.TrashDir()
	}
	res, err := filesystem.Delete(s.cfg.Root, req.Paths, opt)
	if err != nil {
		fail(w, err)
		return
	}

	// Keep the originating scan result in sync with what is on disk now.
	pruned := 0
	if req.TaskID != "" {
		if t, ok := s.tasks.Get(req.TaskID); ok && len(res.Deleted) > 0 {
			pruned = t.Prune(res.Deleted)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"deleted":    res.Deleted,
		"failed":     res.Failed,
		"freedBytes": res.FreedBytes,
		"count":      res.Count,
		"pruned":     pruned,
		"taskId":     req.TaskID,
	})
}
