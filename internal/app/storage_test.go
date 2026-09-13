package app

import (
	"database/sql"
	"fmt"
	"testing"
	"time"
)

func TestMigrateKeepsOnlyDeepSeekFlash(t *testing.T) {
	dsn := fmt.Sprintf("file:storage-test-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO providers
			(id, name, kind, base_url, api_key, model, multimodal, is_default, created_at)
		VALUES
			(1, 'DeepSeek primary', 'deepseek', 'https://api.deepseek.com', '', 'deepseek-v4-pro', 0, 1, 10),
			(2, 'DeepSeek secondary', 'DeepSeek', 'https://api.deepseek.com', '', 'deepseek-chat', 0, 0, 20),
			(3, 'OpenAI', 'openai', 'https://api.openai.com/v1', '', 'gpt-4o', 1, 0, 30);
		INSERT INTO provider_models (provider_id, model, label, custom, created_at)
		VALUES
			(1, 'deepseek-v4-pro', 'DeepSeek V4 Pro', 0, 10),
			(1, 'deepseek-reasoner', 'DeepSeek Reasoner', 1, 11),
			(2, 'deepseek-chat', 'DeepSeek Chat', 0, 20),
			(3, 'gpt-4o', 'GPT-4o', 0, 30);`); err != nil {
		t.Fatal(err)
	}

	// Run twice to prove startup migration remains idempotent.
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}

	rows, err := db.Query(`
		SELECT id, model, multimodal FROM providers
		WHERE lower(trim(kind)) = 'deepseek' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close() //nolint:errcheck
	count := 0
	for rows.Next() {
		var id int64
		var model string
		var multimodal bool
		if err := rows.Scan(&id, &model, &multimodal); err != nil {
			t.Fatal(err)
		}
		if model != "deepseek-flash" || !multimodal {
			t.Fatalf("DeepSeek provider %d = model %q, multimodal %v", id, model, multimodal)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("DeepSeek provider count = %d, want 2", count)
	}

	var model, label string
	var custom bool
	var modelCount int
	if err := db.QueryRow(`
		SELECT COUNT(*), MIN(model), MIN(label), MIN(custom)
		FROM provider_models WHERE provider_id IN (1, 2)`).Scan(
		&modelCount, &model, &label, &custom,
	); err != nil {
		t.Fatal(err)
	}
	if modelCount != 2 || model != "deepseek-flash" || label != "DeepSeek V4.1 Flash" || custom {
		t.Fatalf("DeepSeek models = count %d, model %q, label %q, custom %v", modelCount, model, label, custom)
	}

	var openAIModel string
	var openAIMultimodal bool
	if err := db.QueryRow("SELECT model, multimodal FROM providers WHERE id = 3").Scan(
		&openAIModel, &openAIMultimodal,
	); err != nil {
		t.Fatal(err)
	}
	if openAIModel != "gpt-4o" || !openAIMultimodal {
		t.Fatalf("OpenAI provider changed to model %q, multimodal %v", openAIModel, openAIMultimodal)
	}
	var openAIModelCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM provider_models WHERE provider_id = 3 AND model = 'gpt-4o'").Scan(&openAIModelCount); err != nil {
		t.Fatal(err)
	}
	if openAIModelCount != 1 {
		t.Fatalf("OpenAI model count = %d, want 1", openAIModelCount)
	}
}

func TestMigrateAddsAndBackfillsProviderModelMultimodal(t *testing.T) {
	dsn := fmt.Sprintf("file:provider-model-multimodal-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.Exec(`
		CREATE TABLE providers (
			id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, kind TEXT NOT NULL,
			base_url TEXT NOT NULL DEFAULT '', api_key TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '',
			multimodal INTEGER NOT NULL DEFAULT 0, is_default INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL
		);
		CREATE TABLE provider_models (
			provider_id INTEGER NOT NULL, model TEXT NOT NULL, label TEXT NOT NULL DEFAULT '',
			custom INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL,
			PRIMARY KEY (provider_id, model)
		);
		INSERT INTO providers (id, name, kind, model, multimodal, created_at)
		VALUES (1, 'custom', 'custom', 'vision-model', 1, 1);
		INSERT INTO provider_models (provider_id, model, custom, created_at)
		VALUES (1, 'vision-model', 1, 1), (1, 'text-model', 1, 1);`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}

	var vision, text bool
	if err := db.QueryRow("SELECT multimodal FROM provider_models WHERE model = 'vision-model'").Scan(&vision); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT multimodal FROM provider_models WHERE model = 'text-model'").Scan(&text); err != nil {
		t.Fatal(err)
	}
	if !vision || text {
		t.Fatalf("backfilled capabilities = vision %v text %v", vision, text)
	}
}
