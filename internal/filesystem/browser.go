package filesystem

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Entry is a single file system row shown in the browser.
type Entry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	IsDir     bool   `json:"isDir"`
	IsSymlink bool   `json:"isSymlink"`
	Size      int64  `json:"size"`
	MTime     int64  `json:"mtime"` // unix milliseconds
	Ext       string `json:"ext"`
	Mode      string `json:"mode"`
}

// Crumb is one step of the breadcrumb navigation.
type Crumb struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// Summary aggregates the filtered listing.
type Summary struct {
	FileCount int   `json:"fileCount"`
	DirCount  int   `json:"dirCount"`
	TotalSize int64 `json:"totalSize"`
}

// ListOptions describes one directory listing request.
type ListOptions struct {
	Path      string
	DirsOnly  bool
	Page      int
	PageSize  int
	Sort      string // name | size | mtime | type
	Order     string // asc | desc
	Name      string
	Exts      []string
	MinSize   int64 // <0 means unset
	MaxSize   int64 // <0 means unset
	MTimeFrom int64 // unix ms, 0 means unset
	MTimeTo   int64 // unix ms, 0 means unset
}

// ListResult is the paginated listing payload.
type ListResult struct {
	Path        string  `json:"path"`
	Parent      string  `json:"parent"`
	Breadcrumbs []Crumb `json:"breadcrumbs"`
	Total       int     `json:"total"`
	Page        int     `json:"page"`
	PageSize    int     `json:"pageSize"`
	Entries     []Entry `json:"entries"`
	Summary     Summary `json:"summary"`
}

// ReadDir lists one directory without recursing and without following
// symbolic links. Directory sizes are intentionally not computed here: any
// heavy IO must be triggered explicitly by the user.
func ReadDir(root, dir string) ([]Entry, []Entry, error) { // returns (dirs, files, err)
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	dirs := make([]Entry, 0, len(des)/4)
	files := make([]Entry, 0, len(des))
	for _, de := range des {
		info, err := de.Info() // lstat: does not follow symlinks
		if err != nil {
			continue
		}
		full := filepath.Join(dir, de.Name())
		e := Entry{
			Name:      de.Name(),
			Path:      full,
			IsSymlink: info.Mode()&os.ModeSymlink != 0,
			Size:      info.Size(),
			MTime:     info.ModTime().UnixMilli(),
			Ext:       strings.ToLower(filepath.Ext(de.Name())),
			Mode:      info.Mode().String(),
		}
		e.IsDir = info.IsDir()
		if e.IsSymlink {
			e.IsDir = false // symlinks are shown as plain entries, never traversed
		}
		if e.IsDir {
			dirs = append(dirs, e)
		} else {
			files = append(files, e)
		}
	}
	return dirs, files, nil
}

// List applies filters, natural sorting and pagination to one directory.
func List(root string, opt ListOptions) (*ListResult, error) {
	dir, err := Resolve(root, opt.Path)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return nil, os.ErrInvalid
	}

	page, size := opt.Page, opt.PageSize
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 200
	}
	if size > 5000 {
		size = 5000
	}

	dirs, files, err := ReadDir(root, dir)
	if err != nil {
		return nil, err
	}

	all := make([]Entry, 0, len(dirs)+len(files))
	for _, e := range dirs {
		if match(e, opt) {
			all = append(all, e)
		}
	}
	for _, e := range files {
		if !opt.DirsOnly && match(e, opt) {
			all = append(all, e)
		}
	}

	sortEntries(all, opt.Sort, opt.Order)

	summary := Summary{}
	for _, e := range all {
		if e.IsDir {
			summary.DirCount++
		} else {
			summary.FileCount++
			summary.TotalSize += e.Size
		}
	}

	total := len(all)
	start := (page - 1) * size
	if start > total {
		start = total
	}
	end := start + size
	if end > total {
		end = total
	}

	return &ListResult{
		Path:        dir,
		Parent:      parentOf(root, dir),
		Breadcrumbs: breadcrumbs(root, dir),
		Total:       total,
		Page:        page,
		PageSize:    size,
		Entries:     all[start:end],
		Summary:     summary,
	}, nil
}

func match(e Entry, o ListOptions) bool {
	if o.Name != "" && !strings.Contains(strings.ToLower(e.Name), strings.ToLower(o.Name)) {
		return false
	}
	if len(o.Exts) > 0 {
		if e.IsDir {
			return false
		}
		ok := false
		for _, ext := range o.Exts {
			ext = strings.ToLower(strings.TrimSpace(ext))
			if ext == "" {
				continue
			}
			if !strings.HasPrefix(ext, ".") {
				ext = "." + ext
			}
			if e.Ext == ext {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if o.MinSize >= 0 || o.MaxSize >= 0 {
		if e.IsDir {
			return false
		}
		if o.MinSize >= 0 && e.Size < o.MinSize {
			return false
		}
		if o.MaxSize >= 0 && e.Size > o.MaxSize {
			return false
		}
	}
	if o.MTimeFrom > 0 && e.MTime < o.MTimeFrom {
		return false
	}
	if o.MTimeTo > 0 && e.MTime > o.MTimeTo {
		return false
	}
	return true
}

func sortEntries(entries []Entry, key, order string) {
	less := func(i, j int) bool {
		a, b := entries[i], entries[j]
		var c int
		switch key {
		case "size":
			c = compareInt(a.Size, b.Size)
		case "mtime":
			c = compareInt(a.MTime, b.MTime)
		case "type":
			c = strings.Compare(typeKey(a), typeKey(b))
		default: // name
			if c = cmpNatural(a.Name, b.Name); c == 0 {
				c = strings.Compare(a.Path, b.Path)
			}
			if c != 0 {
				return c < 0
			}
			return false
		}
		if c != 0 {
			return c < 0
		}
		return NaturalLess(a.Name, b.Name)
	}

	if strings.EqualFold(order, "desc") {
		sort.SliceStable(entries, func(i, j int) bool { return less(j, i) })
	} else {
		sort.SliceStable(entries, less)
	}

	// Directories always stay grouped at the top of the listing.
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].IsDir && !entries[j].IsDir })
}

func cmpNatural(a, b string) int {
	if NaturalLess(a, b) {
		return -1
	}
	if NaturalLess(b, a) {
		return 1
	}
	return 0
}

func compareInt(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func typeKey(e Entry) string {
	if e.IsDir {
		return "d:" + e.Name
	}
	return "f:" + e.Ext
}

func parentOf(root, dir string) string {
	if IsRoot(root, dir) {
		return ""
	}
	return filepath.Dir(dir)
}

func breadcrumbs(root, dir string) []Crumb {
	base := CleanRoot(root)
	crumbs := []Crumb{{Name: filepath.Base(base), Path: base}}
	if dir == base {
		return crumbs
	}
	rel, err := filepath.Rel(base, dir)
	if err != nil {
		return crumbs
	}
	cur := base
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "." || part == "" {
			continue
		}
		cur = filepath.Join(cur, part)
		crumbs = append(crumbs, Crumb{Name: part, Path: cur})
	}
	return crumbs
}
