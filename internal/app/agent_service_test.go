package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentcore "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	agentevent "trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session"
	"trpc.group/trpc-go/trpc-agent-go/skill"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

func TestBuildChatMetrics(t *testing.T) {
	usage := &model.Usage{
		PromptTokens:     120,
		CompletionTokens: 80,
		TotalTokens:      200,
		PromptTokensDetails: model.PromptTokensDetails{
			CachedTokens: 40,
		},
		CompletionTokensDetails: model.CompletionTokensDetails{
			ReasoningTokens: 24,
		},
		TimingInfo: &model.TimingInfo{FirstTokenDuration: 500 * time.Millisecond},
	}

	got := buildChatMetrics("test-model", usage, 2500*time.Millisecond)
	if got.Model != "test-model" || got.PromptTokens != 120 || got.CompletionTokens != 80 || got.TotalTokens != 200 {
		t.Fatalf("token metrics = %#v", got)
	}
	if got.ReasoningTokens != 24 || got.CachedTokens != 40 || got.DurationMs != 2500 || got.FirstTokenMs != 500 {
		t.Fatalf("detail metrics = %#v", got)
	}
	if got.TokensPerSecond != 40 {
		t.Fatalf("tokens/second = %v, want 40", got.TokensPerSecond)
	}
}

func TestBuildChatMetricsWithoutProviderUsageOnlyKeepsTiming(t *testing.T) {
	got := buildChatMetrics("test-model", nil, 1500*time.Millisecond)
	if got.Model != "test-model" || got.DurationMs != 1500 {
		t.Fatalf("metrics = %#v", got)
	}
	if got.TotalTokens != 0 || got.TokensPerSecond != 0 || got.FirstTokenMs != 0 {
		t.Fatalf("unreported usage must remain empty: %#v", got)
	}
}

func TestInstructionWithContextDescribesWorkspacePathRules(t *testing.T) {
	workspace := filepath.Clean(t.TempDir())
	instruction := instructionWithContext(workspace)
	for _, expected := range []string{
		"当前工作区的完整绝对路径:" + workspace,
		"相对路径",
		"相对 workdir",
	} {
		if !strings.Contains(instruction, expected) {
			t.Errorf("instruction does not contain %q: %s", expected, instruction)
		}
	}
	if strings.Contains(instruction, "当前本地时间") {
		t.Fatalf("stable instruction contains dynamic time: %q", instruction)
	}
	if instruction != instructionWithContext(workspace) {
		t.Fatal("instruction changed for the same workspace")
	}

	withoutWorkspace := instructionWithContext("")
	if !strings.Contains(withoutWorkspace, "当前未选择工作区") || !strings.Contains(withoutWorkspace, "相对文件路径") {
		t.Fatalf("no-workspace instruction = %q", withoutWorkspace)
	}
}

