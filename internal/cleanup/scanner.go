// Package cleanup implements the "cleanup" scans: empty files, empty
// directories (with recursive pruning semantics) and small files.
package cleanup

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"file-cleaner/internal/filesystem"
	"file-cleaner/internal/task"
)

// Mode identifies the cleanup scan type.
type Mode string

const (
	ModeEmptyFile Mode = "empty_file"
	ModeEmptyDir  Mode = "empty_dir"
	ModeSmallFile Mode = "small_file"
)

// Item is one scan result row.
type Item struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	MTime   int64  `json:"mtime"`
	IsDir   bool   `json:"isDir"`
	RelPath string `json:"relPath"`
}

// Result is the scan payload kept in memory and served with pagination.
type Result struct {
	Items     []Item `json:"items"`
	Truncated bool   `json:"truncated"`
	TotalSize int64  `json:"totalSize"`
	Scanned   int64  `json:"scannedFiles"`

	mu       sync.RWMutex
	scanRoot string
}

// Len implements the task result length contract.
func (r *Result) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.Items)
}

// Count is an alias used by the task snapshot summary.
func (r *Result) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.Items)
}

// Summary returns the task-list summary, recomputed after every prune.
func (r *Result) Summary() map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.summaryLocked()
}

func (r *Result) summaryLocked() map[string]any {
	return map[string]any{
		"count":     len(r.Items),
		"totalSize": r.TotalSize,
		"scanRoot":  r.scanRoot,
		"truncated": r.Truncated,
	}
}

// Prune drops deleted entries so a refreshed page no longer lists them.
func (r *Result) Prune(paths []string) int {
	if len(paths) == 0 {
		return 0
	}
	set := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		set[p] = struct{}{}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	kept := r.Items[:0]
	removed := 0
	var total int64
	for _, it := range r.Items {
		if _, hit := set[it.Path]; hit {
			removed++
			continue
		}
		kept = append(kept, it)
		total += it.Size
	}
	r.Items = kept
	r.TotalSize = total
	return removed
}

// Page returns one page of results together with the current totals.
func (r *Result) Page(page, size int) (items []Item, total int, totalSize int64, truncated bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	total = len(r.Items)
	start := (page - 1) * size
	if start > total {
		start = total
	}
	end := start + size
	if end > total {
		end = total
	}
	items = r.Items[start:end]
	return items, total, r.TotalSize, r.Truncated
}

// Params describes a cleanup scan request.
type Params struct {
	Mode Mode
	// Path is the scan root, always inside the allowed root.
	Path string
	// Recursive enables walking into sub directories (default true).
	Recursive bool
	// MaxSize is the inclusive upper bound for small_file scans, in bytes.
	MaxSize int64
	// IncludeEmpty includes zero byte files in small_file scans.
	IncludeEmpty bool
	// MaxResults caps the in-memory result set.
	MaxResults int
}

// Scan runs a cleanup scan and reports progress through t.
func Scan(ctx context.Context, root string, p Params, t *task.Task) (*Result, error) {
	dir, err := filesystem.EnsureDir(root, p.Path)
	if err != nil {
		return nil, err
	}
	if p.MaxResults <= 0 {
		p.MaxResults = 500000
	}
	res := &Result{Items: make([]Item, 0, 1024), scanRoot: dir}

	switch p.Mode {
	case ModeEmptyFile:
		err = scanFiles(ctx, dir, p, res, t, func(size int64) bool { return size == 0 })
	case ModeSmallFile:
		err = scanFiles(ctx, dir, p, res, t, func(size int64) bool {
			if size == 0 && !p.IncludeEmpty {
				return false
			}
			return size <= p.MaxSize
		})
	case ModeEmptyDir:
		err = scanEmptyDirs(ctx, dir, p, res, t)
	default:
		return nil, os.ErrInvalid
	}
	if err != nil {
		return nil, err
	}

	sort.SliceStable(res.Items, func(i, j int) bool {
		if res.Items[i].Size != res.Items[j].Size {
			return res.Items[i].Size < res.Items[j].Size
		}
		return filesystem.NaturalLess(res.Items[i].Name, res.Items[j].Name)
	})
	for _, it := range res.Items {
		res.TotalSize += it.Size
	}
	t.SetResult(res)
	t.SetSummary(res.Summary())
	return res, nil
}

