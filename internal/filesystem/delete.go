package filesystem

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DeleteOptions controls a delete batch.
type DeleteOptions struct {
	// TrashDir, when non-empty, receives moved entries instead of removing them.
	TrashDir string
	// KeepRoots lists directories that must never be deleted (the scan roots).
	KeepRoots []string
}

// DeleteFailure describes one path that could not be removed.
type DeleteFailure struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

// DeleteResult summarizes a delete batch.
type DeleteResult struct {
	Deleted    []string        `json:"deleted"`
	Failed     []DeleteFailure `json:"failed"`
	FreedBytes int64           `json:"freedBytes"`
	Count      int             `json:"count"`
}

// DeletePreview is the confirmation payload shown before deleting. The Paths
// list is display-only and therefore capped: the web UI deletes from its own
// (chunked) selection, never from an echoed back list.
type DeletePreview struct {
	Count     int      `json:"count"`
	TotalSize int64    `json:"totalSize"`
	FileCount int      `json:"fileCount"`
	DirCount  int      `json:"dirCount"`
	Paths     []string `json:"paths"`
	Missing   []string `json:"missing"`
	Skipped   []string `json:"skipped"`
	Truncated bool     `json:"truncated"`
}

// previewPathLimit caps how many paths are echoed back for display.
const previewPathLimit = 200

// statSize returns the size of a single entry, or the recursive size of a
// directory, without following symlinks.
func statSize(path string) (int64, bool) {
	info, err := os.Lstat(path)
	if err != nil {
		return 0, false
	}
	if !info.IsDir() {
		return info.Size(), true
	}
	var total int64
	_ = filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 || d.IsDir() {
			return nil
		}
		if fi, err := d.Info(); err == nil {
			total += fi.Size()
		}
		return nil
	})
	return total, true
}

// PreviewDelete validates a selection and reports what would be freed.
func PreviewDelete(root string, paths []string, keepRoots []string) (*DeletePreview, error) {
	out := &DeletePreview{Paths: []string{}, Missing: []string{}, Skipped: []string{}}
	base := CleanRoot(root)
	kept := cleanKeepRoots(keepRoots)

	for _, raw := range paths {
		p, err := Resolve(root, raw)
		if err != nil {
			out.noteSkipped(raw)
			continue
		}
		if p == base || isKept(p, kept) {
			out.noteSkipped(p)
			continue
		}
		size, ok := statSize(p)
		if !ok {
			out.noteMissing(p)
			continue
		}
		if info, err := os.Lstat(p); err == nil && info.IsDir() {
			out.DirCount++
		} else {
			out.FileCount++
		}
		out.Count++
		out.TotalSize += size
		if len(out.Paths) < previewPathLimit {
			out.Paths = append(out.Paths, p)
		} else {
			out.Truncated = true
		}
	}
	return out, nil
}

func (d *DeletePreview) noteSkipped(p string) {
	if len(d.Skipped) < previewPathLimit {
		d.Skipped = append(d.Skipped, p)
	} else {
		d.Truncated = true
	}
}

func (d *DeletePreview) noteMissing(p string) {
	if len(d.Missing) < previewPathLimit {
		d.Missing = append(d.Missing, p)
	} else {
		d.Truncated = true
	}
}

// cleanKeepRoots normalizes the protected scan roots once per batch instead
// of once per selected path.
func cleanKeepRoots(keepRoots []string) []string {
	out := make([]string, 0, len(keepRoots))
	for _, k := range keepRoots {
		if strings.TrimSpace(k) == "" {
			continue
		}
		abs, err := filepath.Abs(k)
		if err != nil {
			continue
		}
		out = append(out, filepath.Clean(abs))
	}
	return out
}

// isKept expects already-normalized keep roots.
func isKept(p string, keepRoots []string) bool {
	for _, k := range keepRoots {
		if k == p {
			return true
		}
	}
	return false
}

// Delete removes (or trashes) the selected paths.
//
// Safety rules enforced here, not only in the API layer:
//  1. every path must resolve inside root;
//  2. the root itself can never be deleted;
//  3. entries listed in KeepRoots (scan roots) are never deleted;
//  4. "." and ".." can never be targeted.
func Delete(root string, paths []string, opt DeleteOptions) (*DeleteResult, error) {
	res := &DeleteResult{Deleted: []string{}, Failed: []DeleteFailure{}}
	base := CleanRoot(root)
	kept := cleanKeepRoots(opt.KeepRoots)

	if opt.TrashDir != "" {
		if err := os.MkdirAll(opt.TrashDir, 0o755); err != nil {
			return nil, fmt.Errorf("create trash dir: %w", err)
		}
	}

	for _, raw := range paths {
		p, err := Resolve(root, raw)
		if err != nil {
			res.Failed = append(res.Failed, DeleteFailure{Path: raw, Error: err.Error()})
			continue
		}
		if IsRoot(root, p) || p == base {
			res.Failed = append(res.Failed, DeleteFailure{Path: p, Error: "refusing to delete the root directory"})
			continue
		}
		if isKept(p, kept) {
			res.Failed = append(res.Failed, DeleteFailure{Path: p, Error: "refusing to delete a scan root directory"})
			continue
		}
		baseName := filepath.Base(p)
		if baseName == "." || baseName == ".." || baseName == string(filepath.Separator) {
			res.Failed = append(res.Failed, DeleteFailure{Path: p, Error: "invalid target"})
			continue
		}

		info, err := os.Lstat(p)
		if err != nil {
			res.Failed = append(res.Failed, DeleteFailure{Path: p, Error: err.Error()})
			continue
		}

		size := info.Size()
		if info.IsDir() {
			size, _ = statSize(p)
		}

		if opt.TrashDir != "" {
			err = moveToTrash(p, opt.TrashDir)
		} else if info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			err = os.RemoveAll(p)
		} else {
			err = os.Remove(p)
		}
		if err != nil {
			res.Failed = append(res.Failed, DeleteFailure{Path: p, Error: err.Error()})
			continue
		}
		res.Deleted = append(res.Deleted, p)
		res.FreedBytes += size
	}

	res.Count = len(res.Deleted)
	return res, nil
}

func moveToTrash(p, trashDir string) error {
	rel := strings.ReplaceAll(strings.TrimPrefix(p, string(filepath.Separator)), string(filepath.Separator), "_")
	target := filepath.Join(trashDir, fmt.Sprintf("%s_%d", rel, time.Now().UnixNano()))
	if err := os.Rename(p, target); err == nil {
		return nil
	}
	// Cross-device fallback.
	return fmt.Errorf("move to trash failed for %s", p)
}
