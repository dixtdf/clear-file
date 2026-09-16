// Package disk implements the on-demand directory size scan that powers the
// tree view and the WinDirStat style treemap.
//
// Nothing here ever runs while the user is only browsing a directory: the
// scan is started explicitly and always runs as a background task.
package disk

import (
	"container/heap"
	"context"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"file-cleaner/internal/filesystem"
	"file-cleaner/internal/task"
)

// Node is one directory or file in the scanned tree.
type Node struct {
	Name      string  `json:"name"`
	Path      string  `json:"path"`
	Size      int64   `json:"size"` // recursive size for directories
	IsDir     bool    `json:"isDir"`
	FileCount int64   `json:"fileCount"`
	Children  []*Node `json:"children,omitempty"`
}

// Result is the disk scan payload.
type Result struct {
	Root      *Node  `json:"root"`
	TopFiles  []Node `json:"topFiles"`
	Nodes     int64  `json:"nodes"`
	Files     int64  `json:"files"`
	Dirs      int64  `json:"dirs"`
	Bytes     int64  `json:"bytes"`
	Truncated bool   `json:"truncated"`

	mu    sync.RWMutex
	index map[string]*Node
	dir   string
}

// Len implements the task result length contract.
func (r *Result) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return int(r.Dirs)
}

// Count is an alias used by the task snapshot summary.
func (r *Result) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return int(r.Dirs)
}

// Summary returns the task-list summary, recomputed after every prune.
func (r *Result) Summary() map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.summaryLocked()
}

func (r *Result) summaryLocked() map[string]any {
	return map[string]any{
		"path":      r.dir,
		"bytes":     r.Bytes,
		"files":     r.Files,
		"dirs":      r.Dirs,
		"topFiles":  len(r.TopFiles),
		"truncated": r.Truncated,
	}
}

// Prune drops deleted files/directories from the size tree and subtracts
// their bytes from every ancestor, so the treemap refresh is accurate.
func (r *Result) Prune(paths []string) int {
	if len(paths) == 0 {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	removed := 0
	gone := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		gone[p] = struct{}{}
		if r.removeOne(p) {
			removed++
		}
	}
	if removed == 0 {
		return 0
	}

	if len(r.TopFiles) > 0 {
		kept := r.TopFiles[:0]
		for _, f := range r.TopFiles {
			if _, hit := gone[f.Path]; !hit {
				kept = append(kept, f)
			}
		}
		r.TopFiles = kept
	}
	if r.Root != nil {
		r.Bytes = r.Root.Size
		r.Files = r.Root.FileCount
	}
	return removed
}

func (r *Result) removeOne(p string) bool {
	parent, ok := r.index[filepath.Dir(p)]
	if !ok {
		return false
	}
	for i, c := range parent.Children {
		if c.Path != p {
			continue
		}
		parent.Children = append(parent.Children[:i], parent.Children[i+1:]...)
		r.subtract(p, c.Size, c.FileCount)
		if c.IsDir {
			r.dropIndex(c)
		}
		return true
	}
	return false
}

// subtract walks the ancestor chain, removing the deleted subtree's size.
func (r *Result) subtract(path string, size, files int64) {
	for cur := path; ; {
		if n, ok := r.index[cur]; ok {
			n.Size -= size
			n.FileCount -= files
			if n.Size < 0 {
				n.Size = 0
			}
			if n.FileCount < 0 {
				n.FileCount = 0
			}
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return
		}
		cur = parent
	}
}

func (r *Result) dropIndex(n *Node) {
	delete(r.index, n.Path)
	for _, c := range n.Children {
		if c.IsDir {
			r.dropIndex(c)
		}
	}
}

// Params describes a disk scan request.
type Params struct {
	Path     string
	MaxDepth int   // tree detail depth (default 8)
	MaxNodes int64 // safety cap on retained nodes (default 400000)
}

// Scan builds the size tree for path.
func Scan(ctx context.Context, root string, p Params, t *task.Task) (*Result, error) {
	dir, err := filesystem.EnsureDir(root, p.Path)
	if err != nil {
		return nil, err
	}
	if p.MaxDepth <= 0 {
		p.MaxDepth = 8
	}
	if p.MaxNodes <= 0 {
		p.MaxNodes = 400000
	}

	res := &Result{
		TopFiles: make([]Node, 0, 256),
		index:    map[string]*Node{},
		dir:      dir,
	}
	res.Root = res.build(ctx, dir, 0, p, t)
	if res.Root == nil {
		return nil, os.ErrInvalid
	}
	res.Files = res.Root.FileCount
	res.Bytes = res.Root.Size

	t.SetResult(res)
	t.SetSummary(res.Summary())
	return res, nil
}

