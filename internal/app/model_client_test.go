package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

func TestModelsEndpoint(t *testing.T) {
	tests := []struct {
		base string
		want string
	}{
		{"https://api.openai.com/v1", "https://api.openai.com/v1/models"},
		{"https://api.deepseek.com/", "https://api.deepseek.com/models"},
		{"http://localhost:11434/v1/chat/completions", "http://localhost:11434/v1/models"},
		{"http://localhost:11434/v1/models", "http://localhost:11434/v1/models"},
	}
	for _, tt := range tests {
		t.Run(tt.base, func(t *testing.T) {
			got, err := modelsEndpoint(tt.base)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("modelsEndpoint() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFetchProviderModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("path = %q, want /v1/models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data":[{"id":"z-model"},{"id":"a-model"}],
			"models":[{"name":"local-model"},{"model":"a-model"}]
		}`))
	}))
	defer server.Close()

	service := &SettingsService{}
	got, err := service.FetchProviderModels(ProviderInput{
		BaseURL: server.URL + "/v1",
		APIKey:  "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a-model", "local-model", "z-model"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("FetchProviderModels() = %#v, want %#v", got, want)
	}
}

func TestFetchProviderModelsReturnsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "invalid token", http.StatusUnauthorized)
	}))
	defer server.Close()

	service := &SettingsService{}
	_, err := service.FetchProviderModels(ProviderInput{BaseURL: server.URL})
	if err == nil {
		t.Fatal("FetchProviderModels() error = nil, want HTTP error")
	}
}

func TestVolcenginePlanUsesOpenAICompatibleChatEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/plan/v3/chat/completions" {
			t.Errorf("path = %q, want /api/plan/v3/chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer agent-plan-key" {
			t.Errorf("Authorization = %q", got)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if payload["model"] != "ark-code-latest" {
			t.Errorf("model = %#v, want ark-code-latest", payload["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"test","object":"chat.completion","created":1,"model":"ark-code-latest","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	client, err := buildModel(Provider{
		Name: "火山方舟 Agent Plan", Kind: "volcengine-plan",
		BaseURL: server.URL + "/api/plan/v3", APIKey: "agent-plan-key", Model: "ark-code-latest",
	})
	if err != nil {
		t.Fatal(err)
	}
	responses, err := client.GenerateContent(context.Background(), model.NewRequest([]model.Message{model.NewUserMessage("hello")}))
	if err != nil {
		t.Fatal(err)
	}
	var content string
	for response := range responses {
		if response.Error != nil {
			t.Fatal(response.Error)
		}
		if len(response.Choices) > 0 {
			content += response.Choices[0].Message.Content
		}
	}
	if content != "ok" {
		t.Fatalf("response content = %q, want ok", content)
	}
}

func TestDiscoverVolcenginePlanModelsUsesStaticCatalog(t *testing.T) {
	models, err := (&SettingsService{}).DiscoverProviderModels(ProviderInput{
		Kind: "volcengine-plan", BaseURL: "http://127.0.0.1:1/api/plan/v3",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 13 || models[0].ID != "ark-code-latest" {
		t.Fatalf("Agent Plan models = %#v", models)
	}
	foundMini := false
	for _, item := range models {
		if !item.SupportsTools || item.Status != "available" {
			t.Fatalf("Agent Plan model capabilities = %#v", item)
		}
		if item.ID == "doubao-seed-2.0-mini" {
			foundMini = true
		}
	}
	if !foundMini {
		t.Fatal("doubao-seed-2.0-mini is missing from static discovery")
	}
}

func TestDiscoverProviderModelsMergesHerdsmanCapabilities(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[
				{"id":"plain-chat","status":"stopped"},
				{"id":"reasoning-chat","status":"running"},
				{"id":"speech","status":"running"}
			]}`))
		case "/api/v1/models":
			_, _ = w.Write([]byte(`[
				{"name":"plain-chat","type":"text-generation","parameters":{"reasoning_control":{"type":"none","default_enabled":false}}},
				{"name":"reasoning-chat","type":"multimodal","parameters":{"reasoning_control":{"type":"thinking","efforts":["low","medium","xhigh"],"default_effort":"xhigh"}}},
				{"name":"speech","type":"tts","parameters":{}}
			]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	models, err := (&SettingsService{}).DiscoverProviderModels(ProviderInput{
		Kind:    "herdsman",
		BaseURL: server.URL + "/v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Fatalf("DiscoverProviderModels() returned %d models, want 2: %#v", len(models), models)
	}
	if models[0].ID != "plain-chat" || models[0].Reasoning.Type != ReasoningNone || models[0].Multimodal {
		t.Fatalf("plain model = %#v", models[0])
	}
	if models[1].ID != "reasoning-chat" || models[1].Status != "running" || !models[1].Multimodal {
		t.Fatalf("reasoning model = %#v", models[1])
	}
	wantLevels := []string{"low", "medium", "xhigh"}
	if models[1].Reasoning.Type != ReasoningEffort || !reflect.DeepEqual(models[1].Reasoning.Levels, wantLevels) {
		t.Fatalf("reasoning spec = %#v, want effort %#v", models[1].Reasoning, wantLevels)
	}
}

func TestDiscoverOpenRouterModelsReadsCapabilities(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/models" {
			t.Errorf("path = %q, want /api/v1/models", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[
			{"id":"plain-only","architecture":{"input_modalities":["text"]},"supported_parameters":["temperature"]},
			{"id":"text-model","architecture":{"input_modalities":["text"]},"supported_parameters":["tools"]},
			{"id":"vision-reasoning","architecture":{"input_modalities":["text","image"]},"supported_parameters":["reasoning_effort","tools"]}
		]}`))
	}))
	defer server.Close()

	models, err := (&SettingsService{}).DiscoverProviderModels(ProviderInput{
		Kind: "openrouter", BaseURL: server.URL + "/api/v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 3 || models[0].ID != "plain-only" || models[0].Multimodal || models[0].SupportsTools {
		t.Fatalf("text model = %#v", models)
	}
	if models[1].ID != "text-model" || !models[1].SupportsTools {
		t.Fatalf("tool model = %#v", models[1])
	}
	vision := models[2]
	if vision.ID != "vision-reasoning" || !vision.Multimodal || !vision.SupportsTools || vision.Reasoning.Type != ReasoningEffort {
		t.Fatalf("vision model = %#v", vision)
	}
	wantLevels := []string{"low", "medium", "high", "xhigh"}
	if !reflect.DeepEqual(vision.Reasoning.Levels, wantLevels) {
		t.Fatalf("reasoning levels = %#v, want %#v", vision.Reasoning.Levels, wantLevels)
	}
}