func TestLateContextMessagesContainCurrentLocalTime(t *testing.T) {
	current := time.Date(2026, 9, 14, 12, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	messages := lateContextMessages(current)
	if len(messages) != 1 {
		t.Fatalf("late context messages = %d, want 1", len(messages))
	}
	if messages[0].Role != model.RoleUser || messages[0].Content != "[运行上下文]\n当前本地时间:2026-09-14 12:30:00 +08:00" {
		t.Fatalf("late context message = %#v", messages[0])
	}
}

func TestInstructionDoesNotExposeUnregisteredWorkspaceRequest(t *testing.T) {
	settings := NewSettingsService(newSettingsTestDB(t))
	registered := t.TempDir()
	if _, err := settings.AddWorkspace(registered); err != nil {
		t.Fatal(err)
	}
	unregistered := t.TempDir()
	validated := settings.agentWorkspacePath(unregistered)
	if validated != "" {
		t.Fatalf("unregistered workspace validated as %q", validated)
	}
	instruction := instructionWithContext(validated)
	if strings.Contains(instruction, unregistered) {
		t.Fatalf("instruction exposed unregistered request path: %q", instruction)
	}

	validated = settings.agentWorkspacePath(registered)
	if validated == "" || !strings.Contains(instructionWithContext(validated), validated) {
		t.Fatalf("registered workspace was not included: %q", validated)
	}
}

func TestKnowledgeOnlySkillOptionsDoNotExposeWorkspaceExec(t *testing.T) {
	repo, err := skill.NewFSRepository(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	opts := []llmagent.Option{
		llmagent.WithTools([]tool.Tool{newSkillRunTool(nil, nil)}),
	}
	opts = append(opts, knowledgeOnlySkillOptions(repo)...)
	agent := llmagent.New("test", opts...)

	names := make(map[string]bool)
	for _, agentTool := range agent.Tools() {
		if declaration := agentTool.Declaration(); declaration != nil {
			names[declaration.Name] = true
		}
	}
	for _, expected := range []string{"skill_load", "skill_list_docs", "skill_select_docs", "skill_run"} {
		if !names[expected] {
			t.Errorf("%s is absent", expected)
		}
	}
	if len(names) != 4 {
		t.Fatalf("agent exposes %d tools, want 4: %#v", len(names), names)
	}
	if names["workspace_exec"] {
		t.Error("workspace_exec must not be exposed")
	}
	for _, removed := range []string{
		"create_todo", "list_todos", "set_todo_status", "delete_todo",
		"todo_stats", "list_events", "reminder_upcoming", "reminder_settings",
		"generate_weekly_report", "create_document", "create_table", "export_todos",
	} {
		if names[removed] {
			t.Errorf("legacy tool %s must not be exposed", removed)
		}
	}
}

func TestChatAgentToolsPreserveApplicationToolsWhenMemoryEnabled(t *testing.T) {
	memoryTool := function.NewFunctionTool(
		func(context.Context, struct{}) (string, error) { return "", nil },
		function.WithName("memory_search"),
	)
	names := make(map[string]bool)
	for _, agentTool := range chatAgentTools(nil, []tool.Tool{memoryTool}) {
		if declaration := agentTool.Declaration(); declaration != nil {
			names[declaration.Name] = true
		}
	}
	for _, name := range []string{"skill_run", "memory_search"} {
		if !names[name] {
			t.Errorf("%s is absent when memory tools are enabled", name)
		}
	}
}

type requestCaptureModel struct {
	request *model.Request
}

func (m *requestCaptureModel) GenerateContent(_ context.Context, request *model.Request) (<-chan *model.Response, error) {
	m.request = request
	responses := make(chan *model.Response, 1)
	responses <- &model.Response{
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "ok"}}},
		Done:    true,
	}
	close(responses)
	return responses, nil
}

func (m *requestCaptureModel) Info() model.Info { return model.Info{Name: "capture"} }

func TestChatAgentRequestUsesCompactSkillSurface(t *testing.T) {
	skillsRoot := t.TempDir()
	ensureBuiltinSkills(skillsRoot)
	repo, err := skill.NewFSRepository(skillsRoot)
	if err != nil {
		t.Fatal(err)
	}
	capture := &requestCaptureModel{}
	opts := []llmagent.Option{
		llmagent.WithModel(capture),
		llmagent.WithInstruction(instructionWithContext("")),
		llmagent.WithTools(chatAgentTools(nil, nil)),
		llmagent.WithMaxToolIterations(8),
	}
	opts = append(opts, knowledgeOnlySkillOptions(repo)...)
	agent := llmagent.New("test", opts...)
	invocation := agentcore.NewInvocation(
		agentcore.WithInvocationMessage(model.NewUserMessage("hello")),
		agentcore.WithInvocationSession(&session.Session{}),
	)
	events, err := agent.Run(context.Background(), invocation)
	if err != nil {
		t.Fatal(err)
	}
	for event := range events {
		if event != nil && event.RequiresCompletion {
			key := agentcore.GetAppendEventNoticeKey(event.ID)
			_ = invocation.AddNoticeChannel(context.Background(), key)
			_ = invocation.NotifyCompletion(context.Background(), key)
		}
	}
	if capture.request == nil {
		t.Fatal("model request was not captured")
	}
	if len(capture.request.Tools) != 4 {
		t.Fatalf("request exposes %d tools, want 4: %#v", len(capture.request.Tools), capture.request.Tools)
	}
	for _, name := range []string{"skill_load", "skill_list_docs", "skill_select_docs", "skill_run"} {
		if capture.request.Tools[name] == nil {
			t.Errorf("request tool %s is absent", name)
		}
	}
	encoded, err := json.Marshal(capture.request)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) >= 20000 {
		t.Fatalf("compact agent request metadata is unexpectedly large: %d bytes", len(encoded))
	}
}

