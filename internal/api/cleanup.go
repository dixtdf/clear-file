package api

import (
	"context"
	"net/http"

	"file-cleaner/internal/cleanup"
	"file-cleaner/internal/filesystem"
	"file-cleaner/internal/task"
)

type cleanupScanReq struct {
	Mode         string `json:"mode"` // empty_file | empty_dir | small_file
	Path         string `json:"path"`
	Recursive    *bool  `json:"recursive"`
	MaxSizeBytes int64  `json:"maxSizeBytes"`
	IncludeEmpty *bool  `json:"includeEmpty"`
}

func (s *Server) handleCleanupScan(w http.ResponseWriter, r *http.Request) {
	var req cleanupScanReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Path == "" {
		req.Path = s.cfg.Root
	}
	mode := cleanup.Mode(req.Mode)
	switch mode {
	case cleanup.ModeEmptyFile, cleanup.ModeEmptyDir, cleanup.ModeSmallFile:
	default:
		writeError(w, http.StatusBadRequest, "未知的扫描类型")
		return
	}

	recursive := true
	if req.Recursive != nil {
		recursive = *req.Recursive
	}
	includeEmpty := true
	if req.IncludeEmpty != nil {
		includeEmpty = *req.IncludeEmpty
	}
	if mode == cleanup.ModeSmallFile && req.MaxSizeBytes <= 0 {
		req.MaxSizeBytes = 1024 // default: files below 1 KB
	}

	// Fail fast on a bad path instead of failing inside the task.
	resolved, err := filesystem.EnsureDir(s.cfg.Root, req.Path)
	if err != nil {
		fail(w, err)
		return
	}
	req.Path = resolved
	params := cleanup.Params{
		Mode:         mode,
		Path:         req.Path,
		Recursive:    recursive,
		MaxSize:      req.MaxSizeBytes,
		IncludeEmpty: includeEmpty,
		MaxResults:   s.cfg.MaxResultItems,
	}

	t := s.tasks.Start("cleanup:"+string(mode), map[string]any{
		"mode":         string(mode),
		"path":         req.Path,
		"recursive":    recursive,
		"maxSizeBytes": req.MaxSizeBytes,
	}, func(ctx context.Context, t *task.Task) error {
		_, err := cleanup.Scan(ctx, s.cfg.Root, params, t)
		return err
	})

	writeJSON(w, http.StatusAccepted, map[string]any{"taskId": t.ID})
}
