package app

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	memoryinmemory "trpc.group/trpc-go/trpc-agent-go/memory/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

func newMemoryServiceForTest(t *testing.T) (*MemoryService, *SettingsService) {
	t.Helper()
	settings := NewSettingsService(newSettingsTestDB(t))
	runtime := &memoryRuntime{
		store:  memoryinmemory.NewMemoryService(),
		status: MemoryStatus{State: "idle"},
	}
	t.Cleanup(func() { _ = runtime.store.Close() })
	return NewMemoryService(runtime, settings), settings
}

func TestMemoryConfigDefaultsAndPersistence(t *testing.T) {
	service, settings := newMemoryServiceForTest(t)
	view, err := service.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if !view.Config.Enabled || !view.Config.AutoExtract || view.Config.Strategy != MemoryStrategyBalanced {
		t.Fatalf("default config = %#v", view.Config)
	}

	custom := "  Remember only durable project decisions. {current_date}  "
	view, err = service.SaveSettings(MemoryConfig{Enabled: true, AutoExtract: false, Strategy: MemoryStrategyCustom, CustomPrompt: custom})
	if err != nil {
		t.Fatal(err)
	}
	if view.Config.CustomPrompt != strings.TrimSpace(custom) || view.EffectivePrompt != strings.TrimSpace(custom) {
		t.Fatalf("saved custom prompt = %#v", view.Config)
	}
	raw, err := settings.GetSetting(SettingMemoryConfig)
	if err != nil {
		t.Fatal(err)
	}
	var persisted MemoryConfig
	if err := json.Unmarshal([]byte(raw), &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Strategy != MemoryStrategyCustom || persisted.AutoExtract {
		t.Fatalf("persisted config = %#v", persisted)
	}
}

func TestMemoryConfigValidationAndStrategyPrompts(t *testing.T) {
	if _, err := normalizeMemoryConfig(MemoryConfig{Strategy: MemoryStrategyCustom}); err == nil {
		t.Fatal("empty custom prompt should fail")
	}
	tooLong := strings.Repeat("a", maxCustomMemoryPromptRunes+1)
	if _, err := normalizeMemoryConfig(MemoryConfig{Strategy: MemoryStrategyCustom, CustomPrompt: tooLong}); err == nil {
		t.Fatal("oversized custom prompt should fail")
	}
	for _, strategy := range []string{MemoryStrategyExplicit, MemoryStrategyBalanced, MemoryStrategyComprehensive} {
		prompt, err := memoryPrompt(strategy, "preserved")
		if err != nil {
			t.Fatal(err)
		}
		for _, boundary := range []string{"tool output", "credentials", "model guesses"} {
			if !strings.Contains(prompt, boundary) {
				t.Fatalf("%s prompt does not exclude %q", strategy, boundary)
			}
		}
	}
	custom, err := memoryPrompt(MemoryStrategyCustom, "custom {current_date}")
	if err != nil || custom != "custom {current_date}" {
		t.Fatalf("custom replacement = %q, %v", custom, err)
	}
}

func TestMemoryCRUDAndExportShape(t *testing.T) {
	service, _ := newMemoryServiceForTest(t)
	created, err := service.AddMemory(MemoryInput{Content: "用户偏好简洁回答", Topics: []string{" 偏好 "}, Kind: "fact"})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Content != "用户偏好简洁回答" || len(created.Topics) != 1 {
		t.Fatalf("created = %#v", created)
	}
	updated, err := service.UpdateMemory(MemoryUpdateInput{ID: created.ID, MemoryInput: MemoryInput{Content: "用户偏好简洁且直接的回答", Topics: []string{"偏好"}, Kind: "fact"}})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Content != "用户偏好简洁且直接的回答" {
		t.Fatalf("updated = %#v", updated)
	}
	items, err := service.ListMemories()
	if err != nil || len(items) != 1 {
		t.Fatalf("list = %#v, %v", items, err)
	}
	if err := service.DeleteMemory(updated.ID); err != nil {
		t.Fatal(err)
	}
	items, err = service.ListMemories()
	if err != nil || len(items) != 0 {
		t.Fatalf("list after delete = %#v, %v", items, err)
	}
}

func TestEpisodeRequiresEventTime(t *testing.T) {
	service, _ := newMemoryServiceForTest(t)
	if _, err := service.AddMemory(MemoryInput{Content: "用户参加了会议", Kind: "episode"}); err == nil {
		t.Fatal("episode without event time should fail")
	}
}

func TestSQLiteMemoryPersistsAcrossRuntimeRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	first, err := newMemoryRuntimeAt(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.AddMemory(context.Background(), blankMindMemoryUser, "用户使用 Go 开发桌面应用", []string{"工作"}); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := newMemoryRuntimeAt(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	entries, err := second.ReadMemories(context.Background(), blankMindMemoryUser, memoryListLimit)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Memory.Memory != "用户使用 Go 开发桌面应用" {
		t.Fatalf("persisted entries = %#v", entries)
	}
}

type failingMemoryModel struct{}

func (failingMemoryModel) GenerateContent(context.Context, *model.Request) (<-chan *model.Response, error) {
	return nil, errors.New("extractor unavailable")
}

func (failingMemoryModel) Info() model.Info { return model.Info{Name: "failing"} }

type scriptedMemoryModel struct {
	calls []model.ToolCall
}

func (m scriptedMemoryModel) GenerateContent(context.Context, *model.Request) (<-chan *model.Response, error) {
	responses := make(chan *model.Response, 1)
	responses <- &model.Response{Choices: []model.Choice{{Message: model.Message{ToolCalls: m.calls}}}}
	close(responses)
	return responses, nil
}

func (scriptedMemoryModel) Info() model.Info { return model.Info{Name: "scripted"} }

type capturingMemoryModel struct {
	request *model.Request
}

func (m *capturingMemoryModel) GenerateContent(_ context.Context, request *model.Request) (<-chan *model.Response, error) {
	m.request = request
	responses := make(chan *model.Response)
	close(responses)
	return responses, nil
}

func (*capturingMemoryModel) Info() model.Info { return model.Info{Name: "capturing"} }

func memoryCall(name, args string) model.ToolCall {
	return model.ToolCall{Type: "function", Function: model.FunctionDefinitionParam{Name: name, Arguments: []byte(args)}}
}

func TestMemoryExtractionAppliesOnlyAddAndUpdate(t *testing.T) {
	runtime := &memoryRuntime{store: memoryinmemory.NewMemoryService(), status: MemoryStatus{State: "idle"}}
	t.Cleanup(func() { _ = runtime.store.Close() })
	if err := runtime.store.AddMemory(context.Background(), blankMindMemoryUser, "用户偏好长回答", []string{"偏好"}); err != nil {
		t.Fatal(err)
	}
	entries, err := runtime.store.ReadMemories(context.Background(), blankMindMemoryUser, memoryListLimit)
	if err != nil || len(entries) != 1 {
		t.Fatalf("seed memories = %#v, %v", entries, err)
	}
	id := entries[0].ID
	modelInstance := scriptedMemoryModel{calls: []model.ToolCall{
		memoryCall("memory_update", `{"memory_id":"`+id+`","memory":"用户偏好简洁回答","topics":["偏好"],"memory_kind":"fact"}`),
		memoryCall("memory_add", `{"memory":"用户主要使用 Go","topics":["技术栈"],"memory_kind":"fact"}`),
		memoryCall("memory_delete", `{"memory_id":"`+id+`"}`),
		memoryCall("memory_clear", `{}`),
	}}
	runtime.extract(memoryJob{
		model:    modelInstance,
		prompt:   balancedMemoryPrompt,
		userText: "我更喜欢简洁回答，主要使用 Go",
		messages: []model.Message{model.NewUserMessage("我更喜欢简洁回答，主要使用 Go"), model.NewAssistantMessage("明白")},
	})
	entries, err = runtime.store.ReadMemories(context.Background(), blankMindMemoryUser, memoryListLimit)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries after extraction = %#v", entries)
	}
	contents := map[string]bool{}
	for _, entry := range entries {
		contents[entry.Memory.Memory] = true
	}
	if !contents["用户偏好简洁回答"] || !contents["用户主要使用 Go"] {
		t.Fatalf("extracted contents = %#v", contents)
	}
}

func TestMemoryExtractionRendersDateAndRestrictsActions(t *testing.T) {
	modelInstance := &capturingMemoryModel{}
	runtime := &memoryRuntime{store: memoryinmemory.NewMemoryService(), status: MemoryStatus{State: "idle"}}
	t.Cleanup(func() { _ = runtime.store.Close() })
	runtime.extract(memoryJob{
		model:    modelInstance,
		prompt:   "Reference date: {current_date}",
		userText: "hello",
		messages: []model.Message{model.NewUserMessage("hello"), model.NewAssistantMessage("hi")},
	})
	if modelInstance.request == nil || len(modelInstance.request.Messages) == 0 {
		t.Fatal("extractor did not call the captured model")
	}
	systemPrompt := modelInstance.request.Messages[0].Content
	if strings.Contains(systemPrompt, "{current_date}") || !strings.Contains(systemPrompt, time.Now().UTC().Format(time.DateOnly)) {
		t.Fatalf("rendered prompt = %q", systemPrompt)
	}
	if _, ok := modelInstance.request.Tools["memory_add"]; !ok {
		t.Fatal("memory_add action is missing")
	}
	if _, ok := modelInstance.request.Tools["memory_update"]; !ok {
		t.Fatal("memory_update action is missing")
	}
	if _, ok := modelInstance.request.Tools["memory_delete"]; ok {
		t.Fatal("memory_delete must not be exposed to extraction")
	}
	if _, ok := modelInstance.request.Tools["memory_clear"]; ok {
		t.Fatal("memory_clear must not be exposed to extraction")
	}
}

func TestMemoryExtractionFailureIsAsynchronous(t *testing.T) {
	runtime := &memoryRuntime{
		store:  memoryinmemory.NewMemoryService(),
		jobs:   make(chan memoryJob, 1),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
		status: MemoryStatus{State: "idle"},
	}
	go runtime.worker()
	t.Cleanup(func() { _ = runtime.Close() })
	cfg := defaultMemoryConfig()
	if err := runtime.enqueue(failingMemoryModel{}, cfg, "请记住我喜欢简洁回答", "好的"); err != nil {
		t.Fatalf("enqueue should not expose extraction failure: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status := runtime.snapshotStatus()
		if status.State == "error" {
			if !strings.Contains(status.LastError, "extractor unavailable") {
				t.Fatalf("last error = %q", status.LastError)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("memory runtime did not publish the extraction error")
}

func TestMemoryEnqueueHonorsSwitchesAndCapacity(t *testing.T) {
	runtime := &memoryRuntime{jobs: make(chan memoryJob, 1), status: MemoryStatus{State: "idle"}}
	disabled := defaultMemoryConfig()
	disabled.Enabled = false
	if err := runtime.enqueue(nil, disabled, "user", "assistant"); err != nil || len(runtime.jobs) != 0 {
		t.Fatalf("disabled enqueue = %v, jobs = %d", err, len(runtime.jobs))
	}
	disabled.Enabled = true
	disabled.AutoExtract = false
	if err := runtime.enqueue(nil, disabled, "user", "assistant"); err != nil || len(runtime.jobs) != 0 {
		t.Fatalf("auto disabled enqueue = %v, jobs = %d", err, len(runtime.jobs))
	}
	enabled := defaultMemoryConfig()
	if err := runtime.enqueue(nil, enabled, "user", "assistant"); err != nil {
		t.Fatal(err)
	}
	if err := runtime.enqueue(nil, enabled, "user 2", "assistant 2"); err == nil {
		t.Fatal("full queue should reject the second job")
	}
}

func TestMemoryEnqueueRejectsJobsAfterCloseStarts(t *testing.T) {
	runtime := &memoryRuntime{
		jobs:   make(chan memoryJob, 1),
		closed: true,
		status: MemoryStatus{State: "idle"},
	}
	if err := runtime.enqueue(nil, defaultMemoryConfig(), "user", "assistant"); err == nil {
		t.Fatal("closed runtime should reject new jobs")
	}
	if len(runtime.jobs) != 0 {
		t.Fatalf("closed runtime accepted %d jobs", len(runtime.jobs))
	}
}
