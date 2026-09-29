package duplicate

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"file-cleaner/internal/task"
)

func TestScanProgressCountsEachFileOnce(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"a.txt":        "same",
		"nested/b.txt": "same",
		"nested/c.txt": "diff",
		"single.txt":   "longer",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	done := make(chan struct{})
	var result *Result
	var scanErr error
	job := task.NewManager(1).Start("duplicate", nil, func(ctx context.Context, job *task.Task) error {
		result, scanErr = Scan(ctx, root, Params{Paths: []string{filepath.Join(root, "nested"), root}, Mode: ModeFast}, job)
		close(done)
		return scanErr
	})
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("scan timed out")
	}
	if scanErr != nil {
		t.Fatal(scanErr)
	}
	p := job.Snapshot().Progress
	if p.Files != 4 || p.Dirs != 2 || p.Bytes != 18 || p.Candidates != 3 || p.Found != 1 || p.ReadBytes != 12 {
		t.Fatalf("unexpected progress: %+v", p)
	}
	if result.TotalGroups != 1 || result.TotalFiles != 2 {
		t.Fatalf("unexpected result: groups=%d files=%d", result.TotalGroups, result.TotalFiles)
	}
}

func TestProbeSignatureReadsAllSmallFileBytes(t *testing.T) {
	root := t.TempDir()
	content := bytes.Repeat([]byte{'x'}, probeChunkSize+1)
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	if err := os.WriteFile(first, content, 0o644); err != nil {
		t.Fatal(err)
	}
	content[len(content)-1] = 'y'
	if err := os.WriteFile(second, content, 0o644); err != nil {
		t.Fatal(err)
	}
	a, readA, err := probeSignature(context.Background(), first, int64(len(content)))
	if err != nil {
		t.Fatal(err)
	}
	b, readB, err := probeSignature(context.Background(), second, int64(len(content)))
	if err != nil {
		t.Fatal(err)
	}
	if a == b || readA != int64(len(content)) || readB != int64(len(content)) {
		t.Fatalf("small-file probe missed trailing content: same=%v readA=%d readB=%d", a == b, readA, readB)
	}
	var reported int64
	_, fullRead, err := fullSignature(context.Background(), first, func(n int64) { reported += n })
	if err != nil || fullRead != int64(len(content)) || reported != fullRead {
		t.Fatalf("full hash read %d bytes, reported %d, want %d (error: %v)", fullRead, reported, len(content), err)
	}
}
