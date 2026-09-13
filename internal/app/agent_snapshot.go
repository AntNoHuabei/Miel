package app

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/server/agui/adapter"
	aguirunner "trpc.group/trpc-go/trpc-agent-go/server/agui/runner"
	"trpc.group/trpc-go/trpc-agent-go/session"
	sessionsqlite "trpc.group/trpc-go/trpc-agent-go/session/sqlite"
)

const (
	aguiAppName = "blankmind-app"
	aguiUserID  = "user"
	aguiTrack   = session.Track("agui")
)

// NewAgentService 创建持久化 AG-UI 会话服务。模型 runner 仍按每轮配置动态构建。
func NewAgentService(memoryRuntime *memoryRuntime, attachments *ChatAttachmentService) (*AgentService, error) {
	db, err := sql.Open("sqlite", appDirectories.DatabasePath("agui.db"))
	if err != nil {
		return nil, fmt.Errorf("open AG-UI session store: %w", err)
	}
	db.SetMaxOpenConns(1)
	sessions, err := sessionsqlite.NewService(db)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize AG-UI session store: %w", err)
	}
	service, err := newAgentService(sessions)
	if err != nil {
		return nil, err
	}
	service.memory = memoryRuntime
	service.attachments = attachments
	return service, nil
}

func newAgentService(sessions session.Service) (*AgentService, error) {
	if sessions == nil {
		return nil, errors.New("AG-UI session service is nil")
	}
	base := runner.NewRunner(
		aguiAppName,
		llmagent.New("blankmind-snapshot"),
		runner.WithSessionService(sessions),
	)
	protocolRunner := aguirunner.New(
		base,
		aguirunner.WithAppName(aguiAppName),
		aguirunner.WithSessionService(sessions),
	)
	snapshotter, ok := protocolRunner.(aguirunner.MessagesSnapshotter)
	if !ok {
		_ = base.Close()
		_ = sessions.Close()
		return nil, errors.New("AG-UI runner does not support message snapshots")
	}
	return &AgentService{sessions: sessions, snapshotter: snapshotter, snapshotRun: base}, nil
}

func aguiSessionKey(conversationID int64) session.Key {
	return session.Key{
		AppName:   aguiAppName,
		UserID:    aguiUserID,
		SessionID: "conv-" + strconv.FormatInt(conversationID, 10),
	}
}

// MessagesSnapshot 通过 AG-UI MessagesSnapshot 重放持久化 track events。
// 返回值保持标准 MESSAGES_SNAPSHOT JSON 结构,前端可直接渲染 messages。
func (s *AgentService) MessagesSnapshot(conversationID int64) (map[string]any, error) {
	if conversationID <= 0 {
		return nil, errors.New("会话 ID 无效")
	}
	if s.snapshotter == nil {
		return nil, errors.New("AG-UI snapshot runner 未初始化")
	}
	ctx := context.Background()
	history, err := s.loadMessages(conversationID)
	if err != nil {
		return nil, err
	}
	if err := s.ensureAGUIHistory(ctx, conversationID, history); err != nil {
		return nil, err
	}
	stream, err := s.snapshotter.MessagesSnapshot(ctx, &adapter.RunAgentInput{
		ThreadID: aguiSessionKey(conversationID).SessionID,
		RunID:    "snapshot-" + strconv.FormatInt(time.Now().UnixNano(), 10),
	})
	if err != nil {
		return nil, err
	}
	var result map[string]any
	var streamErr error
	for event := range stream {
		switch typed := event.(type) {
		case *aguievents.MessagesSnapshotEvent:
			payload, err := typed.ToJSON()
			if err != nil {
				streamErr = err
				continue
			}
			if err := json.Unmarshal(payload, &result); err != nil {
				streamErr = err
			}
		case *aguievents.RunErrorEvent:
			streamErr = errors.New(typed.Message)
		}
	}
	if streamErr != nil {
		return nil, streamErr
	}
	if result == nil {
		return nil, errors.New("AG-UI 未返回 MESSAGES_SNAPSHOT")
	}
	if err := mergePersistedUserMessages(result, history); err != nil {
		return nil, err
	}
	metrics, err := loadConversationMessageMetrics(conversationID)
	if err != nil {
		return nil, err
	}
	attachMessageMetrics(result, metrics)
	if err := s.attachSnapshotAttachments(result, history); err != nil {
		return nil, err
	}
	redactAGUIBinaryContent(result)
	return result, nil
}

func mergePersistedUserMessages(snapshot map[string]any, history []ChatMessage) error {
	messages, ok := snapshot["messages"].([]any)
	if !ok {
		messages = []any{}
	}
	existing := make(map[string]struct{}, len(messages))
	for _, raw := range messages {
		if message, ok := raw.(map[string]any); ok {
			if id, ok := message["id"].(string); ok {
				existing[id] = struct{}{}
			}
		}
	}
	for _, persisted := range history {
		if persisted.Role != "user" {
			continue
		}
		id := "m" + strconv.FormatInt(persisted.ID, 10)
		if _, ok := existing[id]; ok {
			continue
		}
		encoded, err := json.Marshal(map[string]any{
			"id":      id,
			"role":    persisted.Role,
			"content": persisted.Content,
		})
		if err != nil {
			return err
		}
		var raw map[string]any
		if err := json.Unmarshal(encoded, &raw); err != nil {
			return err
		}
		messages = append(messages, raw)
	}
	snapshot["messages"] = messages
	return nil
}

