package app

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
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
