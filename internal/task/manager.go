// Package task implements the background scan task queue used by every
// long running operation (cleanup scans, duplicate scans, disk usage scans).
//
// Tasks run inside the Go process with a cancellable context. They never
// block an HTTP request and they stream progress to subscribers (SSE).
package task

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"sync"
	"time"
)

// Status is the lifecycle state of a task.
type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusCanceled  Status = "canceled"
	StatusFailed    Status = "failed"
)

// ErrCanceled is returned by a runner that stopped because of cancellation.
var ErrCanceled = errors.New("task canceled")

// Progress is the live counter block exposed to the UI.
type Progress struct {
	Files      int64   `json:"files"`
	Dirs       int64   `json:"dirs"`
	Bytes      int64   `json:"bytes"`      // sum of file sizes seen in the directory walk
	ReadBytes  int64   `json:"readBytes"`  // content bytes read for verification
	Candidates int64   `json:"candidates"` // duplicate candidate files
	Found      int64   `json:"found"`
	Total      int64   `json:"total"`
	Current    string  `json:"current"`
	Phase      string  `json:"phase"`
	Elapsed    int64   `json:"elapsedMs"`
	Speed      float64 `json:"speed"`     // average content bytes read per second
	FileSpeed  float64 `json:"fileSpeed"` // average unique files found per second
	Percent    float64 `json:"percent"`   // 0..100, -1 when unknown
	Started    int64   `json:"startedAt"`
}

// Event is one SSE payload.
type Event struct {
	Type string `json:"type"` // snapshot | progress | done | error
	Data any    `json:"data"`
}

// Snapshot is the JSON view of a task.
type Snapshot struct {
	ID          string         `json:"id"`
	Type        string         `json:"type"`
	Status      Status         `json:"status"`
	Params      map[string]any `json:"params"`
	Progress    Progress       `json:"progress"`
	Error       string         `json:"error,omitempty"`
	CreatedAt   int64          `json:"createdAt"`
	StartedAt   int64          `json:"startedAt"`
	FinishedAt  int64          `json:"finishedAt"`
	ResultCount int            `json:"resultCount"`
	Summary     any            `json:"summary,omitempty"`
}

// Runner is the function executed in the background.
type Runner func(ctx context.Context, t *Task) error

// Task is a single background job.
type Task struct {
	ID     string
	Type   string
	Params map[string]any

	mgr *Manager

	mu         sync.RWMutex
	status     Status
	progress   Progress
	total      int64
	errMsg     string
	createdAt  time.Time
	startedAt  time.Time
	finishedAt time.Time
	result     any
	summary    any
	subs       map[chan Event]struct{}
	lastPush   time.Time

	ctx    context.Context
	cancel context.CancelFunc
}

// Manager owns all tasks of the process.
type Manager struct {
	mu    sync.RWMutex
	tasks map[string]*Task
	order []string
	max   int
}

// NewManager builds a manager that keeps at most maxTasks finished tasks.
func NewManager(maxTasks int) *Manager {
	if maxTasks <= 0 {
		maxTasks = 50
	}
	return &Manager{tasks: map[string]*Task{}, max: maxTasks}
}

// Start registers a task and runs runner in a fresh goroutine.
func (m *Manager) Start(kind string, params map[string]any, runner Runner) *Task {
	ctx, cancel := context.WithCancel(context.Background())
	t := &Task{
		ID:        newID(),
		Type:      kind,
		Params:    params,
		mgr:       m,
		status:    StatusPending,
		createdAt: time.Now(),
		subs:      map[chan Event]struct{}{},
		ctx:       ctx,
		cancel:    cancel,
		progress:  Progress{Percent: -1},
	}
	if params == nil {
		t.Params = map[string]any{}
	}

	m.mu.Lock()
	m.tasks[t.ID] = t
	m.order = append(m.order, t.ID)
	m.evictLocked()
	m.mu.Unlock()

	go func() {
		t.run(runner)
	}()
	return t
}

// Get returns a task by id.
func (m *Manager) Get(id string) (*Task, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.tasks[id]
	return t, ok
}

