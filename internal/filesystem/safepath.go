package filesystem

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// rootCache memoizes CleanRoot. Delete and preview calls resolve one path per
// selected entry, and re-resolving the root (with its symlink walk) for every
// one of them dominated large batches.
var rootCache sync.Map // string -> string

// ErrOutsideRoot is returned when a requested path escapes the allowed root.
var ErrOutsideRoot = errors.New("path is outside the allowed root")

// CleanRoot normalizes the configured root directory into an absolute,
// symlink-free prefix used for all containment checks.
func CleanRoot(root string) string {
	if root == "" {
		root = string(filepath.Separator)
	}
	if v, ok := rootCache.Load(root); ok {
		return v.(string)
	}

	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	abs = filepath.Clean(abs)
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	rootCache.Store(root, abs)
	return abs
}

// Resolve turns a user supplied path into a safe absolute path that is
// guaranteed to live inside root. Relative input is interpreted as
// relative to root. Any traversal attempt (.., absolute escapes, symlinked
// escapes) is rejected with ErrOutsideRoot.
func Resolve(root, target string) (string, error) {
	base := CleanRoot(root)
	if strings.TrimSpace(target) == "" {
		return base, nil
	}

	p := filepath.FromSlash(strings.TrimSpace(target))
	if !filepath.IsAbs(p) {
		p = filepath.Join(base, p)
	}
	clean := filepath.Clean(p)

	// CleanRoot always yields an absolute path, so the join above is already
	// absolute: skip filepath.Abs (and its per-call os.Getwd) entirely. This
	// matters when a delete batch resolves tens of thousands of paths.
	if !filepath.IsAbs(clean) {
		abs, err := filepath.Abs(clean)
		if err != nil {
			return "", err
		}
		clean = filepath.Clean(abs)
	}

	if !within(base, clean) {
		return "", ErrOutsideRoot
	}

	// Reject paths whose symlink chain leaves the root (e.g. /mnt/evil -> /etc).
	// The Lstat guard keeps batches of (mostly missing) paths cheap: a
	// non-existent target cannot resolve outside the root anyway.
	if _, err := os.Lstat(clean); err == nil {
		if resolved, err := filepath.EvalSymlinks(clean); err == nil {
			if !within(base, filepath.Clean(resolved)) {
				return "", ErrOutsideRoot
			}
		}
	}
	return clean, nil
}

// EnsureDir resolves p (inside root) and verifies it is an existing
// directory, so scans fail loudly instead of silently returning nothing.
func EnsureDir(root, p string) (string, error) {
	dir, err := Resolve(root, p)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("目录不存在或无法访问: %s", dir)
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("不是目录: %s", dir)
	}
	return dir, nil
}

// IsRoot reports whether p is exactly the allowed root.
func IsRoot(root, p string) bool {
	base := CleanRoot(root)
	abs, err := filepath.Abs(p)
	if err != nil {
		return false
	}
	return filepath.Clean(abs) == base
}

func within(base, p string) bool {
	if p == base {
		return true
	}
	base = strings.TrimSuffix(base, string(filepath.Separator))
	return strings.HasPrefix(p, base+string(filepath.Separator))
}