func TestModelRequestCacheFields(t *testing.T) {
	herdsman := Provider{ID: 11, Kind: " Herdsman "}
	first := modelRequestCacheFields(herdsman, 42)
	second := modelRequestCacheFields(herdsman, 42)
	otherConversation := modelRequestCacheFields(herdsman, 43)
	if first["cache_prompt"] != true {
		t.Fatalf("Herdsman cache_prompt = %#v", first["cache_prompt"])
	}
	firstSlot, ok := first["id_slot"].(int64)
	if !ok || firstSlot <= 0 || firstSlot > 0x7fffffff {
		t.Fatalf("Herdsman id_slot = %#v", first["id_slot"])
	}
	if second["id_slot"] != first["id_slot"] {
		t.Fatalf("same conversation slots differ: %#v / %#v", first, second)
	}
	if otherConversation["id_slot"] == first["id_slot"] {
		t.Fatalf("different conversation slots match: %#v / %#v", first, otherConversation)
	}

	openAI := modelRequestCacheFields(Provider{ID: 9, Kind: "openai"}, 42)
	openAIKey, ok := openAI["prompt_cache_key"].(string)
	if !ok || !strings.HasPrefix(openAIKey, "blankmind:v1:") {
		t.Fatalf("OpenAI prompt_cache_key = %#v", openAI["prompt_cache_key"])
	}
	if strings.Contains(openAIKey, ":9:42") {
		t.Fatalf("OpenAI prompt_cache_key exposes raw IDs: %q", openAIKey)
	}
	if modelRequestCacheFields(Provider{ID: 9, Kind: "openai"}, 43)["prompt_cache_key"] == openAIKey {
		t.Fatal("different OpenAI conversations use the same cache key")
	}

	for _, kind := range []string{"custom", "openrouter", "deepseek", ""} {
		if fields := modelRequestCacheFields(Provider{Kind: kind}, 42); len(fields) != 0 {
			t.Errorf("provider %q received cache fields: %#v", kind, fields)
		}
	}
}