func TestOpenRouterToolCapabilityCache(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[
			{"id":"plain","supported_parameters":["temperature"]},
			{"id":"agent","supported_parameters":["tools"]}
		]}`))
	}))
	defer server.Close()

	service := NewSettingsService(nil)
	provider := Provider{
		ID: 7, Name: "OpenRouter", Kind: "openrouter", BaseURL: server.URL + "/v1",
		APIKey: "test", Model: "plain",
	}
	if supported, known := service.providerSupportsTools(provider); !known || supported {
		t.Fatalf("plain capability = supported %v known %v", supported, known)
	}
	provider.Model = "agent"
	if supported, known := service.providerSupportsTools(provider); !known || !supported {
		t.Fatalf("agent capability = supported %v known %v", supported, known)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("model discovery requests = %d, want 1", got)
	}
	provider.BaseURL = server.URL + "/alternate/v1"
	if supported, known := service.providerSupportsTools(provider); !known || !supported {
		t.Fatalf("agent capability after Base URL change = supported %v known %v", supported, known)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("model discovery requests after Base URL change = %d, want 2", got)
	}

	key := providerCapabilityCacheKey(provider)
	service.capabilityMu.Lock()
	entry := service.capabilityCache[key]
	entry.expiresAt = time.Now().Add(-time.Second)
	service.capabilityCache[key] = entry
	service.capabilityMu.Unlock()
	if supported, known := service.providerSupportsTools(provider); !known || !supported {
		t.Fatalf("refreshed agent capability = supported %v known %v", supported, known)
	}
	if got := requests.Load(); got != 3 {
		t.Fatalf("model discovery requests after expiry = %d, want 3", got)
	}

	service.invalidateProviderCapabilities(provider.ID)
	if _, ok := service.cachedProviderCapability(provider, provider.Model); ok {
		t.Fatal("provider capability cache was not invalidated")
	}
}

func TestOpenRouterToolCapabilityFailsOpenWhenDiscoveryFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	provider := Provider{
		ID: 8, Name: "OpenRouter", Kind: "openrouter", BaseURL: server.URL + "/v1",
		APIKey: "test", Model: "unknown",
	}
	supported, known := NewSettingsService(nil).providerSupportsTools(provider)
	if known || !supported {
		t.Fatalf("failed discovery capability = supported %v known %v, want fail-open unknown", supported, known)
	}
}

func TestChatModelRetriesRateLimitThreeTimes(t *testing.T) {
	var requests atomic.Int32
	var bodyMu sync.Mutex
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := requests.Add(1)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
		}
		bodyMu.Lock()
		bodies = append(bodies, string(body))
		bodyMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "0")
		if attempt <= chatModelMaxRetries {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"rate limited","type":"rate_limit_error","code":"429"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"test","object":"chat.completion","created":1,"model":"test","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	client, err := buildChatModel(Provider{Name: "test", Kind: "openai", BaseURL: server.URL, Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	responses, err := client.GenerateContent(context.Background(), model.NewRequest([]model.Message{model.NewUserMessage("hello")}))
	if err != nil {
		t.Fatal(err)
	}
	var content string
	for response := range responses {
		if response.Error != nil {
			t.Fatal(response.Error)
		}
		if len(response.Choices) > 0 {
			content += response.Choices[0].Message.Content
		}
	}
	if content != "ok" {
		t.Fatalf("response content = %q, want ok", content)
	}
	if got := requests.Load(); got != 4 {
		t.Fatalf("chat requests = %d, want 4", got)
	}
	for index, body := range bodies[1:] {
		if body != bodies[0] {
			t.Fatalf("retry request %d body changed\nfirst: %s\nretry: %s", index+1, bodies[0], body)
		}
	}
}

func TestChatModelHonorsRetryAfterMilliseconds(t *testing.T) {
	var requests atomic.Int32
	var firstRequestAt time.Time
	var retryRequestAt time.Time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempt := requests.Add(1)
		if attempt == 1 {
			firstRequestAt = time.Now()
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After-Ms", "25")
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"rate limited","code":"429"}}`))
			return
		}
		retryRequestAt = time.Now()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"test","object":"chat.completion","created":1,"model":"test","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	client, err := buildChatModel(Provider{Name: "test", Kind: "openai", BaseURL: server.URL, Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	responses, err := client.GenerateContent(context.Background(), model.NewRequest([]model.Message{model.NewUserMessage("hello")}))
	if err != nil {
		t.Fatal(err)
	}
	for response := range responses {
		if response.Error != nil {
			t.Fatal(response.Error)
		}
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("chat requests = %d, want 2", got)
	}
	if delay := retryRequestAt.Sub(firstRequestAt); delay < 20*time.Millisecond {
		t.Fatalf("retry delay = %s, want Retry-After-Ms delay", delay)
	}
}