// List returns snapshots, newest first.
func (m *Manager) List() []Snapshot {
	m.mu.RLock()
	list := make([]*Task, 0, len(m.order))
	for _, id := range m.order {
		if t, ok := m.tasks[id]; ok {
			list = append(list, t)
		}
	}
	m.mu.RUnlock()

	out := make([]Snapshot, 0, len(list))
	for _, t := range list {
		out = append(out, t.Snapshot())
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out
}

// Cancel requests cancellation of a running task.
func (m *Manager) Cancel(id string) error {
	t, ok := m.Get(id)
	if !ok {
		return errors.New("task not found")
	}
	t.cancel()
	return nil
}

// Remove drops a finished task from the registry.
func (m *Manager) Remove(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[id]; ok && t.isFinished() {
		delete(m.tasks, id)
		for i, oid := range m.order {
			if oid == id {
				m.order = append(m.order[:i], m.order[i+1:]...)
				break
			}
		}
	}
}

func (m *Manager) evictLocked() {
	for len(m.order) > m.max {
		dropped := false
		for i, id := range m.order {
			if t, ok := m.tasks[id]; ok && t.isFinished() {
				delete(m.tasks, id)
				m.order = append(m.order[:i], m.order[i+1:]...)
				dropped = true
				break
			}
		}
		if !dropped {
			break // every remaining task is still active
		}
	}
}

func (t *Task) run(runner Runner) {
	t.setStatus(StatusRunning)
	t.mu.Lock()
	t.startedAt = time.Now()
	t.progress.Started = t.startedAt.UnixMilli()
	t.mu.Unlock()
	t.publish("snapshot", t.Snapshot())

	err := runner(t.ctx, t)

	t.mu.Lock()
	t.finishedAt = time.Now()
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, ErrCanceled):
		t.status = StatusCanceled
	case err != nil:
		t.status = StatusFailed
		t.errMsg = err.Error()
	default:
		t.status = StatusCompleted
	}
	snap := t.snapshotLocked()
	t.mu.Unlock()

	if snap.Status == StatusCanceled || snap.Status == StatusFailed {
		t.publish("failed", map[string]any{"status": snap.Status, "error": snap.Error})
	}
	t.publish("done", snap)
	t.closeSubs()
}

// Snapshot returns a consistent view of the task.
func (t *Task) Snapshot() Snapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.snapshotLocked()
}

func (t *Task) snapshotLocked() Snapshot {
	p := t.progress
	if !t.startedAt.IsZero() {
		end := t.finishedAt
		if end.IsZero() {
			end = time.Now()
		}
		p.Elapsed = end.Sub(t.startedAt).Milliseconds()
		if p.Elapsed > 0 {
			seconds := float64(p.Elapsed) / 1000.0
			p.Speed = float64(p.ReadBytes) / seconds
			p.FileSpeed = float64(p.Files) / seconds
		}
	}
	if t.total > 0 {
		p.Percent = float64(p.Bytes) / float64(t.total) * 100
		if p.Percent > 100 {
			p.Percent = 100
		}
	}
	if t.status == StatusCompleted {
		p.Percent = 100
	}
	return Snapshot{
		ID:          t.ID,
		Type:        t.Type,
		Status:      t.status,
		Params:      t.Params,
		Progress:    p,
		Error:       t.errMsg,
		CreatedAt:   t.createdAt.UnixMilli(),
		StartedAt:   t.startedAt.UnixMilli(),
		FinishedAt:  t.finishedAt.UnixMilli(),
		ResultCount: resultLen(t.result),
		Summary:     t.summary,
	}
}

// Ctx exposes the task context so runners can honor cancellation.
func (t *Task) Ctx() context.Context { return t.ctx }

// Canceled reports whether cancellation was requested.
func (t *Task) Canceled() bool { return t.ctx.Err() != nil }

// CheckCancel returns ctx.Err() so walkers can stop early.
func (t *Task) CheckCancel() error { return t.ctx.Err() }

// Pruner is implemented by scan results whose entries can be dropped after a
// successful delete, so a refreshed view no longer lists removed files.
type Pruner interface {
	// Prune drops the given absolute paths and returns how many were removed.
	Prune(paths []string) int
	// Summary returns the (re)computed stats shown in task listings.
	Summary() map[string]any
}

// SetResult stores the typed scan payload served by the API.
func (t *Task) SetResult(v any) {
	t.mu.Lock()
	t.result = v
	t.mu.Unlock()
}

// Result returns the stored payload.
func (t *Task) Result() any {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.result
}

