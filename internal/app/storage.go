package app

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// store 持有全局数据库句柄(单连接即可,桌面应用访问量小)。
var store *sql.DB

// dataDir 返回应用数据目录(Windows 下为 %LOCALAPPDATA%\BlankMind)。
func dataDir() string {
	dir, err := appDirectories.Ensure(DirectoryRoot)
	if err != nil {
		fmt.Println("create data dir failed:", err)
		return appDirectories.Root()
	}
	return dir
}

// openStore 打开(必要时创建)SQLite 数据库并执行迁移。
func openStore() error {
	dbPath := appDirectories.DatabasePath("blankmind.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		return fmt.Errorf("ping db: %w", err)
	}
	store = db
	return migrate(db)
}

const schema = `
CREATE TABLE IF NOT EXISTS providers (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	name        TEXT NOT NULL,
	kind        TEXT NOT NULL,
	base_url    TEXT NOT NULL DEFAULT '',
	api_key     TEXT NOT NULL DEFAULT '',
	model       TEXT NOT NULL DEFAULT '',
	multimodal  INTEGER NOT NULL DEFAULT 0,
	is_default  INTEGER NOT NULL DEFAULT 0,
	created_at  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS workspaces (
	path       TEXT PRIMARY KEY,
	name       TEXT NOT NULL,
	created_at INTEGER NOT NULL
);

-- 服务商启用的模型集合(内置目录模型经开关启用;自定义模型手输加入)
CREATE TABLE IF NOT EXISTS provider_models (
	provider_id INTEGER NOT NULL,
	model       TEXT NOT NULL,
	label       TEXT NOT NULL DEFAULT '',
	custom      INTEGER NOT NULL DEFAULT 0,
	multimodal  INTEGER NOT NULL DEFAULT 0,
	created_at  INTEGER NOT NULL,
	PRIMARY KEY (provider_id, model)
);

CREATE TABLE IF NOT EXISTS todos (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	title       TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	deadline    INTEGER NOT NULL DEFAULT 0,
	is_milestone INTEGER NOT NULL DEFAULT 0,
	status      TEXT NOT NULL DEFAULT 'pending',
	source      TEXT NOT NULL DEFAULT 'manual',
	source_id   INTEGER DEFAULT NULL,
	created_at  INTEGER NOT NULL,
	done_at     INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS events (
	id      INTEGER PRIMARY KEY AUTOINCREMENT,
	ts      INTEGER NOT NULL,
	type    TEXT NOT NULL,
	summary TEXT NOT NULL DEFAULT '',
	ref_id  INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS conversations (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	title      TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS messages (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	conversation_id INTEGER NOT NULL,
	role            TEXT NOT NULL,
	content         TEXT NOT NULL DEFAULT '',
	created_at      INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS message_attachments (
	id             TEXT PRIMARY KEY,
	message_id     INTEGER NOT NULL,
	kind           TEXT NOT NULL DEFAULT 'image',
	file_path      TEXT NOT NULL,
	thumbnail_path TEXT NOT NULL,
	mime_type      TEXT NOT NULL,
	original_name  TEXT NOT NULL DEFAULT '',
	width          INTEGER NOT NULL DEFAULT 0,
	height         INTEGER NOT NULL DEFAULT 0,
	size_bytes     INTEGER NOT NULL DEFAULT 0,
	position       INTEGER NOT NULL DEFAULT 0,
	created_at     INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS todo_sources (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	kind            TEXT NOT NULL,
	text_content    TEXT NOT NULL DEFAULT '',
	file_path       TEXT NOT NULL DEFAULT '',
	mime_type       TEXT NOT NULL DEFAULT '',
	conversation_id INTEGER NOT NULL DEFAULT 0,
	message_id      INTEGER NOT NULL DEFAULT 0,
	screenshot_id   INTEGER NOT NULL DEFAULT 0,
	created_at      INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS message_metrics (
	message_id        INTEGER PRIMARY KEY,
	agui_message_id   TEXT NOT NULL DEFAULT '',
	model             TEXT NOT NULL DEFAULT '',
	prompt_tokens     INTEGER NOT NULL DEFAULT 0,
	completion_tokens INTEGER NOT NULL DEFAULT 0,
	total_tokens      INTEGER NOT NULL DEFAULT 0,
	reasoning_tokens  INTEGER NOT NULL DEFAULT 0,
	cached_tokens     INTEGER NOT NULL DEFAULT 0,
	duration_ms       INTEGER NOT NULL DEFAULT 0,
	first_token_ms    INTEGER NOT NULL DEFAULT 0,
	tokens_per_second REAL NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS screenshots (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	path       TEXT NOT NULL DEFAULT '',
	note       TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL
);
`

