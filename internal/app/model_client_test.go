package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

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