// Prune removes deleted paths from the stored scan result, if the result
// supports it, and refreshes the summary. It returns the number of removed
// entries. Subscribers are notified so open pages update their counters.
func (t *Task) Prune(paths []string) int {
	res := t.Result()
	p, ok := res.(Pruner)
	if !ok || len(paths) == 0 {
		return 0
	}
	n := p.Prune(paths)
	if n == 0 {
		return 0
	}
	t.SetSummary(p.Summary())
	t.publish("progress", t.Snapshot())
	return n
}

// SetSummary stores the small summary shown in task lists.
func (t *Task) SetSummary(v any) {
	t.mu.Lock()
	t.summary = v
	t.mu.Unlock()
}

// SetTotal sets the expected byte total (enables the percent bar).
func (t *Task) SetTotal(n int64) {
	t.mu.Lock()
	t.total = n
	t.progress.Total = n
	t.mu.Unlock()
	t.publish("progress", t.Snapshot())
}

// AddFiles increments the scanned file counter.
func (t *Task) AddFiles(n int64) { t.bump(func(p *Progress) { p.Files += n }) }

// AddDirs increments the scanned directory counter.
func (t *Task) AddDirs(n int64) { t.bump(func(p *Progress) { p.Dirs += n }) }

// AddBytes increments the scanned byte counter.
func (t *Task) AddBytes(n int64) { t.bump(func(p *Progress) { p.Bytes += n }) }

// AddReadBytes records bytes actually read from file content during verification.
func (t *Task) AddReadBytes(n int64) { t.bump(func(p *Progress) { p.ReadBytes += n }) }

// IncCandidates counts files that share a size with at least one other file.
func (t *Task) IncCandidates(n int64) { t.bump(func(p *Progress) { p.Candidates += n }) }

// IncFound increments the "results found so far" counter.
func (t *Task) IncFound(n int64) { t.bump(func(p *Progress) { p.Found += n }) }

// SetPhase names the current stage of a multi-pass scan.
func (t *Task) SetPhase(phase string) { t.bump(func(p *Progress) { p.Phase = phase }) }

// SetCurrent records the path currently being processed.
func (t *Task) SetCurrent(path string) {
	t.mu.Lock()
	t.progress.Current = path
	t.mu.Unlock()
	t.publishThrottled()
}

func (t *Task) bump(fn func(*Progress)) {
	t.mu.Lock()
	fn(&t.progress)
	t.mu.Unlock()
	t.publishThrottled()
}

func (t *Task) setStatus(s Status) {
	t.mu.Lock()
	t.status = s
	t.mu.Unlock()
}

func (t *Task) isFinished() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.status == StatusCompleted || t.status == StatusCanceled || t.status == StatusFailed
}

// Subscribe registers an SSE subscriber and returns the channel plus a
// cancel function. The first value is always a snapshot of current state.
func (t *Task) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 64)
	t.mu.Lock()
	t.subs[ch] = struct{}{}
	snap := t.snapshotLocked()
	done := t.isFinishedLocked()
	t.mu.Unlock()

	ch <- Event{Type: "snapshot", Data: snap}
	if done {
		ch <- Event{Type: "done", Data: snap}
	}

	unsub := func() {
		// Only detach: closing the channel here could race with a publish
		// that already snapshotted the subscriber list.
		t.mu.Lock()
		delete(t.subs, ch)
		t.mu.Unlock()
	}
	return ch, unsub
}

func (t *Task) isFinishedLocked() bool {
	return t.status == StatusCompleted || t.status == StatusCanceled || t.status == StatusFailed
}

func (t *Task) publish(kind string, data any) {
	t.mu.Lock()
	subs := make([]chan Event, 0, len(t.subs))
	for ch := range t.subs {
		subs = append(subs, ch)
	}
	t.mu.Unlock()

	ev := Event{Type: kind, Data: data}
	for _, ch := range subs {
		select {
		case ch <- ev:
		default: // slow consumer: drop the frame rather than blocking the scan
		}
	}
}

func (t *Task) publishThrottled() {
	t.mu.Lock()
	if time.Since(t.lastPush) < 200*time.Millisecond {
		t.mu.Unlock()
		return
	}
	t.lastPush = time.Now()
	t.mu.Unlock()
	t.publish("progress", t.Snapshot())
}

func (t *Task) closeSubs() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for ch := range t.subs {
		delete(t.subs, ch)
		close(ch)
	}
}

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(b)
}

func resultLen(v any) int {
	switch r := v.(type) {
	case nil:
		return 0
	case interface{ Len() int }:
		return r.Len()
	case interface{ Count() int }:
		return r.Count()
	}
	return 0
}