func TestRunContextPreservesStablePrefixAndAddsCacheFields(t *testing.T) {
	capture := &requestCaptureModel{}
	agent := llmagent.New("test",
		llmagent.WithModel(capture),
		llmagent.WithInstruction(instructionWithContext("C:\\workspace")),
	)
	sess := &session.Session{Events: []agentevent.Event{
		requestHistoryEvent("user", model.NewUserMessage("previous question")),
		requestHistoryEvent("test", model.NewAssistantMessage("previous answer")),
	}}
	current := time.Date(2026, 9, 14, 12, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	fields := modelRequestCacheFields(Provider{ID: 11, Kind: "herdsman"}, 42)
	runOptions := agentcore.RunOptions{}
	agentcore.WithLateContextMessages(lateContextMessages(current))(&runOptions)
	agentcore.WithModelRequestExtraFields(fields)(&runOptions)
	invocation := agentcore.NewInvocation(
		agentcore.WithInvocationMessage(model.NewUserMessage("current question")),
		agentcore.WithInvocationSession(sess),
		agentcore.WithInvocationRunOptions(runOptions),
	)
	events, err := agent.Run(context.Background(), invocation)
	if err != nil {
		t.Fatal(err)
	}
	for event := range events {
		if event != nil && event.RequiresCompletion {
			key := agentcore.GetAppendEventNoticeKey(event.ID)
			_ = invocation.AddNoticeChannel(context.Background(), key)
			_ = invocation.NotifyCompletion(context.Background(), key)
		}
	}
	if capture.request == nil {
		t.Fatal("model request was not captured")
	}
	messages := capture.request.Messages
	want := []model.Message{
		model.NewSystemMessage(instructionWithContext("C:\\workspace")),
		model.NewUserMessage("previous question"),
		model.NewAssistantMessage("previous answer"),
		lateContextMessages(current)[0],
		model.NewUserMessage("current question"),
	}
	if len(messages) != len(want) {
		t.Fatalf("request messages = %d, want %d: %#v", len(messages), len(want), messages)
	}
	for i := range want {
		if messages[i].Role != want[i].Role || messages[i].Content != want[i].Content {
			t.Errorf("message[%d] = %#v, want %#v", i, messages[i], want[i])
		}
	}
	if capture.request.ExtraFields["cache_prompt"] != true || capture.request.ExtraFields["id_slot"] != fields["id_slot"] {
		t.Fatalf("request cache fields = %#v, want %#v", capture.request.ExtraFields, fields)
	}
}

func requestHistoryEvent(author string, message model.Message) agentevent.Event {
	return agentevent.Event{
		Author: author,
		Response: &model.Response{
			Done:    true,
			Choices: []model.Choice{{Index: 0, Message: message}},
		},
	}
}

func TestChatCapabilityOptionsDisableAllToolsForPlainChat(t *testing.T) {
	skillsRoot := t.TempDir()
	ensureBuiltinSkills(skillsRoot)
	repo, err := skill.NewFSRepository(skillsRoot)
	if err != nil {
		t.Fatal(err)
	}
	capture := &requestCaptureModel{}
	opts := []llmagent.Option{
		llmagent.WithModel(capture),
		llmagent.WithInstruction(plainChatInstruction),
		llmagent.WithGenerationConfig(model.GenerationConfig{Stream: true}),
		llmagent.WithMaxToolIterations(8),
	}
	opts = append(opts, chatCapabilityOptions(false, chatAgentTools(nil, nil), repo, true)...)
	agent := llmagent.New("test", opts...)
	invocation := agentcore.NewInvocation(
		agentcore.WithInvocationMessage(model.NewUserMessage("hello")),
		agentcore.WithInvocationSession(&session.Session{}),
	)
	events, err := agent.Run(context.Background(), invocation)
	if err != nil {
		t.Fatal(err)
	}
	for event := range events {
		if event != nil && event.RequiresCompletion {
			key := agentcore.GetAppendEventNoticeKey(event.ID)
			_ = invocation.AddNoticeChannel(context.Background(), key)
			_ = invocation.NotifyCompletion(context.Background(), key)
		}
	}
	if capture.request == nil {
		t.Fatal("model request was not captured")
	}
	if len(capture.request.Tools) != 0 {
		t.Fatalf("plain-chat request exposes %d tools, want 0: %#v", len(capture.request.Tools), capture.request.Tools)
	}
}

func TestChatCapabilityOptionsKeepFullSkillSurface(t *testing.T) {
	skillsRoot := t.TempDir()
	ensureBuiltinSkills(skillsRoot)
	repo, err := skill.NewFSRepository(skillsRoot)
	if err != nil {
		t.Fatal(err)
	}
	capture := &requestCaptureModel{}
	opts := []llmagent.Option{
		llmagent.WithModel(capture),
		llmagent.WithInstruction(systemInstruction),
	}
	opts = append(opts, chatCapabilityOptions(true, chatAgentTools(nil, nil), repo, false)...)
	agent := llmagent.New("test", opts...)
	invocation := agentcore.NewInvocation(
		agentcore.WithInvocationMessage(model.NewUserMessage("hello")),
		agentcore.WithInvocationSession(&session.Session{}),
	)
	events, err := agent.Run(context.Background(), invocation)
	if err != nil {
		t.Fatal(err)
	}
	for event := range events {
		if event != nil && event.RequiresCompletion {
			key := agentcore.GetAppendEventNoticeKey(event.ID)
			_ = invocation.AddNoticeChannel(context.Background(), key)
			_ = invocation.NotifyCompletion(context.Background(), key)
		}
	}
	if capture.request == nil {
		t.Fatal("model request was not captured")
	}
	if len(capture.request.Tools) != 4 {
		t.Fatalf("full Agent request exposes %d tools, want 4: %#v", len(capture.request.Tools), capture.request.Tools)
	}
}

func TestFinalChatErrorPrefersUpstreamErrorWhenContentIsEmpty(t *testing.T) {
	upstream := errors.New("403 model is only available on agentic harnesses")
	if got := finalChatError("", upstream); !errors.Is(got, upstream) {
		t.Fatalf("empty response error = %v, want upstream error", got)
	}
	if got := finalChatError("partial response", upstream); !errors.Is(got, upstream) {
		t.Fatalf("partial response error = %v, want upstream error", got)
	}
	if got := finalChatError("", nil); got == nil || got.Error() != "模型未返回有效内容" {
		t.Fatalf("generic empty response error = %v", got)
	}
}

func TestHerdsmanCompactSkillRequestLive(t *testing.T) {
	if os.Getenv("BLANKMIND_TEST_HERDSMAN") != "1" {
		t.Skip("set BLANKMIND_TEST_HERDSMAN=1 to run against local Herdsman")
	}
	todo := setupSkillCommandTest(t)
	skillsRoot := t.TempDir()
	ensureBuiltinSkills(skillsRoot)
	repo, err := skill.NewFSRepository(skillsRoot)
	if err != nil {
		t.Fatal(err)
	}
	client, err := buildModel(Provider{
		Name: "Herdsman", Kind: "herdsman", BaseURL: "http://localhost:8080/v1", Model: "Gemma4:12B-IT",
	})
	if err != nil {
		t.Fatal(err)
	}
	opts := []llmagent.Option{
		llmagent.WithModel(client),
		llmagent.WithInstruction(instructionWithContext("")),
		llmagent.WithTools(chatAgentTools(&todoToolSource{input: todoSourceInput{
			Kind: "conversation", TextContent: "创建 skill-live-test 待办",
		}}, nil)),
		llmagent.WithGenerationConfig(model.GenerationConfig{Stream: true}),
		llmagent.WithMaxToolIterations(8),
	}
	opts = append(opts, knowledgeOnlySkillOptions(repo)...)
	agent := llmagent.New("test", opts...)
	baseRunner := runner.NewRunner("test", agent)
	defer baseRunner.Close()
	events, err := baseRunner.Run(
		context.Background(), "live-user", "live-session",
		model.NewUserMessage("请使用 todo skill 创建一个标题为 skill-live-test 的待办，不设置截止时间。"),
		agentcore.WithStream(true),
	)
	if err != nil {
		t.Fatal(err)
	}
	for event := range events {
		if event != nil && event.Error != nil {
			t.Fatal(event.Error)
		}
	}
	items, err := todo.ListTodos()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Title != "skill-live-test" {
		t.Fatalf("Gemma4 did not execute the Cobra skill command: %#v", items)
	}
}

func TestApplyReasoningOff(t *testing.T) {
	tests := []struct {
		name      string
		kind      string
		modelName string
		level     string
		want      *bool
	}{
		{name: "qwen empty", kind: "qwen", modelName: "qwen-plus", level: "", want: boolPtr(false)},
		{name: "deepseek effort off", kind: "deepseek", modelName: "deepseek-flash", level: "off", want: boolPtr(false)},
		{name: "deepseek effort none", kind: "deepseek", modelName: "deepseek-flash", level: "none", want: boolPtr(false)},
		{name: "hunyuan", kind: "hunyuan", modelName: "hunyuan-turbos-latest", want: boolPtr(false)},
		{name: "glm", kind: "glm", modelName: "glm-4.7", want: boolPtr(false)},
		{name: "minimax case insensitive", kind: "minimax", modelName: "MiniMax-M2", want: boolPtr(false)},
		{name: "always cannot disable", kind: "kimi", modelName: "kimi-k2-thinking"},
		{name: "volcengine effort uses default when disabled", kind: "volcengine-plan", modelName: "glm-5.3", level: "off"},
		{name: "unsupported has no toggle", kind: "openai", modelName: "gpt-4o"},
		{name: "openai effort has no toggle", kind: "openai", modelName: "o3-mini"},
		{name: "custom uses provider default", kind: "custom", modelName: "unknown"},
		{name: "herdsman explicit off", kind: "herdsman", modelName: "local-model", level: "off", want: boolPtr(false)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got model.GenerationConfig
			applyReasoning(&got, tt.kind, tt.modelName, tt.level)
			if tt.want == nil {
				if got.ThinkingEnabled != nil {
					t.Fatalf("ThinkingEnabled = %v, want nil", *got.ThinkingEnabled)
				}
				return
			}
			if got.ThinkingEnabled == nil || *got.ThinkingEnabled != *tt.want {
				t.Fatalf("ThinkingEnabled = %v, want %v", got.ThinkingEnabled, *tt.want)
			}
			if got.ReasoningEffort != nil {
				t.Fatalf("ReasoningEffort = %q, want nil", *got.ReasoningEffort)
			}
		})
	}
}

