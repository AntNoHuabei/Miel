package app

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	aguitypes "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"trpc.group/trpc-go/trpc-agent-go/session/inmemory"
	sessionsqlite "trpc.group/trpc-go/trpc-agent-go/session/sqlite"
)

func TestMessagesSnapshotMigratesLegacyHistoryOnce(t *testing.T) {
	db := newSettingsTestDB(t)
	oldStore := store
	store = db
	t.Cleanup(func() { store = oldStore })

	if _, err := db.Exec(`
		INSERT INTO conversations (id, title, created_at, updated_at)
		VALUES (42, 'snapshot', 1, 2);
		INSERT INTO messages (conversation_id, role, content, created_at)
		VALUES (42, 'user', 'hello', 1), (42, 'assistant', 'world', 2)`); err != nil {
		t.Fatal(err)
	}

	sessions := inmemory.NewSessionService()
	t.Cleanup(func() { _ = sessions.Close() })
	service, err := newAgentService(sessions)
	if err != nil {
		t.Fatal(err)
	}

	for attempt := 0; attempt < 2; attempt++ {
		snapshot, err := service.MessagesSnapshot(42)
		if err != nil {
			t.Fatal(err)
		}
		decoded := decodeMessagesSnapshot(t, snapshot)
		if decoded.Type != "MESSAGES_SNAPSHOT" {
			t.Fatalf("snapshot type = %q", decoded.Type)
		}
		if len(decoded.Messages) != 2 {
			t.Fatalf("snapshot messages = %#v, want 2", decoded.Messages)
		}
		if decoded.Messages[0].Role != aguitypes.RoleUser || decoded.Messages[0].Content != "hello" {
			t.Fatalf("first message = %#v", decoded.Messages[0])
		}
		if decoded.Messages[1].Role != aguitypes.RoleAssistant || decoded.Messages[1].Content != "world" {
			t.Fatalf("second message = %#v", decoded.Messages[1])
		}
	}
}

func TestMessagesSnapshotPersistsInSQLiteSession(t *testing.T) {
	db := newSettingsTestDB(t)
	oldStore := store
	store = db
	t.Cleanup(func() { store = oldStore })
	if _, err := db.Exec(`
		INSERT INTO conversations (id, title, created_at, updated_at)
		VALUES (43, 'persistent snapshot', 1, 1);
		INSERT INTO messages (conversation_id, role, content, created_at)
		VALUES (43, 'user', 'persisted', 1)`); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "agui.db")
	openService := func() *AgentService {
		sessionDB, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		sessionDB.SetMaxOpenConns(1)
		sessions, err := sessionsqlite.NewService(sessionDB)
		if err != nil {
			t.Fatal(err)
		}
		service, err := newAgentService(sessions)
		if err != nil {
			t.Fatal(err)
		}
		return service
	}

	first := openService()
	if _, err := first.MessagesSnapshot(43); err != nil {
		t.Fatal(err)
	}
	if err := first.sessions.Close(); err != nil {
		t.Fatal(err)
	}

	second := openService()
	t.Cleanup(func() { _ = second.sessions.Close() })
	snapshot, err := second.MessagesSnapshot(43)
	if err != nil {
		t.Fatal(err)
	}
	decoded := decodeMessagesSnapshot(t, snapshot)
	if len(decoded.Messages) != 1 || decoded.Messages[0].Content != "persisted" {
		t.Fatalf("persisted snapshot = %#v", decoded.Messages)
	}
}

func TestAgentServiceShutdownIsIdempotent(t *testing.T) {
	sessions := inmemory.NewSessionService()
	service, err := newAgentService(sessions)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ServiceShutdown(); err != nil {
		t.Fatal(err)
	}
	if err := service.ServiceShutdown(); err != nil {
		t.Fatal(err)
	}
}

func decodeMessagesSnapshot(t *testing.T, snapshot map[string]any) struct {
	Type     string              `json:"type"`
	Messages []aguitypes.Message `json:"messages"`
} {
	t.Helper()
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Type     string              `json:"type"`
		Messages []aguitypes.Message `json:"messages"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}
