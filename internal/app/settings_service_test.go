package app

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func newSettingsTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:settings-test-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestListProvidersDoesNotDeadlockWhenMigratingAPIKey(t *testing.T) {
	db := newSettingsTestDB(t)
	if _, err := db.Exec(`
		INSERT INTO providers (name, kind, base_url, api_key, model, is_default, created_at)
		VALUES ('test', 'custom', 'http://localhost/v1', 'plain-secret', 'test-model', 1, 1)`); err != nil {
		t.Fatal(err)
	}

	oldSet := credentialSet
	oldGet := credentialGet
	credentialSet = func(_, _ string) error { return nil }
	credentialGet = func(string) (string, error) { return "", nil }
	t.Cleanup(func() {
		credentialSet = oldSet
		credentialGet = oldGet
	})

	result := make(chan error, 1)
	go func() {
		providers, err := NewSettingsService(db).ListProviders()
		if err == nil && (len(providers) != 1 || providers[0].APIKey != "plain-secret") {
			err = fmt.Errorf("unexpected providers: %#v", providers)
		}
		result <- err
	}()

	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("ListProviders blocked while migrating API key")
	}

	var stored string
	if err := db.QueryRow("SELECT api_key FROM providers LIMIT 1").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "" {
		t.Fatalf("api_key = %q, want cleared after credential migration", stored)
	}
}

func TestProviderCatalogMethodsReturn(t *testing.T) {
	service := &SettingsService{}
	catalog, err := service.ModelCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog) == 0 {
		t.Fatal("ModelCatalog returned no providers")
	}
	if templates := service.ProviderTemplates(); len(templates) == 0 {
		t.Fatal("ProviderTemplates returned no providers")
	}
}

func TestProviderModelsReturns(t *testing.T) {
	db := newSettingsTestDB(t)
	if _, err := db.Exec(`
		INSERT INTO provider_models (provider_id, model, label, custom, created_at)
		VALUES (7, 'test-model', 'Test Model', 0, 1)`); err != nil {
		t.Fatal(err)
	}
	models, err := NewSettingsService(db).ProviderModels(7)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].Model != "test-model" {
		t.Fatalf("ProviderModels() = %#v", models)
	}
}

func TestModelMutationsNotifyAndRefreshOptions(t *testing.T) {
	db := newSettingsTestDB(t)
	if _, err := db.Exec(`
		INSERT INTO providers (id, name, kind, base_url, api_key, model, is_default, created_at)
		VALUES (1, 'test', 'custom', 'http://localhost/v1', '', 'model-a', 1, 1);
		INSERT INTO provider_models (provider_id, model, label, custom, created_at)
		VALUES (1, 'model-a', 'Model A', 1, 1);`); err != nil {
		t.Fatal(err)
	}

	service := NewSettingsService(db)
	events := 0
	service.setNotify(func(name string, _ any) {
		if name == "models.changed" {
			events++
		}
	})

	if err := service.EnableModel(1, "model-b", "Model B", true); err != nil {
		t.Fatal(err)
	}
	options, err := service.ModelOptions()
	if err != nil {
		t.Fatal(err)
	}
	if len(options) != 2 {
		t.Fatalf("ModelOptions after enable returned %d items, want 2", len(options))
	}

	if err := service.SetProviderModel(1, "model-b"); err != nil {
		t.Fatal(err)
	}
	if err := service.DisableModel(1, "model-a"); err != nil {
		t.Fatal(err)
	}
	options, err = service.ModelOptions()
	if err != nil {
		t.Fatal(err)
	}
	if len(options) != 1 || options[0].Model != "model-b" || !options[0].IsDefault {
		t.Fatalf("ModelOptions after mutations = %#v", options)
	}
	if events != 3 {
		t.Fatalf("models.changed events = %d, want 3", events)
	}
}

func TestSetProviderModelPersistsAcrossDatabaseReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "settings.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO providers (id, name, kind, base_url, api_key, model, is_default, created_at)
		VALUES
			(1, 'first', 'custom', 'http://localhost/first', '', 'model-a', 1, 1),
			(2, 'second', 'custom', 'http://localhost/second', '', 'model-b', 0, 1);
		INSERT INTO provider_models (provider_id, model, label, custom, created_at)
		VALUES
			(1, 'model-a', 'Model A', 1, 1),
			(2, 'model-b', 'Model B', 1, 1);`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := NewSettingsService(db).SetProviderModel(2, "model-b"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	provider, err := NewSettingsService(reopened).DefaultProvider()
	if err != nil {
		t.Fatal(err)
	}
	if provider.ID != 2 || provider.Model != "model-b" || !provider.IsDefault {
		t.Fatalf("restored provider = %#v, want provider 2 model-b as default", provider)
	}
}

func TestDefaultModelSupportsVisionUsesCurrentCatalogModel(t *testing.T) {
	db := newSettingsTestDB(t)
	if _, err := db.Exec(`
		INSERT INTO providers (id, name, kind, base_url, api_key, model, multimodal, is_default, created_at)
		VALUES (1, 'DeepSeek', 'deepseek', 'https://api.deepseek.com', '',
			'deepseek-flash', 0, 1, 1)`); err != nil {
		t.Fatal(err)
	}

	supported, err := NewSettingsService(db).DefaultModelSupportsVision()
	if err != nil {
		t.Fatal(err)
	}
	if !supported {
		t.Fatal("DeepSeek Flash should be detected as a vision model")
	}
}

func TestDeepSeekCatalogOnlyContainsFlash(t *testing.T) {
	provider, found := catalogLookup("deepseek")
	if !found {
		t.Fatal("DeepSeek catalog entry is missing")
	}
	if len(provider.Models) != 1 || provider.Models[0].ID != "deepseek-flash" {
		t.Fatalf("DeepSeek catalog models = %#v, want only deepseek-flash", provider.Models)
	}
	if !provider.Models[0].Multimodal {
		t.Fatal("deepseek-flash should support image input")
	}
}