func TestChatModelDoesNotRetryForbidden(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"forbidden","type":"permission_error","code":"403"}}`))
	}))
	defer server.Close()

	client, err := buildChatModel(Provider{Name: "test", Kind: "openai", BaseURL: server.URL, Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	responses, err := client.GenerateContent(context.Background(), model.NewRequest([]model.Message{model.NewUserMessage("hello")}))
	if err != nil {
		t.Fatal(err)
	}
	for range responses {
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("forbidden requests = %d, want 1", got)
	}
}

func TestRegularModelKeepsSDKDefaultRetryCount(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limited","type":"rate_limit_error","code":"429"}}`))
	}))
	defer server.Close()

	client, err := buildModel(Provider{Name: "test", Kind: "openai", BaseURL: server.URL, Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	responses, err := client.GenerateContent(context.Background(), model.NewRequest([]model.Message{model.NewUserMessage("hello")}))
	if err != nil {
		t.Fatal(err)
	}
	var responseErr error
	for response := range responses {
		if response.Error != nil {
			responseErr = response.Error
		}
	}
	if responseErr == nil {
		t.Fatal("regular model rate limit error is missing")
	}
	if got := requests.Load(); got != 3 {
		t.Fatalf("regular model requests = %d, want SDK default 3", got)
	}
}

func TestSetProviderModelSyncsHerdsmanMultimodalCapability(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"text-chat"},{"id":"vision-chat"}]}`))
		case "/api/v1/models":
			_, _ = w.Write([]byte(`[
				{"name":"text-chat","type":"text-generation"},
				{"name":"vision-chat","type":"multimodal"}
			]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	db := newSettingsTestDB(t)
	if _, err := db.Exec(`
		INSERT INTO providers (id, name, kind, base_url, api_key, model, multimodal, is_default, created_at)
		VALUES (1, 'Herdsman', 'herdsman', ?, '', 'text-chat', 0, 1, 1);
		INSERT INTO provider_models (provider_id, model, label, custom, created_at)
		VALUES (1, 'text-chat', '', 1, 1), (1, 'vision-chat', '', 1, 1);`, server.URL+"/v1"); err != nil {
		t.Fatal(err)
	}

	service := NewSettingsService(db)
	assertCurrent := func(model string, multimodal bool) {
		t.Helper()
		if err := service.SetProviderModel(1, model); err != nil {
			t.Fatal(err)
		}
		var gotModel string
		var gotMultimodal bool
		if err := db.QueryRow("SELECT model, multimodal FROM providers WHERE id = 1").Scan(&gotModel, &gotMultimodal); err != nil {
			t.Fatal(err)
		}
		if gotModel != model || gotMultimodal != multimodal {
			t.Fatalf("provider = model %q multimodal %v, want %q %v", gotModel, gotMultimodal, model, multimodal)
		}
	}

	assertCurrent("vision-chat", true)
	assertCurrent("text-chat", false)
}