// scanFiles handles empty_file and small_file scans in a single streaming
// pass. Nothing is buffered beyond the (capped) result set.
func scanFiles(ctx context.Context, dir string, p Params, res *Result, t *task.Task, keep func(int64) bool) error {
	err := filesystem.Walk(ctx, dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries, keep the scan alive
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			t.AddDirs(1)
			t.SetCurrent(path)
			if path != dir && !p.Recursive {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil // never follow links; a link is not a real file
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		t.AddFiles(1)
		t.AddBytes(info.Size())
		if !keep(info.Size()) {
			return nil
		}
		t.IncFound(1)
		res.Items = append(res.Items, Item{
			Path:    path,
			Name:    d.Name(),
			Size:    info.Size(),
			MTime:   info.ModTime().UnixMilli(),
			RelPath: rel(dir, path),
		})
		if len(res.Items) >= p.MaxResults {
			res.Truncated = true
			return fs.SkipAll
		}
		return nil
	})
	if err != nil && ctx.Err() == nil && err != fs.SkipAll {
		return err
	}
	return ctx.Err()
}

// scanEmptyDirs finds directories that are empty, or that become empty once
// their empty descendants are removed (bottom-up pruning):
//
//	/a/b/c  with c empty -> c is reported; b becomes empty -> b reported; a
//	becomes empty only if it contained nothing but b -> then a is reported.
//
// The scan root itself is never reported, so a fully emptied
// /mnt/download stays in place.
func scanEmptyDirs(ctx context.Context, dir string, p Params, res *Result, t *task.Task) error {
	// visit returns true when the directory ends up empty (all of its
	// content is removable). The caller is responsible for reporting it.
	var visit func(path string) (bool, error)
	visit = func(path string) (bool, error) {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		t.SetCurrent(path)

		des, err := os.ReadDir(path)
		if err != nil {
			return false, nil // unreadable: treat as not empty, keep scanning
		}
		// A directory is empty when every entry it holds is itself removable.
		// Starting from true also covers the "no entries at all" case.
		empty := true

		for _, de := range des {
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			info, err := de.Info()
			if err != nil {
				empty = false
				continue
			}
			full := filepath.Join(path, de.Name())
			if info.IsDir() {
				t.AddDirs(1)
				if !p.Recursive {
					empty = false
					continue
				}
				childEmpty, err := visit(full)
				if err != nil {
					return false, err
				}
				if childEmpty {
					if err := report(res, t, p, full, de.Name(), info, dir); err != nil {
						return false, err
					}
				} else {
					empty = false
				}
				continue
			}
			t.AddFiles(1)
			empty = false
		}
		return empty, nil
	}

	des, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, de := range des {
		if !de.IsDir() {
			continue
		}
		info, err := de.Info()
		if err != nil {
			continue
		}
		full := filepath.Join(dir, de.Name())
		t.AddDirs(1)
		childEmpty, err := visit(full)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err == fs.SkipAll {
				return nil
			}
			return err
		}
		if childEmpty {
			if err := report(res, t, p, full, de.Name(), info, dir); err != nil {
				if err == fs.SkipAll {
					return nil
				}
				return err
			}
		}
	}
	return nil
}

func report(res *Result, t *task.Task, p Params, full, name string, info fs.FileInfo, scanRoot string) error {
	t.IncFound(1)
	res.Items = append(res.Items, Item{
		Path:    full,
		Name:    name,
		MTime:   info.ModTime().UnixMilli(),
		IsDir:   true,
		RelPath: rel(scanRoot, full),
	})
	if len(res.Items) >= p.MaxResults {
		res.Truncated = true
		return fs.SkipAll
	}
	return nil
}

func rel(base, p string) string {
	r, err := filepath.Rel(base, p)
	if err != nil {
		return p
	}
	return strings.TrimPrefix(r, "."+string(filepath.Separator))
}
