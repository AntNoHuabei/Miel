package app

import (
	"context"
	"strings"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/memory"
	memoryinmemory "trpc.group/trpc-go/trpc-agent-go/memory/inmemory"
)

func TestTodoPromptWithMemoryIncludesEnabledMemory(t *testing.T) {
	settings := NewSettingsService(newSettingsTestDB(t))
	runtime := &memoryRuntime{store: memoryinmemory.NewMemoryService()}
	if err := runtime.AddMemory(
		context.Background(), blankMindMemoryUser, "北极星项目的负责人是李鹏", []string{"项目"},
		memory.WithMetadata(&memory.Metadata{Kind: memory.KindFact}),
	); err != nil {
		t.Fatal(err)
	}

	prompt, err := todoPromptWithMemory(settings, runtime, "提取待办")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, `"北极星项目的负责人是李鹏"`) {
		t.Fatalf("prompt does not contain memory: %q", prompt)
	}
	if !strings.Contains(prompt, "不得根据记忆新增原始内容未表达的任务") {
		t.Fatalf("prompt does not constrain memory use: %q", prompt)
	}
}

func TestTodoPromptWithMemoryOmitsDisabledMemory(t *testing.T) {
	settings := NewSettingsService(newSettingsTestDB(t))
	if err := settings.SetSetting(SettingMemoryConfig, `{"enabled":false,"autoExtract":true,"strategy":"balanced"}`); err != nil {
		t.Fatal(err)
	}
	runtime := &memoryRuntime{store: memoryinmemory.NewMemoryService()}
	if err := runtime.AddMemory(context.Background(), blankMindMemoryUser, "不应进入请求", nil); err != nil {
		t.Fatal(err)
	}

	const base = "提取待办"
	prompt, err := todoPromptWithMemory(settings, runtime, base)
	if err != nil {
		t.Fatal(err)
	}
	if prompt != base {
		t.Fatalf("prompt = %q, want unchanged base prompt", prompt)
	}
}