func TestApplyReasoningOn(t *testing.T) {
	tests := []struct {
		name       string
		kind       string
		modelName  string
		level      string
		wantToggle *bool
		wantEffort string
	}{
		{name: "qwen toggle", kind: "qwen", modelName: "qwen-plus", level: "on", wantToggle: boolPtr(true)},
		{name: "minimax toggle", kind: "minimax", modelName: "MiniMax-M2", level: "on", wantToggle: boolPtr(true)},
		{name: "deepseek effort", kind: "deepseek", modelName: "deepseek-flash", level: "max", wantToggle: boolPtr(true), wantEffort: "max"},
		{name: "openai effort", kind: "openai", modelName: "o3-mini", level: "medium", wantEffort: "medium"},
		{name: "openrouter compatible effort", kind: "openrouter", modelName: "openai/gpt-5", level: "xhigh", wantEffort: "xhigh"},
		{name: "herdsman compatible effort", kind: "herdsman", modelName: "local-model", level: "xhigh", wantToggle: boolPtr(true), wantEffort: "xhigh"},
		{name: "volcengine effort", kind: "volcengine-plan", modelName: "glm-5.3", level: "high", wantEffort: "high"},
		{name: "volcengine rejects unsupported local level", kind: "volcengine-plan", modelName: "glm-5.3", level: "xhigh"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got model.GenerationConfig
			applyReasoning(&got, tt.kind, tt.modelName, tt.level)
			if tt.wantToggle == nil {
				if got.ThinkingEnabled != nil {
					t.Fatalf("ThinkingEnabled = %v, want nil", *got.ThinkingEnabled)
				}
			} else if got.ThinkingEnabled == nil || *got.ThinkingEnabled != *tt.wantToggle {
				t.Fatalf("ThinkingEnabled = %v, want %v", got.ThinkingEnabled, *tt.wantToggle)
			}
			if tt.wantEffort == "" {
				if got.ReasoningEffort != nil {
					t.Fatalf("ReasoningEffort = %q, want nil", *got.ReasoningEffort)
				}
			} else if got.ReasoningEffort == nil || *got.ReasoningEffort != tt.wantEffort {
				t.Fatalf("ReasoningEffort = %v, want %q", got.ReasoningEffort, tt.wantEffort)
			}
		})
	}
}