// migrate 执行建表(幂等)。
func migrate(db *sql.DB) error {
	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if err := ensureColumn(db, "todos", "source_id", "INTEGER DEFAULT NULL"); err != nil {
		return fmt.Errorf("migrate todos source: %w", err)
	}
	if err := ensureColumn(db, "provider_models", "multimodal", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return fmt.Errorf("migrate provider model multimodal: %w", err)
	}
	if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_todos_source_id ON todos(source_id)"); err != nil {
		return fmt.Errorf("index todos source: %w", err)
	}
	if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_message_attachments_message_id ON message_attachments(message_id)"); err != nil {
		return fmt.Errorf("index message attachments: %w", err)
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin provider migration: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// 旧数据回填:为已有服务商补齐启用模型行(以其当前 model 为准)
	if _, err := tx.Exec(`
		INSERT OR IGNORE INTO provider_models (provider_id, model, label, custom, created_at)
		SELECT id, model, '', 0, created_at FROM providers
		WHERE model <> ''`); err != nil {
		return fmt.Errorf("backfill provider_models: %w", err)
	}
	// 旧库只有服务商当前模型的能力标识，将它回填到对应模型行。
	if _, err := tx.Exec(`
		UPDATE provider_models
		SET multimodal = 1
		WHERE multimodal = 0 AND EXISTS (
			SELECT 1 FROM providers p
			WHERE p.id = provider_models.provider_id
			  AND p.model = provider_models.model
			  AND p.multimodal = 1
		)`); err != nil {
		return fmt.Errorf("backfill provider model multimodal: %w", err)
	}
	// DeepSeek 当前仅提供 Flash。清理历史 Pro/Reasoner/Chat 等型号，避免旧数据库
	// 继续把已下线型号暴露给模型选择器。
	if _, err := tx.Exec(`
		UPDATE providers
		SET model = 'deepseek-flash', multimodal = 1
		WHERE lower(trim(kind)) = 'deepseek'`); err != nil {
		return fmt.Errorf("normalize deepseek providers: %w", err)
	}
	if _, err := tx.Exec(`
		DELETE FROM provider_models
		WHERE provider_id IN (
			SELECT id FROM providers WHERE lower(trim(kind)) = 'deepseek'
		)`); err != nil {
		return fmt.Errorf("remove legacy deepseek models: %w", err)
	}
	if _, err := tx.Exec(`
		INSERT INTO provider_models (provider_id, model, label, custom, multimodal, created_at)
		SELECT id, 'deepseek-flash', 'DeepSeek V4.1 Flash', 0, 1, created_at
		FROM providers WHERE lower(trim(kind)) = 'deepseek'`); err != nil {
		return fmt.Errorf("add deepseek flash model: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit provider migration: %w", err)
	}
	return nil
}

func ensureColumn(db *sql.DB, table, column, definition string) error {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == column {
			found = true
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = db.Exec("ALTER TABLE " + table + " ADD COLUMN " + column + " " + definition)
	return err
}

// now 返回当前 unix 秒。
func now() int64 { return time.Now().Unix() }

// insertEvent 追加一条操作日志并返回其 ID。
func insertEvent(db *sql.DB, typ, summary string, refID int64) (int64, error) {
	res, err := db.Exec(
		"INSERT INTO events (ts, type, summary, ref_id) VALUES (?, ?, ?, ?)",
		now(), typ, summary, refID,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}
