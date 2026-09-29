// Package duplicate implements the three-stage duplicate file scan:
//
//	stage 1  group by file size only            (fastest, low accuracy)
//	stage 2  + head/middle/tail probe hashes    (fast, very high accuracy)
//	stage 3  + full content hash                (slowest, highest accuracy)
//
// Stage 1 runs as two streaming passes so that a 10 million file tree never
// has to be held in memory: pass A counts how many files exist per size,
// pass B keeps only the files whose size collided.
package duplicate

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"file-cleaner/internal/filesystem"
	"file-cleaner/internal/task"
)

// Mode is the duplicate scan accuracy mode.
type Mode string

const (
	ModeSize Mode = "size"
	ModeFast Mode = "fast"
	ModeFull Mode = "full"
)

// File is one duplicate candidate.
type File struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	MTime int64  `json:"mtime"`
}

// Group is a set of files considered duplicates of each other.
type Group struct {
	ID          int    `json:"id"`
	Size        int64  `json:"size"`
	Count       int    `json:"count"`
	Reclaimable int64  `json:"reclaimable"` // size * (count - 1)
	Verified    string `json:"verified"`    // size | fast | full
	Files       []File `json:"files"`
}

// Result is the scan payload.
type Result struct {
	Groups          []Group `json:"groups"`
	CandidateGroups int     `json:"candidateGroups"`
	TotalGroups     int     `json:"totalGroups"`
	TotalFiles      int     `json:"totalFiles"`
	TotalSize       int64   `json:"totalSize"`
	Reclaimable     int64   `json:"reclaimable"`
	Truncated       bool    `json:"truncated"`

	mu       sync.RWMutex
	mode     string
	scanDirs []string
}

// Len implements the task result length contract.
func (r *Result) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.Groups)
}

// Count is an alias used by the task snapshot summary.
func (r *Result) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.Groups)
}

// Summary returns the task-list summary, recomputed after every prune.
func (r *Result) Summary() map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.summaryLocked()
}

func (r *Result) summaryLocked() map[string]any {
	return map[string]any{
		"groups":      r.TotalGroups,
		"files":       r.TotalFiles,
		"totalSize":   r.TotalSize,
		"reclaimable": r.Reclaimable,
		"candidates":  r.CandidateGroups,
		"mode":        r.mode,
		"truncated":   r.Truncated,
	}
}

// Prune drops deleted files from their groups; groups that no longer hold at
// least two files stop being duplicates and are removed entirely.
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

	removed := 0
	kept := make([]Group, 0, len(r.Groups))
	for _, g := range r.Groups {
		files := make([]File, 0, len(g.Files))
		for _, f := range g.Files {
			if _, hit := set[f.Path]; hit {
				removed++
				continue
			}
			files = append(files, f)
		}
		if len(files) < 2 {
			continue // nothing left to deduplicate
		}
		g.Files = files
		g.Count = len(files)
		g.Reclaimable = g.Size * int64(len(files)-1)
		kept = append(kept, g)
	}
	if removed == 0 {
		return 0
	}

	var files, total, reclaimable int64
	for i := range kept {
		kept[i].ID = i + 1
		files += int64(kept[i].Count)
		total += kept[i].Size * int64(kept[i].Count)
		reclaimable += kept[i].Reclaimable
	}
	r.Groups = kept
	r.TotalGroups = len(kept)
	r.TotalFiles = int(files)
	r.TotalSize = total
	r.Reclaimable = reclaimable
	return removed
}

// PageGroups returns one page of duplicate groups plus current totals.
func (r *Result) PageGroups(page, size int) (groups []Group, total int) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	total = len(r.Groups)
	start := (page - 1) * size
	if start > total {
		start = total
	}
	end := start + size
	if end > total {
		end = total
	}
	return r.Groups[start:end], total
}

// Params describes a duplicate scan request.
type Params struct {
	// Paths are the scan directories (resolved inside the allowed root).
	Paths []string
	// Mode selects the accuracy stage.
	Mode Mode
	// MinSize ignores files smaller than this (bytes, default 1).
	MinSize int64
	// MaxCandidateFiles caps how many files are hash-verified.
	MaxCandidateFiles int
	// MaxGroups caps how many duplicate groups are retained.
	MaxGroups int
}

