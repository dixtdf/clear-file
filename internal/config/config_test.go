package config

import (
	"path/filepath"
	"testing"

	"file-cleaner/internal/filesystem"
)

func TestRootDefaultsToFilesystemRootAndCanBeOverridden(t *testing.T) {
	t.Setenv("FILE_CLEANER_ROOT", "")
	if got, want := Load().Root, filesystem.CleanRoot(string(filepath.Separator)); got != want {
		t.Fatalf("default root = %q, want %q", got, want)
	}

	custom := t.TempDir()
	t.Setenv("FILE_CLEANER_ROOT", custom)
	if got := Load().Root; got != filesystem.CleanRoot(custom) {
		t.Fatalf("custom root = %q, want %q", got, custom)
	}
}