func loadConversationMessageMetrics(conversationID int64) (map[string]ChatMetrics, error) {
	rows, err := store.Query(`
		SELECT mm.agui_message_id, mm.model, mm.prompt_tokens, mm.completion_tokens,
			mm.total_tokens, mm.reasoning_tokens, mm.cached_tokens, mm.duration_ms,
			mm.first_token_ms, mm.tokens_per_second
		FROM message_metrics mm
		JOIN messages m ON m.id = mm.message_id
		WHERE m.conversation_id = ? AND mm.agui_message_id <> ''`, conversationID)
	if err != nil {
		return nil, fmt.Errorf("load message metrics: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	metrics := make(map[string]ChatMetrics)
	for rows.Next() {
		var messageID string
		var item ChatMetrics
		if err := rows.Scan(
			&messageID, &item.Model, &item.PromptTokens, &item.CompletionTokens,
			&item.TotalTokens, &item.ReasoningTokens, &item.CachedTokens,
			&item.DurationMs, &item.FirstTokenMs, &item.TokensPerSecond,
		); err != nil {
			return nil, fmt.Errorf("scan message metrics: %w", err)
		}
		metrics[messageID] = item
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate message metrics: %w", err)
	}
	return metrics, nil
}

func attachMessageMetrics(snapshot map[string]any, metrics map[string]ChatMetrics) {
	messages, ok := snapshot["messages"].([]any)
	if !ok {
		return
	}
	for _, raw := range messages {
		message, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		messageID, _ := message["id"].(string)
		if item, exists := metrics[messageID]; exists {
			message["metrics"] = item
		}
	}
}

func (s *AgentService) attachSnapshotAttachments(snapshot map[string]any, history []ChatMessage) error {
	byID := make(map[string][]MessageAttachment)
	for _, message := range history {
		if len(message.Attachments) == 0 {
			continue
		}
		items := make([]MessageAttachment, 0, len(message.Attachments))
		for _, attachment := range message.Attachments {
			thumbnail, err := readLimitedFile(attachment.ThumbnailPath, maxChatAttachmentBytes)
			if err != nil {
				return fmt.Errorf("读取聊天图片缩略图失败: %w", err)
			}
			attachment.ThumbnailDataURI = "data:image/png;base64," + base64.StdEncoding.EncodeToString(thumbnail)
			items = append(items, attachment)
		}
		byID["m"+strconv.FormatInt(message.ID, 10)] = items
	}
	messages, ok := snapshot["messages"].([]any)
	if !ok {
		return nil
	}
	for _, raw := range messages {
		message, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		messageID, _ := message["id"].(string)
		if attachments := byID[messageID]; len(attachments) > 0 {
			message["attachments"] = attachments
		}
	}
	return nil
}

// redactAGUIBinaryContent keeps original image payloads out of Wails events and snapshots.
func redactAGUIBinaryContent(value any) {
	switch typed := value.(type) {
	case map[string]any:
		contentType, _ := typed["type"].(string)
		mimeType, _ := typed["mimeType"].(string)
		if contentType == "binary" && strings.HasPrefix(strings.ToLower(mimeType), "image/") {
			delete(typed, "data")
		}
		if contentType == "image" {
			if source, ok := typed["source"].(map[string]any); ok && source["type"] == "data" {
				delete(source, "value")
			}
		}
		for _, child := range typed {
			redactAGUIBinaryContent(child)
		}
	case []any:
		for _, child := range typed {
			redactAGUIBinaryContent(child)
		}
	}
}

type trackEventReader interface {
	GetTrackEvents(context.Context, session.Key, session.Track, ...session.Option) (*session.TrackEvents, error)
}

// ensureAGUIHistory 仅为尚无 AG-UI track 的旧会话迁移原有纯文本消息。
func (s *AgentService) ensureAGUIHistory(ctx context.Context, conversationID int64, history []ChatMessage) error {
	s.historyMu.Lock()
	defer s.historyMu.Unlock()

	reader, ok := s.sessions.(trackEventReader)
	if !ok {
		return errors.New("AG-UI session service does not support track reads")
	}
	key := aguiSessionKey(conversationID)
	tracked, err := reader.GetTrackEvents(ctx, key, aguiTrack)
	if err != nil {
		return fmt.Errorf("read AG-UI history: %w", err)
	}
	if tracked != nil && len(tracked.Events) > 0 {
		return nil
	}
	if len(history) == 0 {
		return nil
	}

	sess, err := s.sessions.GetSession(ctx, key)
	if err != nil {
		return fmt.Errorf("get AG-UI session: %w", err)
	}
	if sess == nil {
		sess, err = s.sessions.CreateSession(ctx, key, session.StateMap{})
		if err != nil {
			return fmt.Errorf("create AG-UI session: %w", err)
		}
	}
	writer, ok := s.sessions.(session.TrackService)
	if !ok {
		return errors.New("AG-UI session service does not support track writes")
	}

	for i, message := range history {
		if message.Role != "user" && message.Role != "assistant" {
			continue
		}
		messageID := "m" + strconv.FormatInt(message.ID, 10)
		events := []aguievents.Event{
			aguievents.NewTextMessageStartEvent(messageID, aguievents.WithRole(message.Role)),
			aguievents.NewTextMessageContentEvent(messageID, message.Content),
			aguievents.NewTextMessageEndEvent(messageID),
		}
		baseTime := time.Unix(message.CreatedAt, 0)
		for j, event := range events {
			payload, err := event.ToJSON()
			if err != nil {
				return fmt.Errorf("encode legacy AG-UI message: %w", err)
			}
			if err := writer.AppendTrackEvent(ctx, sess, &session.TrackEvent{
				Track:     aguiTrack,
				Payload:   json.RawMessage(payload),
				Timestamp: baseTime.Add(time.Duration(i*3+j) * time.Nanosecond),
			}); err != nil {
				return fmt.Errorf("migrate legacy AG-UI message: %w", err)
			}
		}
	}
	return nil
}
