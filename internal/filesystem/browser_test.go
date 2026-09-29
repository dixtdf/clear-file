package filesystem

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListDirectoriesOnlyPaginatesDirectories(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"alpha", "beta"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "between.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	for page, want := range []string{"alpha", "beta"} {
		res, err := List(root, ListOptions{Path: root, DirsOnly: true, Page: page + 1, PageSize: 1, MinSize: -1, MaxSize: -1})
		if err != nil {
			t.Fatal(err)
		}
		if res.Total != 2 || len(res.Entries) != 1 || !res.Entries[0].IsDir || res.Entries[0].Name != want {
			t.Fatalf("page %d: got total=%d entries=%+v", page+1, res.Total, res.Entries)
		}
	}
}