// build walks one directory recursively, aggregating sizes bottom-up.
func (r *Result) build(ctx context.Context, path string, depth int, p Params, t *task.Task) *Node {
	if ctx.Err() != nil {
		return nil
	}
	if _, err := os.Lstat(path); err != nil {
		return nil
	}

	node := &Node{
		Name:  filepath.Base(path),
		Path:  path,
		IsDir: true,
	}
	r.Nodes++
	r.Dirs++
	r.index[path] = node
	t.AddDirs(1)
	t.SetCurrent(path)

	des, err := os.ReadDir(path)
	if err != nil {
		return node // unreadable directory: keep it, size stays 0
	}

	children := make([]*Node, 0, len(des))
	for _, de := range des {
		if ctx.Err() != nil {
			return node
		}
		full := filepath.Join(path, de.Name())
		fi, err := de.Info() // lstat: symlinks are never followed
		if err != nil {
			continue
		}
		if de.Type()&os.ModeSymlink != 0 {
			continue // skip links entirely so loops cannot happen
		}
		if fi.IsDir() {
			child := r.build(ctx, full, depth+1, p, t)
			if child == nil {
				continue
			}
			node.Size += child.Size
			node.FileCount += child.FileCount
			if depth+1 <= p.MaxDepth && r.Nodes < p.MaxNodes {
				children = append(children, child)
			} else {
				delete(r.index, full)
			}
			continue
		}

		node.Size += fi.Size()
		node.FileCount++
		r.Files++
		t.AddFiles(1)
		t.AddBytes(fi.Size())
		r.pushTop(Node{Name: fi.Name(), Path: full, Size: fi.Size(), IsDir: false, FileCount: 1})

		if depth+1 <= p.MaxDepth && r.Nodes < p.MaxNodes {
			r.Nodes++
			children = append(children, &Node{
				Name:      fi.Name(),
				Path:      full,
				Size:      fi.Size(),
				IsDir:     false,
				FileCount: 1,
			})
		} else {
			r.Truncated = true
		}
	}

	sort.Slice(children, func(i, j int) bool { return children[i].Size > children[j].Size })
	node.Children = children
	return node
}

// ---- top files (largest first, bounded heap) ----

type fileHeap []Node

func (h fileHeap) Len() int           { return len(h) }
func (h fileHeap) Less(i, j int) bool { return h[i].Size < h[j].Size }
func (h fileHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *fileHeap) Push(x any)        { *h = append(*h, x.(Node)) }
func (h *fileHeap) Pop() any          { old := *h; n := len(old); it := old[n-1]; *h = old[:n-1]; return it }

const topFilesLimit = 200

func (r *Result) pushTop(n Node) {
	r.TopFiles = append(r.TopFiles, n)
	if len(r.TopFiles) <= topFilesLimit*4 {
		return
	}
	h := fileHeap(r.TopFiles)
	heap.Init(&h)
	for h.Len() > topFilesLimit {
		heap.Pop(&h)
	}
	r.TopFiles = []Node(h)
}

// Finalize sorts the retained top files; called by the API before serving.
func (r *Result) Finalize() {
	if len(r.TopFiles) == 0 {
		return
	}
	sort.Slice(r.TopFiles, func(i, j int) bool { return r.TopFiles[i].Size > r.TopFiles[j].Size })
	if len(r.TopFiles) > topFilesLimit {
		h := fileHeap(r.TopFiles)
		heap.Init(&h)
		for h.Len() > topFilesLimit {
			heap.Pop(&h)
		}
		r.TopFiles = []Node(h)
		sort.Slice(r.TopFiles, func(i, j int) bool { return r.TopFiles[i].Size > r.TopFiles[j].Size })
	}
}

// Lookup returns the node for an absolute path.
func (r *Result) Lookup(path string) (*Node, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n, ok := r.index[path]
	return n, ok
}

// Tree returns the subtree below path, trimmed to maxDepth levels.
func (r *Result) Tree(path string, maxDepth int) (*Node, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if path == "" {
		return clone(r.Root, 0, maxDepth), true
	}
	n, ok := r.index[path]
	if !ok {
		return nil, false
	}
	return clone(n, 0, maxDepth), true
}

func clone(n *Node, depth, maxDepth int) *Node {
	if n == nil {
		return nil
	}
	out := &Node{
		Name:      n.Name,
		Path:      n.Path,
		Size:      n.Size,
		IsDir:     n.IsDir,
		FileCount: n.FileCount,
	}
	if maxDepth > 0 && depth >= maxDepth {
		return out
	}
	if len(n.Children) > 0 {
		out.Children = make([]*Node, 0, len(n.Children))
		for _, c := range n.Children {
			out.Children = append(out.Children, clone(c, depth+1, maxDepth))
		}
	}
	return out
}

// TreemapNode is a treemap cell.
type TreemapNode struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	IsDir bool   `json:"isDir"`
	Count int64  `json:"fileCount"`
}

// Treemap returns the direct children of path, largest first, ready to be
// laid out as rectangles. When limit > 0 the tail is folded into one cell.
func (r *Result) Treemap(path string, limit int) ([]TreemapNode, int64, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var n *Node
	if path == "" {
		n = r.Root
	} else if v, ok := r.index[path]; ok {
		n = v
	} else {
		return nil, 0, false
	}
	cells := make([]TreemapNode, 0, len(n.Children))
	for _, c := range n.Children {
		cells = append(cells, TreemapNode{
			Name:  c.Name,
			Path:  c.Path,
			Size:  c.Size,
			IsDir: c.IsDir,
			Count: c.FileCount,
		})
	}
	sort.Slice(cells, func(i, j int) bool { return cells[i].Size > cells[j].Size })

	if limit > 0 && len(cells) > limit {
		var rest int64
		var restCount int64
		for _, c := range cells[limit:] {
			rest += c.Size
			restCount += c.Count
		}
		cells = cells[:limit]
		cells = append(cells, TreemapNode{Name: "其他", Path: "__other__", Size: rest, Count: restCount})
	}
	return cells, n.Size, true
}
