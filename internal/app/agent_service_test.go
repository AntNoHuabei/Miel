package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model"
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

func TestKnowledgeOnlySkillOptionsDoNotExposeWorkspaceExec(t *testing.T) {
	repo, err := skill.NewFSRepository(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	opts := []llmagent.Option{
		llmagent.WithTools(todoAgentTools(nil)),
	}
	opts = append(opts, knowledgeOnlySkillOptions(repo)...)
	agent := llmagent.New("test", opts...)

	names := make(map[string]bool)
	for _, agentTool := range agent.Tools() {
		if declaration := agentTool.Declaration(); declaration != nil {
			names[declaration.Name] = true
		}
	}
	if !names["skill_load"] {
		t.Error("skill_load is absent")
	}
	if names["workspace_exec"] {
		t.Error("workspace_exec must not be exposed")
	}
	if !names["list_todos"] {
		t.Error("list_todos is absent")
	}
}

func TestChatAgentToolsPreserveApplicationToolsWhenMemoryEnabled(t *testing.T) {
	memoryTool := function.NewFunctionTool(
		func(context.Context, struct{}) (string, error) { return "", nil },
		function.WithName("memory_search"),
	)
	names := make(map[string]bool)
	for _, agentTool := range chatAgentTools([]tool.Tool{memoryTool}) {
		if declaration := agentTool.Declaration(); declaration != nil {
			names[declaration.Name] = true
		}
	}
	for _, name := range []string{"list_todos", "todo_stats", "memory_search"} {
		if !names[name] {
			t.Errorf("%s is absent when memory tools are enabled", name)
		}
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
		{name: "herdsman compatible effort", kind: "herdsman", modelName: "local-model", level: "xhigh", wantToggle: boolPtr(true), wantEffort: "xhigh"},
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
