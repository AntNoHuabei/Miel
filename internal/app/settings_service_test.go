package app

import (
	"database/sql"
	"fmt"
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
