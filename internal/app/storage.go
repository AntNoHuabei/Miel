package app

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/adrg/xdg"
	_ "modernc.org/sqlite"
)

// store 持有全局数据库句柄(单连接即可,桌面应用访问量小)。
var store *sql.DB

// dataDir 返回应用数据目录(Windows 下为 %LOCALAPPDATA%\BlankMind)。
func dataDir() string {
	dir := filepath.Join(xdg.DataHome, "BlankMind")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Println("create data dir failed:", err)
	}
	return dir
}

// openStore 打开(必要时创建)SQLite 数据库并执行迁移。
func openStore() error {
	dbPath := filepath.Join(dataDir(), "blankmind.db")
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

-- 服务商启用的模型集合(内置目录模型经开关启用;自定义模型手输加入)
CREATE TABLE IF NOT EXISTS provider_models (
	provider_id INTEGER NOT NULL,
	model       TEXT NOT NULL,
	label       TEXT NOT NULL DEFAULT '',
	custom      INTEGER NOT NULL DEFAULT 0,
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
	// 旧数据回填:为已有服务商补齐启用模型行(以其当前 model 为准)
	if _, err := db.Exec(`
		INSERT OR IGNORE INTO provider_models (provider_id, model, label, custom, created_at)
		SELECT id, model, '', 0, created_at FROM providers
		WHERE model <> ''`); err != nil {
		return fmt.Errorf("backfill provider_models: %w", err)
	}
	return nil
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