func TestBuildModelSendsImageForDeepSeekFlash(t *testing.T) {
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
		_, _ = w.Write([]byte(`{"id":"test","object":"chat.completion","created":1,"model":"deepseek-flash","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	client, err := buildModel(Provider{
		Name: "test", Kind: "deepseek", BaseURL: server.URL,
		Model: "deepseek-flash", Multimodal: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := model.NewRequest([]model.Message{{
		Role:    model.RoleUser,
		Content: "describe",
		ContentParts: []model.ContentPart{{
			Type:  model.ContentTypeImage,
			Image: &model.Image{Data: []byte("png"), Format: "png"},
		}},
	}})
	responses, err := client.GenerateContent(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	for response := range responses {
		if response.Error != nil {
			t.Fatal(response.Error)
		}
	}

	payload := <-bodyCh
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) == 0 {
		t.Fatalf("messages = %#v", payload["messages"])
	}
	message, ok := messages[len(messages)-1].(map[string]any)
	if !ok {
		t.Fatalf("message = %#v", messages[len(messages)-1])
	}
	content, ok := message["content"].([]any)
	if !ok || len(content) != 2 {
		t.Fatalf("content = %#v, want text and image parts", message["content"])
	}
	imagePart, ok := content[1].(map[string]any)
	if !ok || imagePart["type"] != "image_url" {
		t.Fatalf("image part = %#v", content[1])
	}
	imageURL, ok := imagePart["image_url"].(map[string]any)
	if !ok || imageURL["url"] != "data:image/png;base64,cG5n" {
		t.Fatalf("image_url = %#v", imagePart["image_url"])
	}
}
