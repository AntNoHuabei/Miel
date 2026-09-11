package app

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
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

func TestMessagesSnapshotAttachesPersistedAssistantMetrics(t *testing.T) {
	db := newSettingsTestDB(t)
	oldStore := store
	store = db
	t.Cleanup(func() { store = oldStore })
	if _, err := db.Exec(`
		INSERT INTO conversations (id, title, created_at, updated_at)
		VALUES (44, 'metrics snapshot', 1, 2);
		INSERT INTO messages (id, conversation_id, role, content, created_at)
		VALUES (91, 44, 'user', 'hello', 1), (92, 44, 'assistant', 'world', 2);
		INSERT INTO message_metrics (
			message_id, agui_message_id, model, prompt_tokens, completion_tokens,
			total_tokens, reasoning_tokens, cached_tokens, duration_ms,
			first_token_ms, tokens_per_second
		) VALUES (92, 'm92', 'test-model', 12, 8, 20, 3, 4, 1500, 250, 6.4)`); err != nil {
		t.Fatal(err)
	}

	sessions := inmemory.NewSessionService()
	t.Cleanup(func() { _ = sessions.Close() })
	service, err := newAgentService(sessions)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.MessagesSnapshot(44)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Messages []struct {
			ID      string       `json:"id"`
			Metrics *ChatMetrics `json:"metrics"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Messages) != 2 || decoded.Messages[0].Metrics != nil {
		t.Fatalf("snapshot messages = %#v", decoded.Messages)
	}
	metrics := decoded.Messages[1].Metrics
	if decoded.Messages[1].ID != "m92" || metrics == nil || metrics.TotalTokens != 20 || metrics.TokensPerSecond != 6.4 {
		t.Fatalf("assistant metrics = %#v", decoded.Messages[1])
	}
}

func TestMessagesSnapshotKeepsImageMessageOrderAndRedactsOriginal(t *testing.T) {
	attachmentService := newAttachmentTestService(t)
	oldStore := store
	store = attachmentService.db
	t.Cleanup(func() { store = oldStore })

	sessions := inmemory.NewSessionService()
	t.Cleanup(func() { _ = sessions.Close() })
	service, err := newAgentService(sessions)
	if err != nil {
		t.Fatal(err)
	}
	service.attachments = attachmentService

	draft, err := attachmentService.stageBytes(testPNG(t, 6, 4), "context.png")
	if err != nil {
		t.Fatal(err)
	}
	conversationID, userMessageID, err := service.saveUserMessage(0, "看看这张图", []string{draft.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.saveMessage(conversationID, "assistant", "看到了", "assistant-1", nil); err != nil {
		t.Fatal(err)
	}

	snapshot, err := service.MessagesSnapshot(conversationID)
	if err != nil {
		t.Fatal(err)
	}
	messages, ok := snapshot["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("snapshot messages = %#v", snapshot["messages"])
	}
	first, ok := messages[0].(map[string]any)
	if !ok || first["id"] != "m"+strconv.FormatInt(userMessageID, 10) || first["role"] != "user" {
		t.Fatalf("first message = %#v", messages[0])
	}
	attachments, ok := first["attachments"].([]MessageAttachment)
	if !ok || len(attachments) != 1 || !strings.HasPrefix(attachments[0].ThumbnailDataURI, "data:image/png;base64,") {
		t.Fatalf("snapshot attachments = %#v", first["attachments"])
	}
	if content, ok := first["content"].([]any); ok {
		for _, part := range content {
			if binary, ok := part.(map[string]any); ok && binary["data"] != nil {
				t.Fatalf("original image data leaked into snapshot: %#v", binary)
			}
		}
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