func TestDisabledReasoningPayload(t *testing.T) {
	tests := []struct {
		name      string
		kind      string
		modelName string
		assert    func(*testing.T, map[string]any)
	}{
		{
			name: "qwen uses enable_thinking false", kind: "qwen", modelName: "qwen-plus",
			assert: func(t *testing.T, body map[string]any) {
				if value, ok := body["enable_thinking"].(bool); !ok || value {
					t.Fatalf("enable_thinking = %#v, want false", body["enable_thinking"])
				}
			},
		},
		{
			name: "deepseek uses disabled thinking object", kind: "deepseek", modelName: "deepseek-flash",
			assert: assertThinkingDisabled,
		},
		{
			name: "hunyuan uses disabled thinking object", kind: "hunyuan", modelName: "hunyuan-turbos-latest",
			assert: assertThinkingDisabled,
		},
		{
			name: "glm uses disabled thinking object", kind: "glm", modelName: "glm-4.7",
			assert: assertThinkingDisabled,
		},
		{
			name: "minimax uses disabled thinking object", kind: "minimax", modelName: "MiniMax-M2",
			assert: assertThinkingDisabled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bodyCh := make(chan map[string]any, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("read request: %v", err)
				}
				var payload map[string]any
				if err := json.Unmarshal(body, &payload); err != nil {
					t.Errorf("decode request: %v", err)
				}
				bodyCh <- payload
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"test","object":"chat.completion","created":1,"model":"test","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
			}))
			defer server.Close()

			client, err := buildModel(Provider{
				Name: "test", Kind: tt.kind, BaseURL: server.URL, Model: tt.modelName,
			})
			if err != nil {
				t.Fatal(err)
			}
			request := model.NewRequest([]model.Message{model.NewUserMessage("test")})
			applyReasoning(&request.GenerationConfig, tt.kind, tt.modelName, "")
			responses, err := client.GenerateContent(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			for response := range responses {
				if response.Error != nil {
					t.Fatal(response.Error)
				}
			}
			tt.assert(t, <-bodyCh)
		})
	}
}

