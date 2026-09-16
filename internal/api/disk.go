package api

import (
	"context"
	"net/http"

	"file-cleaner/internal/disk"
	"file-cleaner/internal/filesystem"
	"file-cleaner/internal/task"
)

type diskScanReq struct {
	Path     string `json:"path"`
	MaxDepth int    `json:"maxDepth"`
}

func (s *Server) handleDiskScan(w http.ResponseWriter, r *http.Request) {
	var req diskScanReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Path == "" {
		req.Path = s.cfg.Root
	}
	// Fail fast on a bad path instead of failing inside the task.
	resolved, err := filesystem.EnsureDir(s.cfg.Root, req.Path)
	if err != nil {
		fail(w, err)
		return
	}
	req.Path = resolved

	params := disk.Params{Path: req.Path, MaxDepth: req.MaxDepth}

	t := s.tasks.Start("disk", map[string]any{"path": req.Path}, func(ctx context.Context, t *task.Task) error {
		res, err := disk.Scan(ctx, s.cfg.Root, params, t)
		if err != nil {
			return err
		}
		res.Finalize()
		return nil
	})

	writeJSON(w, http.StatusAccepted, map[string]any{"taskId": t.ID})
}

func (s *Server) handleDiskTree(w http.ResponseWriter, r *http.Request) {
	res, ok := s.diskResult(w, r)
	if !ok {
		return
	}
	path := qs(r, "path", "")
	depth := qi(r, "depth", 3)
	if depth < 1 || depth > 12 {
		depth = 3
	}
	node, ok := res.Tree(path, depth)
	if !ok {
		writeError(w, http.StatusNotFound, "目录不在扫描结果中")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"root":     res.Root,
		"node":     node,
		"topFiles": res.TopFiles,
	})
}

func (s *Server) handleDiskTreemap(w http.ResponseWriter, r *http.Request) {
	res, ok := s.diskResult(w, r)
	if !ok {
		return
	}
	path := qs(r, "path", "")
	limit := qi(r, "limit", 300)
	cells, total, found := res.Treemap(path, limit)
	if !found {
		writeError(w, http.StatusNotFound, "目录不在扫描结果中")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path":  path,
		"total": total,
		"cells": cells,
	})
}

func (s *Server) diskResult(w http.ResponseWriter, r *http.Request) (*disk.Result, bool) {
	t, ok := s.tasks.Get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "任务不存在")
		return nil, false
	}
	res, ok := t.Result().(*disk.Result)
	if !ok {
		writeError(w, http.StatusConflict, "任务尚未产生结果")
		return nil, false
	}
	return res, true
}
