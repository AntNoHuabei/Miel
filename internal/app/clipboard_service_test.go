package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfirmClipboardTextTodosSharesTraceableSource(t *testing.T) {
	db := newTodoTestDB(t)
	todo := NewTodoService(db)
	service := &ClipboardService{todo: todo, drafts: make(map[string]clipboardDraftPayload)}
	draftID := "text-draft"
	service.drafts[draftID] = clipboardDraftPayload{
		kind: "clipboard_text", text: "周五提交报告", createdAt: now(),
	}
	created, err := service.ConfirmTodos(ConfirmClipboardTodosReq{
		DraftID: draftID,
		Items: []ExtractedTodo{
			{Title: "提交报告", DueDate: "2026-09-11"},
			{Title: "检查附件"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 2 || created[0].SourceID == 0 || created[0].SourceID != created[1].SourceID {
		t.Fatalf("unexpected todos: %#v", created)
	}
	source, err := todo.GetTodoSource(created[0].SourceID)
	if err != nil {
		t.Fatal(err)
	}
	if source.Kind != "clipboard_text" || source.TextContent != "周五提交报告" || !source.Available {
		t.Fatalf("unexpected source: %#v", source)
	}
	if _, ok := service.drafts[draftID]; ok {
		t.Fatal("confirmed draft was not removed")
	}
}

func TestConfirmClipboardWithoutItemsDiscardsDraft(t *testing.T) {
	service := &ClipboardService{
		todo: NewTodoService(newTodoTestDB(t)), drafts: make(map[string]clipboardDraftPayload),
	}
	service.drafts["empty"] = clipboardDraftPayload{kind: "clipboard_text", text: "nothing"}
	if _, err := service.ConfirmTodos(ConfirmClipboardTodosReq{DraftID: "empty"}); err == nil {
		t.Fatal("ConfirmTodos returned no error")
	}
	if _, ok := service.drafts["empty"]; ok {
		t.Fatal("empty draft was not discarded")
	}
}

func TestConfirmClipboardImageMovesDraftAndRetainsSource(t *testing.T) {
	db := newTodoTestDB(t)
	todo := NewTodoService(db)
	root := t.TempDir()
	draftPath := filepath.Join(root, ".draft", "image.png")
	if err := os.MkdirAll(filepath.Dir(draftPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(draftPath, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := &ClipboardService{
		todo: todo, sourceDir: root, drafts: map[string]clipboardDraftPayload{
			"image-draft": {kind: "clipboard_image", draftPath: draftPath, createdAt: now()},
		},
	}
	created, err := service.ConfirmTodos(ConfirmClipboardTodosReq{
		DraftID: "image-draft",
		Items:   []ExtractedTodo{{Title: "检查图片"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 1 {
		t.Fatalf("created %d todos, want 1", len(created))
	}
	source, err := todo.GetTodoSource(created[0].SourceID)
	if err != nil {
		t.Fatal(err)
	}
	if source.Kind != "clipboard_image" || source.MIMEType != "image/png" {
		t.Fatalf("unexpected source: %#v", source)
	}
	if _, err := os.Stat(source.FilePath); err != nil {
		t.Fatalf("confirmed source file missing: %v", err)
	}
	if _, err := os.Stat(draftPath); !os.IsNotExist(err) {
		t.Fatalf("draft still exists: %v", err)
	}
}

func TestDiscardClipboardImageRemovesDraft(t *testing.T) {
	draftPath := filepath.Join(t.TempDir(), "draft.png")
	if err := os.WriteFile(draftPath, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := &ClipboardService{drafts: map[string]clipboardDraftPayload{
		"discard": {kind: "clipboard_image", draftPath: draftPath},
	}}
	service.DiscardDraft("discard")
	if _, err := os.Stat(draftPath); !os.IsNotExist(err) {
		t.Fatalf("discarded draft still exists: %v", err)
	}
}

func TestConfirmClipboardImageRestoresDraftWhenDatabaseFails(t *testing.T) {
	db := newTodoTestDB(t)
	todo := NewTodoService(db)
	root := t.TempDir()
	draftPath := filepath.Join(root, ".draft", "retry.png")
	if err := os.MkdirAll(filepath.Dir(draftPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(draftPath, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := &ClipboardService{
		todo: todo, sourceDir: root, drafts: map[string]clipboardDraftPayload{
			"retry": {kind: "clipboard_image", draftPath: draftPath, createdAt: now()},
		},
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConfirmTodos(ConfirmClipboardTodosReq{
		DraftID: "retry", Items: []ExtractedTodo{{Title: "will fail"}},
	}); err == nil {
		t.Fatal("ConfirmTodos returned no database error")
	}
	if _, err := os.Stat(draftPath); err != nil {
		t.Fatalf("draft was not restored after failure: %v", err)
	}
}