func TestHerdsmanReasoningPayload(t *testing.T) {
	bodyCh := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		bodyCh <- payload
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"test","object":"chat.completion","created":1,"model":"local-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	client, err := buildModel(Provider{
		Name: "Herdsman", Kind: "herdsman", BaseURL: server.URL, Model: "local-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	request := model.NewRequest([]model.Message{model.NewUserMessage("test")})
	applyReasoning(&request.GenerationConfig, "herdsman", "local-model", "xhigh")
	responses, err := client.GenerateContent(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	for response := range responses {
		if response.Error != nil {
			t.Fatal(response.Error)
		}
	}

	body := <-bodyCh
	if enabled, ok := body["thinking_enabled"].(bool); !ok || !enabled {
		t.Fatalf("thinking_enabled = %#v, want true", body["thinking_enabled"])
	}
	if effort := body["reasoning_effort"]; effort != "xhigh" {
		t.Fatalf("reasoning_effort = %#v, want xhigh", effort)
	}
}

func TestVolcenginePlanReasoningPayload(t *testing.T) {
	bodyCh := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		bodyCh <- payload
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"test","object":"chat.completion","created":1,"model":"glm-5.3","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	client, err := buildModel(Provider{
		Name: "火山方舟 Agent Plan", Kind: "volcengine-plan", BaseURL: server.URL, Model: "glm-5.3",
	})
	if err != nil {
		t.Fatal(err)
	}
	request := model.NewRequest([]model.Message{model.NewUserMessage("test")})
	applyReasoning(&request.GenerationConfig, "volcengine-plan", "glm-5.3", "high")
	responses, err := client.GenerateContent(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	for response := range responses {
		if response.Error != nil {
			t.Fatal(response.Error)
		}
	}

	body := <-bodyCh
	if effort := body["reasoning_effort"]; effort != "high" {
		t.Fatalf("reasoning_effort = %#v, want high", effort)
	}
}

func assertThinkingDisabled(t *testing.T, body map[string]any) {
	t.Helper()
	thinking, ok := body["thinking"].(map[string]any)
	if !ok || thinking["type"] != "disabled" {
		t.Fatalf("thinking = %#v, want {type: disabled}", body["thinking"])
	}
}

func boolPtr(value bool) *bool {
	return &value
}
