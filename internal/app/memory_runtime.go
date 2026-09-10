package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"trpc.group/trpc-go/trpc-agent-go/memory"
	"trpc.group/trpc-go/trpc-agent-go/memory/extractor"
	memorysqlite "trpc.group/trpc-go/trpc-agent-go/memory/sqlite"
	memorytool "trpc.group/trpc-go/trpc-agent-go/memory/tool"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/session"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

const (
	memoryQueueCapacity = 32
	memoryJobTimeout    = 30 * time.Second
	memoryListLimit     = 1000
)

var blankMindMemoryUser = memory.UserKey{AppName: aguiAppName, UserID: aguiUserID}

type memoryJob struct {
	model    model.Model
	prompt   string
	userText string
	messages []model.Message
}

type memoryRuntime struct {
	store memory.Service

	jobs   chan memoryJob
	stop   chan struct{}
	done   chan struct{}
	mu     sync.RWMutex
	status MemoryStatus
	notify func(string, any)
	cancel context.CancelFunc
	closed bool
}

func newMemoryRuntime() (*memoryRuntime, error) {
	return newMemoryRuntimeAt(filepath.Join(dataDir(), "memory.db"))
}

func newMemoryRuntimeAt(dbPath string) (*memoryRuntime, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open memory store: %w", err)
	}
	db.SetMaxOpenConns(1)
	svc, err := memorysqlite.NewService(db,
		memorysqlite.WithMemoryLimit(memoryListLimit),
		memorysqlite.WithSoftDelete(true),
		memorysqlite.WithToolEnabled(memory.UpdateToolName, true),
		memorysqlite.WithToolEnabled(memory.DeleteToolName, false),
		memorysqlite.WithToolEnabled(memory.ClearToolName, false),
		memorysqlite.WithToolEnabled(memory.LoadToolName, false),
	)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize memory store: %w", err)
	}
	r := &memoryRuntime{
		store:  svc,
		jobs:   make(chan memoryJob, memoryQueueCapacity),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
		status: MemoryStatus{State: "idle"},
	}
	go r.worker()
	return r, nil
}

func (r *memoryRuntime) setNotify(fn func(string, any)) {
	r.mu.Lock()
	r.notify = fn
	r.mu.Unlock()
}

func (r *memoryRuntime) emit(name string, value any) {
	r.mu.RLock()
	fn := r.notify
	r.mu.RUnlock()
	if fn != nil {
		fn(name, value)
	}
}

func (r *memoryRuntime) snapshotStatus() MemoryStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.status
}

func (r *memoryRuntime) updateStatus(state, lastError string, success bool) {
	r.mu.Lock()
	r.status.State = state
	r.status.PendingJobs = len(r.jobs)
	if success {
		r.status.LastSuccessAt = time.Now().Format(time.RFC3339)
		r.status.LastError = ""
	} else if lastError != "" {
		r.status.LastError = lastError
	}
	status := r.status
	r.mu.Unlock()
	r.emit("memory.status", status)
}

func (r *memoryRuntime) enqueue(modelInstance model.Model, cfg MemoryConfig, userText, answer string) error {
	if !cfg.Enabled || !cfg.AutoExtract {
		return nil
	}
	prompt, err := memoryPrompt(cfg.Strategy, cfg.CustomPrompt)
	if err != nil {
		return err
	}
	job := memoryJob{
		model:    modelInstance,
		prompt:   prompt,
		userText: userText,
		messages: []model.Message{
			model.NewUserMessage(userText),
			model.NewAssistantMessage(answer),
		},
	}
	r.mu.RLock()
	if r.closed {
		r.mu.RUnlock()
		return errors.New("记忆服务已关闭")
	}
	queued := false
	select {
	case r.jobs <- job:
		queued = true
	default:
	}
	r.mu.RUnlock()
	if queued {
		r.updateStatus(r.snapshotStatus().State, "", false)
		return nil
	}
	err = errors.New("记忆提取队列已满，本轮未保存")
	r.updateStatus("error", err.Error(), false)
	return err
}

func (r *memoryRuntime) worker() {
	defer close(r.done)
	for {
		select {
		case <-r.stop:
			return
		default:
		}
		select {
		case <-r.stop:
			return
		case job := <-r.jobs:
			select {
			case <-r.stop:
				return
			default:
			}
			r.extract(job)
		}
	}
}

type enabledToolsSetter interface {
	SetEnabledTools(map[string]struct{})
}

