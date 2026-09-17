package app

import (
	"database/sql"
	"fmt"
	"testing"
	"time"
)

func TestMigrateRecoversInterruptedPlanRuns(t *testing.T) {
	dsn := fmt.Sprintf("file:plan-recovery-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO plans (id, conversation_id, status, current_revision, created_at, updated_at)
		VALUES (1, 1, 'executing', 1, 1, 1);
		INSERT INTO plan_runs (id, plan_id, revision, provider_id, model, status, started_at)
		VALUES (1, 1, 1, 1, 'model', 'executing', 1)`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}
	var planStatus, runStatus string
	if err := db.QueryRow("SELECT status FROM plans WHERE id = 1").Scan(&planStatus); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT status FROM plan_runs WHERE id = 1").Scan(&runStatus); err != nil {
		t.Fatal(err)
	}
	if planStatus != planStatusInterrupted || runStatus != planStatusInterrupted {
		t.Fatalf("recovered statuses = %q, %q", planStatus, runStatus)
	}
}

func TestMigrateAddsAndBackfillsMessagePlanLinks(t *testing.T) {
	dsn := fmt.Sprintf("file:message-plan-links-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`
		CREATE TABLE messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT, conversation_id INTEGER NOT NULL,
			role TEXT NOT NULL, content TEXT NOT NULL DEFAULT '',
			message_type TEXT NOT NULL DEFAULT 'chat', created_at INTEGER NOT NULL
		)`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO conversations (id, title, created_at, updated_at) VALUES (1, 'legacy', 1, 1);
		INSERT INTO messages (id, conversation_id, role, content, message_type, created_at)
		VALUES (1, 1, 'user', 'plan', 'plan_request', 1),
		       (2, 1, 'assistant', '# Plan', 'plan_response', 2);
		INSERT INTO plans (id, conversation_id, status, current_revision, created_at, updated_at)
		VALUES (1, 1, 'pending', 1, 1, 2);
		INSERT INTO plan_revisions (plan_id, revision, content, user_message_id, assistant_message_id, provider_id, model, created_at)
		VALUES (1, 1, '# Plan', 1, 2, 1, 'model', 2)`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}
	for _, messageID := range []int{1, 2} {
		var planID int64
		var revision int
		if err := db.QueryRow("SELECT plan_id, plan_revision FROM messages WHERE id = ?", messageID).Scan(&planID, &revision); err != nil {
			t.Fatal(err)
		}
		if planID != 1 || revision != 1 {
			t.Fatalf("message %d plan link = %d:%d", messageID, planID, revision)
		}
	}
}

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

func TestMigrateUpgradesLegacyVolcengineCodingProvider(t *testing.T) {
	dsn := fmt.Sprintf("file:volcengine-plan-migration-%d?mode=memory&cache=shared", time.Now().UnixNano())
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
			(1, '我的火山套餐', 'volcengine-coding', 'https://ark.cn-beijing.volces.com/api/coding/v3',
			 'secret-key', 'ark-code-latest', 0, 1, 10),
			(2, '自定义地址', 'volcengine-coding', 'http://127.0.0.1:8080/v1',
			 'other-key', 'custom-model', 1, 0, 20);
		INSERT INTO provider_models (provider_id, model, label, custom, multimodal, created_at)
		VALUES (1, 'ark-code-latest', 'Ark Code Latest', 0, 0, 10);`); err != nil {
		t.Fatal(err)
	}

	// Repeated startup must not alter the migrated data further.
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}

	var name, kind, baseURL, apiKey, model string
	if err := db.QueryRow(`
		SELECT name, kind, base_url, api_key, model FROM providers WHERE id = 1`).Scan(
		&name, &kind, &baseURL, &apiKey, &model,
	); err != nil {
		t.Fatal(err)
	}
	if name != "我的火山套餐" || kind != "volcengine-plan" ||
		baseURL != "https://ark.cn-beijing.volces.com/api/plan/v3" ||
		apiKey != "secret-key" || model != "ark-code-latest" {
		t.Fatalf("migrated provider = name %q kind %q url %q key %q model %q",
			name, kind, baseURL, apiKey, model)
	}

	var customKind, customURL string
	if err := db.QueryRow("SELECT kind, base_url FROM providers WHERE id = 2").Scan(&customKind, &customURL); err != nil {
		t.Fatal(err)
	}
	if customKind != "volcengine-plan" || customURL != "http://127.0.0.1:8080/v1" {
		t.Fatalf("custom endpoint = kind %q url %q", customKind, customURL)
	}

	var modelCount int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM provider_models
		WHERE provider_id = 1 AND model = 'ark-code-latest'`).Scan(&modelCount); err != nil {
		t.Fatal(err)
	}
	if modelCount != 1 {
		t.Fatalf("preserved model rows = %d, want 1", modelCount)
	}
}
