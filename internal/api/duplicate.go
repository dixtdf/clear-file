package api

import (
	"context"
	"net/http"

	"file-cleaner/internal/duplicate"
	"file-cleaner/internal/filesystem"
	"file-cleaner/internal/task"
)

type duplicateScanReq struct {
	Paths             []string `json:"paths"`
	Mode              string   `json:"mode"` // size | fast | full
	MinSizeBytes      int64    `json:"minSizeBytes"`
	MaxCandidateFiles int      `json:"maxCandidateFiles"`
}

func (s *Server) handleDuplicateScan(w http.ResponseWriter, r *http.Request) {
	var req duplicateScanReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.Paths) == 0 {
		req.Paths = []string{s.cfg.Root}
	}
	mode := duplicate.Mode(req.Mode)
	switch mode {
	case duplicate.ModeSize, duplicate.ModeFast, duplicate.ModeFull:
	case "":
		mode = duplicate.ModeFast
	default:
		writeError(w, http.StatusBadRequest, "未知的校验模式")
		return
	}

	maxGroups := s.cfg.MaxResultItems
	if maxGroups > 5000 {
		maxGroups = 5000 // 5000 groups is already far more than a UI can act on
	}

	// Fail fast on bad directories instead of failing inside the task.
	resolved := make([]string, 0, len(req.Paths))
	for _, p := range req.Paths {
		d, err := filesystem.EnsureDir(s.cfg.Root, p)
		if err != nil {
			fail(w, err)
			return
		}
		resolved = append(resolved, d)
	}
	req.Paths = resolved
	params := duplicate.Params{
		Paths:             req.Paths,
		Mode:              mode,
		MinSize:           req.MinSizeBytes,
		MaxCandidateFiles: req.MaxCandidateFiles,
		MaxGroups:         maxGroups,
	}

	t := s.tasks.Start("duplicate", map[string]any{
		"paths": req.Paths,
		"mode":  string(mode),
	}, func(ctx context.Context, t *task.Task) error {
		_, err := duplicate.Scan(ctx, s.cfg.Root, params, t)
		return err
	})

	writeJSON(w, http.StatusAccepted, map[string]any{"taskId": t.ID})
}

func (s *Server) handleDuplicateGroups(w http.ResponseWriter, r *http.Request) {
	t, ok := s.tasks.Get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "任务不存在")
		return
	}
	res, ok := t.Result().(*duplicate.Result)
	if !ok {
		writeError(w, http.StatusConflict, "任务尚未产生结果")
		return
	}

	page := qi(r, "page", 1)
	size := qi(r, "pageSize", 50)
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 500 {
		size = 50
	}

	groups, total := res.PageGroups(page, size)

	writeJSON(w, http.StatusOK, map[string]any{
		"total":       total,
		"page":        page,
		"pageSize":    size,
		"groups":      groups,
		"totalFiles":  res.TotalFiles,
		"totalSize":   res.TotalSize,
		"reclaimable": res.Reclaimable,
		"candidates":  res.CandidateGroups,
		"truncated":   res.Truncated,
	})
}
