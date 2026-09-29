package task

import (
	"context"
	"testing"
	"time"
)

func TestSpeedUsesContentReadsNotFileSizes(t *testing.T) {
	ready := make(chan struct{})
	release := make(chan struct{})
	job := NewManager(1).Start("duplicate", nil, func(_ context.Context, job *Task) error {
		job.AddFiles(4)
		job.AddBytes(1 << 40)
		job.AddReadBytes(2048)
		close(ready)
		<-release
		return nil
	})
	defer close(release)
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("task did not start")
	}
	time.Sleep(20 * time.Millisecond)
	p := job.Snapshot().Progress
	if p.Elapsed <= 0 || p.Speed <= 0 || p.Speed >= 1<<20 || p.FileSpeed <= 0 {
		t.Fatalf("speed should use 2048 read bytes and 4 files, not 1 TiB of file sizes: %+v", p)
	}
}