// Scan runs the duplicate scan and streams progress into t.
func Scan(ctx context.Context, root string, p Params, t *task.Task) (*Result, error) {
	dirs := make([]string, 0, len(p.Paths))
	for _, raw := range p.Paths {
		d, err := filesystem.EnsureDir(root, raw)
		if err != nil {
			return nil, err
		}
		if resolved, err := filepath.EvalSymlinks(d); err == nil {
			d = resolved
		}
		covered := false
		for _, existing := range dirs {
			if directoryContains(existing, d) {
				covered = true
				break
			}
		}
		if covered {
			continue
		}
		kept := dirs[:0]
		for _, existing := range dirs {
			if !directoryContains(d, existing) {
				kept = append(kept, existing)
			}
		}
		dirs = append(kept, d)
	}
	if len(dirs) == 0 {
		return nil, errors.New("至少需要一个扫描目录")
	}
	if p.MinSize <= 0 {
		p.MinSize = 1 // zero byte files are handled by the empty-file scan
	}
	if p.MaxCandidateFiles <= 0 {
		p.MaxCandidateFiles = 200000
	}
	if p.MaxGroups <= 0 {
		p.MaxGroups = 2000
	}
	if p.Mode == "" {
		p.Mode = ModeFast
	}

	res := &Result{Groups: make([]Group, 0, 64), mode: string(p.Mode), scanDirs: dirs}

	// ---- pass A: count files per size (only the counter map is kept) ----
	t.SetPhase("统计文件")
	counts := make(map[int64]int64, 1024)
	for _, d := range dirs {
		if err := walkFiles(ctx, d, t, true, func(path string, info fs.FileInfo) error {
			if info.Size() < p.MinSize {
				return nil
			}
			counts[info.Size()]++
			return nil
		}); err != nil {
			return nil, err
		}
	}

	// ---- pass B: keep only the files whose size is shared ----
	t.SetPhase("筛选候选文件")
	candidates := make(map[int64][]File, len(counts)/4+1)
	total := 0
	truncated := false
	for _, d := range dirs {
		if truncated {
			break
		}
		if err := walkFiles(ctx, d, t, false, func(path string, info fs.FileInfo) error {
			size := info.Size()
			if counts[size] < 2 {
				return nil
			}
			if total >= p.MaxCandidateFiles {
				truncated = true
				return fs.SkipAll
			}
			candidates[size] = append(candidates[size], File{
				Path:  path,
				Name:  info.Name(),
				Size:  size,
				MTime: info.ModTime().UnixMilli(),
			})
			total++
			t.IncCandidates(1)
			return nil
		}); err != nil {
			return nil, err
		}
	}
	res.CandidateGroups = len(candidates)
	res.Truncated = truncated

	// ---- stage 3: hash verification ----
	if p.Mode == ModeSize {
		t.SetPhase("汇总结果")
	} else {
		t.SetPhase("校验文件内容")
	}
	sizes := make([]int64, 0, len(candidates))
	for size := range candidates {
		sizes = append(sizes, size)
	}
	sort.Slice(sizes, func(i, j int) bool { return sizes[i] > sizes[j] }) // biggest win first

	groups := make([]Group, 0, 64)
	for _, size := range sizes {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		files := candidates[size]
		var buckets map[string][]File
		var verified string

		switch p.Mode {
		case ModeSize:
			buckets = map[string][]File{"size:" + itoa(size): files}
			verified = "size"
		case ModeFast:
			b, err := groupBy(ctx, files, t, ModeFast)
			if err != nil {
				return nil, err
			}
			buckets, verified = b, "fast"
		default:
			b, err := groupBy(ctx, files, t, ModeFull)
			if err != nil {
				return nil, err
			}
			buckets, verified = b, "full"
		}

		for _, group := range buckets {
			if len(group) < 2 {
				continue
			}
			sort.Slice(group, func(i, j int) bool {
				return !filesystem.NaturalLess(group[j].Path, group[i].Path)
			})
			g := Group{
				Size:        size,
				Count:       len(group),
				Reclaimable: size * int64(len(group)-1),
				Verified:    verified,
				Files:       group,
			}
			groups = append(groups, g)
			res.TotalFiles += len(group)
			res.TotalSize += size * int64(len(group))
			res.Reclaimable += g.Reclaimable
			t.IncFound(1)
			if len(groups) >= p.MaxGroups {
				res.Truncated = true
				break
			}
		}
		if len(groups) >= p.MaxGroups {
			break
		}
	}

	sort.SliceStable(groups, func(i, j int) bool { return groups[i].Reclaimable > groups[j].Reclaimable })
	for i := range groups {
		groups[i].ID = i + 1
	}
	res.Groups = groups
	res.TotalGroups = len(groups)

	t.SetResult(res)
	t.SetSummary(res.Summary())
	return res, nil
}

func directoryContains(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))))
}

// walkFiles streams every regular file below dir (symlinks are never
// followed) and reports progress on the shared task.
func walkFiles(ctx context.Context, dir string, t *task.Task, countProgress bool, fn func(path string, info fs.FileInfo) error) error {
	err := filesystem.Walk(ctx, dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if countProgress {
				t.AddDirs(1)
			}
			t.SetCurrent(path)
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if countProgress {
			t.AddFiles(1)
			t.AddBytes(info.Size())
		}
		return fn(path, info)
	})
	if err != nil && ctx.Err() == nil && err != fs.SkipAll {
		return err
	}
	return ctx.Err()
}

// groupBy computes probe or full signatures with a bounded worker pool and
// buckets files that share a signature.
func groupBy(ctx context.Context, files []File, t *task.Task, mode Mode) (map[string][]File, error) {
	type item struct {
		file File
		sig  string
		err  error
	}

	workers := 4
	if n := numCPU(); n > 0 {
		workers = n
	}
	if workers > 8 {
		workers = 8
	}

	jobs := make(chan File)
	out := make(chan item, workers*2)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range jobs {
				if ctx.Err() != nil {
					out <- item{file: f, err: ctx.Err()}
					continue
				}
				var sig string
				var readBytes int64
				var err error
				if mode == ModeFull {
					sig, _, err = fullSignature(ctx, f.Path, t.AddReadBytes)
				} else {
					sig, readBytes, err = probeSignature(ctx, f.Path, f.Size)
					if readBytes > 0 {
						t.AddReadBytes(readBytes)
					}
				}
				out <- item{file: f, sig: sig, err: err}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, f := range files {
			if ctx.Err() != nil {
				return
			}
			jobs <- f
		}
	}()

	go func() {
		wg.Wait()
		close(out)
	}()

	buckets := make(map[string][]File, len(files)/2+1)
	for it := range out {
		if it.err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue // unreadable file: drop it from the candidate set
		}
		t.SetCurrent(it.file.Path)
		buckets[it.sig] = append(buckets[it.sig], it.file)
	}
	return buckets, ctx.Err()
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