func (r *memoryRuntime) extract(job memoryJob) {
	ctx, cancel := context.WithTimeout(context.Background(), memoryJobTimeout)
	r.mu.Lock()
	r.cancel = cancel
	r.mu.Unlock()
	r.updateStatus("extracting", "", false)
	defer func() {
		cancel()
		r.mu.Lock()
		r.cancel = nil
		closed := r.closed
		r.mu.Unlock()
		if !closed && r.snapshotStatus().State != "error" {
			r.updateStatus("idle", "", false)
		}
	}()

	existing, err := r.store.SearchMemories(ctx, blankMindMemoryUser, job.userText,
		memory.WithSearchOptions(memory.SearchOptions{Query: job.userText, MaxResults: 20, Deduplicate: true}))
	if err != nil {
		r.updateStatus("error", err.Error(), false)
		return
	}
	ext := extractor.NewExtractor(job.model, extractor.WithPrompt(job.prompt))
	if setter, ok := ext.(enabledToolsSetter); ok {
		setter.SetEnabledTools(map[string]struct{}{
			memory.AddToolName:    {},
			memory.UpdateToolName: {},
		})
	}
	ops, err := ext.Extract(extractor.WithReferenceDate(ctx, time.Now()), job.messages, existing)
	if err != nil {
		r.updateStatus("error", err.Error(), false)
		return
	}
	changed := false
	for _, op := range ops {
		if op == nil {
			continue
		}
		metadata := &memory.Metadata{Kind: op.MemoryKind, EventTime: op.EventTime, Participants: op.Participants, Location: op.Location}
		switch op.Type {
		case extractor.OperationAdd:
			err = r.store.AddMemory(ctx, blankMindMemoryUser, strings.TrimSpace(op.Memory), op.Topics, memory.WithMetadata(metadata))
		case extractor.OperationUpdate:
			result := &memory.UpdateResult{MemoryID: op.MemoryID}
			err = r.store.UpdateMemory(ctx, memory.Key{AppName: aguiAppName, UserID: aguiUserID, MemoryID: op.MemoryID},
				strings.TrimSpace(op.Memory), op.Topics, memory.WithUpdateMetadata(metadata), memory.WithUpdateResult(result))
		default:
			continue
		}
		if err != nil {
			r.updateStatus("error", err.Error(), false)
			return
		}
		changed = true
	}
	if changed {
		r.emit("memory.changed", "extracted")
	}
	r.updateStatus("idle", "", true)
}

func (r *memoryRuntime) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	if r.cancel != nil {
		r.cancel()
	}
	close(r.stop)
	r.mu.Unlock()
	<-r.done
	for {
		select {
		case <-r.jobs:
		default:
			return r.store.Close()
		}
	}
}

// memory.Service implementation used by Runner. Only add/search tools are exposed.
func (r *memoryRuntime) ReadMemories(ctx context.Context, key memory.UserKey, limit int) ([]*memory.Entry, error) {
	return r.store.ReadMemories(ctx, key, limit)
}
func (r *memoryRuntime) SearchMemories(ctx context.Context, key memory.UserKey, query string, opts ...memory.SearchOption) ([]*memory.Entry, error) {
	return r.store.SearchMemories(ctx, key, query, opts...)
}
func (r *memoryRuntime) AddMemory(ctx context.Context, key memory.UserKey, value string, topics []string, opts ...memory.AddOption) error {
	if err := r.store.AddMemory(ctx, key, value, topics, opts...); err != nil {
		return err
	}
	r.emit("memory.changed", "added")
	return nil
}
func (r *memoryRuntime) UpdateMemory(ctx context.Context, key memory.Key, value string, topics []string, opts ...memory.UpdateOption) error {
	if err := r.store.UpdateMemory(ctx, key, value, topics, opts...); err != nil {
		return err
	}
	r.emit("memory.changed", "updated")
	return nil
}
func (r *memoryRuntime) DeleteMemory(ctx context.Context, key memory.Key) error {
	return r.store.DeleteMemory(ctx, key)
}
func (r *memoryRuntime) ClearMemories(ctx context.Context, key memory.UserKey) error {
	return r.store.ClearMemories(ctx, key)
}
func (r *memoryRuntime) Tools() []tool.Tool {
	return []tool.Tool{memorytool.NewSearchTool(), memorytool.NewAddTool()}
}
func (r *memoryRuntime) EnqueueAutoMemoryJob(context.Context, *session.Session) error { return nil }
