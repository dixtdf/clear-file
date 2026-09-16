package filesystem

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
)

// WalkFunc is called for every visited directory entry.
// Returning filepath.SkipDir for a directory skips its subtree; any other
// non-nil error aborts the walk.
type WalkFunc func(path string, d fs.DirEntry, err error) error

// Walk streams a directory tree using lstat semantics: symbolic links are
// reported but never followed, which prevents symlink loops.
//
// The traversal is streaming on purpose - it never materializes the whole
// tree in memory, so million-entry directories stay feasible. The caller
// must stop on ctx cancellation by returning ctx.Err() from fn.
func Walk(ctx context.Context, root string, fn WalkFunc) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fn(path, d, err)
	})
}

// DirSize returns the recursive size of a directory without following
// symbolic links. It is only ever called from explicit user-triggered scans.
func DirSize(ctx context.Context, path string) (int64, int64, error) {
	var size, count int64
	err := Walk(ctx, path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable subtree: skip instead of failing the scan
		}
		if d.IsDir() || d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if info, err := d.Info(); err == nil {
			size += info.Size()
			count++
		}
		return nil
	})
	return size, count, err
}
