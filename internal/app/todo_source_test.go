package app

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTodoTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:todo-test-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if err := migrate(db); err != nil {
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestMigrateAddsTodoSourceColumnToExistingDatabase(t *testing.T) {
	db, err := sql.Open("sqlite", fmt.Sprintf("file:todo-migrate-%d?mode=memory&cache=shared", time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE todos (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		deadline INTEGER NOT NULL DEFAULT 0,
		is_milestone INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL DEFAULT 'pending',
		source TEXT NOT NULL DEFAULT 'manual',
		created_at INTEGER NOT NULL,
		done_at INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}
	var sourceID int64
	if err := db.QueryRow("SELECT source_id FROM todos LIMIT 1").Scan(&sourceID); err != sql.ErrNoRows {
		t.Fatalf("source_id migration query = %v, want sql.ErrNoRows", err)
	}
}

func TestManualTodoStoresNullSourceReference(t *testing.T) {
	db := newTodoTestDB(t)
	service := NewTodoService(db)
	created, err := service.CreateTodo(TodoInput{Title: "manual", Source: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	if created.SourceID != 0 {
		t.Fatalf("source id = %d, want 0", created.SourceID)
	}
	var isNull int
	if err := db.QueryRow("SELECT source_id IS NULL FROM todos WHERE id = ?", created.ID).Scan(&isNull); err != nil {
		t.Fatal(err)
	}
	if isNull != 1 {
		t.Fatal("manual todo source_id is not NULL")
	}
}

func TestSharedClipboardSourceIsRemovedWithLastTodo(t *testing.T) {
	db := newTodoTestDB(t)
	service := NewTodoService(db)
	path := filepath.Join(t.TempDir(), "clipboard.png")
	if err := os.WriteFile(path, []byte("image"), 0o600); err != nil {
		t.Fatal(err)
	}
	items, err := service.createTodosWithSource([]TodoInput{
		{Title: "first", Source: "clipboard"},
		{Title: "second", Source: "clipboard"},
	}, todoSourceInput{Kind: "clipboard_image", FilePath: path, MIMEType: "image/png"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].SourceID == 0 || items[0].SourceID != items[1].SourceID {
		t.Fatalf("todos do not share source: %#v", items)
	}
	if err := service.DeleteTodo(items[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("source removed while still referenced: %v", err)
	}
	if err := service.DeleteTodo(items[1].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("owned source file still exists: %v", err)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM todo_sources").Scan(&count); err != nil || count != 0 {
		t.Fatalf("todo_sources count = %d, err = %v", count, err)
	}
}

func TestScreenshotSourceDoesNotOwnScreenshotFile(t *testing.T) {
	db := newTodoTestDB(t)
	service := NewTodoService(db)
	path := filepath.Join(t.TempDir(), "shot.jpg")
	if err := os.WriteFile(path, []byte("image"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := db.Exec("INSERT INTO screenshots (path, note, created_at) VALUES (?, ?, ?)", path, "note", now())
	if err != nil {
		t.Fatal(err)
	}
	shotID, _ := res.LastInsertId()
	items, err := service.createTodosWithSource([]TodoInput{
		{Title: "from shot", Source: "screenshot"},
		{Title: "second from shot", Source: "screenshot"},
	},
		todoSourceInput{Kind: "screenshot", FilePath: path, MIMEType: "image/jpeg", ScreenshotID: shotID})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].SourceID != items[1].SourceID {
		t.Fatalf("screenshot todos do not share a source: %#v", items)
	}
	source, err := service.GetTodoSource(items[0].SourceID)
	if err != nil {
		t.Fatal(err)
	}
	if !source.Available || source.ScreenshotNote != "note" || source.DataURI == "" {
		t.Fatalf("unexpected source: %#v", source)
	}
	if err := service.DeleteTodo(items[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("screenshot file was removed: %v", err)
	}
	if err := service.DeleteTodo(items[1].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("screenshot file was removed with last source reference: %v", err)
	}
}

func TestConversationToolTodosShareUserMessageSource(t *testing.T) {
	db := newTodoTestDB(t)
	service := NewTodoService(db)
	if _, err := db.Exec("INSERT INTO conversations (id, title, created_at, updated_at) VALUES (7, '计划讨论', 1, 1)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO messages (id, conversation_id, role, content, created_at) VALUES (9, 7, 'user', '记得提交报告', 1)"); err != nil {
		t.Fatal(err)
	}
	source := &todoToolSource{input: todoSourceInput{
		Kind: "conversation", TextContent: "记得提交报告", ConversationID: 7, MessageID: 9,
	}}
	first, err := source.create(service, TodoInput{Title: "提交报告", Source: "chat"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := source.create(service, TodoInput{Title: "检查附件", Source: "chat"})
	if err != nil {
		t.Fatal(err)
	}
	if first.SourceID == 0 || first.SourceID != second.SourceID {
		t.Fatalf("source ids = %d and %d", first.SourceID, second.SourceID)
	}
	detail, err := service.GetTodoSource(first.SourceID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.TextContent != "记得提交报告" || detail.ConversationTitle != "计划讨论" || !detail.Available {
		t.Fatalf("unexpected source: %#v", detail)
	}
}
