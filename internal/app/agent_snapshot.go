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
func NewAgentService(memoryRuntime *memoryRuntime, attachments *ChatAttachmentService, permissions ...*PermissionService) (*AgentService, error) {
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
	if len(permissions) > 0 {
		service.permissions = permissions[0]
	}
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

// MessagesSnapshot returns the application-level ordered conversation timeline.
// The underlying AG-UI snapshot remains internal so plan entities never masquerade as chat messages.
func (s *AgentService) MessagesSnapshot(conversationID int64) (map[string]any, error) {
	raw, err := s.rawMessagesSnapshot(conversationID)
	if err != nil {
		return nil, err
	}
	return buildConversationSnapshot(raw, conversationID)
}

func (s *AgentService) rawMessagesSnapshot(conversationID int64) (map[string]any, error) {
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
	runErrors, err := loadChatRunErrors(conversationID)
	if err != nil {
		return nil, err
	}
	mergePersistedRunErrors(result, runErrors)
	metrics, err := loadConversationMessageMetrics(conversationID)
	if err != nil {
		return nil, err
	}
	attachMessageMetrics(result, metrics)
	if err := attachMessageTypes(result, conversationID); err != nil {
		return nil, err
	}
	if err := s.attachSnapshotAttachments(result, history); err != nil {
		return nil, err
	}
	if s.artifacts != nil {
		if err := s.artifacts.attachSnapshot(result, conversationID); err != nil {
			return nil, err
		}
	}
	redactAGUIBinaryContent(result)
	return result, nil
}

type snapshotMessageMeta struct {
	MessageID     int64
	AGUIMessageID string
	MessageType   string
	PlanID        int64
	PlanRevision  int
	CreatedAt     int64
}

func loadSnapshotMessageMeta(conversationID int64) (map[string]snapshotMessageMeta, error) {
	rows, err := store.Query(`
		SELECT m.id, COALESCE(mm.agui_message_id, ''), m.message_type,
			m.plan_id, m.plan_revision, m.created_at
		FROM messages m LEFT JOIN message_metrics mm ON mm.message_id = m.id
		WHERE m.conversation_id = ?`, conversationID)
	if err != nil {
		return nil, fmt.Errorf("load message metadata: %w", err)
	}
	messageMeta := make(map[string]snapshotMessageMeta)
	for rows.Next() {
		var item snapshotMessageMeta
		if err := rows.Scan(&item.MessageID, &item.AGUIMessageID, &item.MessageType, &item.PlanID, &item.PlanRevision, &item.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		messageMeta["m"+strconv.FormatInt(item.MessageID, 10)] = item
		if item.AGUIMessageID != "" {
			messageMeta[item.AGUIMessageID] = item
		}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return messageMeta, nil
}

func attachMessageTypes(snapshot map[string]any, conversationID int64) error {
	messageMeta, err := loadSnapshotMessageMeta(conversationID)
	if err != nil {
		return err
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
		if item, exists := messageMeta[messageID]; exists && item.MessageType != "chat" {
			message["messageType"] = item.MessageType
		}
	}
	return nil
}

type snapshotPlanView struct {
	ID              int64  `json:"id"`
	MessageID       string `json:"messageId"`
	Revision        int    `json:"revision"`
	CurrentRevision int    `json:"currentRevision"`
	Status          string `json:"status"`
	Content         string `json:"content"`
	GeneratedModel  string `json:"generatedModel"`
	ExecutionModel  string `json:"executionModel,omitempty"`
	CreatedAt       int64  `json:"createdAt"`
}

type snapshotPlanRun struct {
	ID          int64  `json:"id"`
	PlanID      int64  `json:"planId"`
	Revision    int    `json:"revision"`
	Status      string `json:"status"`
	Model       string `json:"model"`
	Error       string `json:"error,omitempty"`
	StartedAt   int64  `json:"startedAt"`
	CompletedAt int64  `json:"completedAt,omitempty"`
}

func planRevisionKey(planID int64, revision int) string {
	return strconv.FormatInt(planID, 10) + ":" + strconv.Itoa(revision)
}

func loadSnapshotPlans(conversationID int64) (map[string]snapshotPlanView, map[string][]snapshotPlanRun, error) {
	rows, err := store.Query(`
		SELECT r.assistant_message_id, COALESCE(mm.agui_message_id, ''), r.plan_id,
			r.revision, p.current_revision, p.status, r.content, r.model, r.created_at,
			COALESCE((SELECT pr.model FROM plan_runs pr
				WHERE pr.plan_id = r.plan_id AND pr.revision = r.revision
				ORDER BY pr.id DESC LIMIT 1), '')
		FROM plan_revisions r
		JOIN plans p ON p.id = r.plan_id
		LEFT JOIN message_metrics mm ON mm.message_id = r.assistant_message_id
		WHERE p.conversation_id = ? ORDER BY r.plan_id, r.revision`, conversationID)
	if err != nil {
		return nil, nil, fmt.Errorf("load plan snapshot: %w", err)
	}
	plans := make(map[string]snapshotPlanView)
	for rows.Next() {
		var assistantMessageID int64
		var aguiMessageID string
		var item snapshotPlanView
		if err := rows.Scan(&assistantMessageID, &aguiMessageID, &item.ID, &item.Revision,
			&item.CurrentRevision, &item.Status, &item.Content, &item.GeneratedModel,
			&item.CreatedAt, &item.ExecutionModel); err != nil {
			rows.Close()
			return nil, nil, err
		}
		if item.Revision < item.CurrentRevision {
			item.Status = "revised"
		}
		item.MessageID = "m" + strconv.FormatInt(assistantMessageID, 10)
		plans[item.MessageID] = item
		if aguiMessageID != "" {
			plans[aguiMessageID] = item
		}
	}
	if err := rows.Close(); err != nil {
		return nil, nil, err
	}

	runRows, err := store.Query(`
		SELECT pr.id, pr.plan_id, pr.revision, pr.status, pr.model, pr.error,
			pr.started_at, pr.completed_at
		FROM plan_runs pr JOIN plans p ON p.id = pr.plan_id
		WHERE p.conversation_id = ? ORDER BY pr.id`, conversationID)
	if err != nil {
		return nil, nil, fmt.Errorf("load plan runs: %w", err)
	}
	runs := make(map[string][]snapshotPlanRun)
	for runRows.Next() {
		var item snapshotPlanRun
		if err := runRows.Scan(&item.ID, &item.PlanID, &item.Revision, &item.Status,
			&item.Model, &item.Error, &item.StartedAt, &item.CompletedAt); err != nil {
			runRows.Close()
			return nil, nil, err
		}
		key := planRevisionKey(item.PlanID, item.Revision)
		runs[key] = append(runs[key], item)
	}
	if err := runRows.Close(); err != nil {
		return nil, nil, err
	}
	return plans, runs, nil
}

func buildConversationSnapshot(snapshot map[string]any, conversationID int64) (map[string]any, error) {
	messageMeta, err := loadSnapshotMessageMeta(conversationID)
	if err != nil {
		return nil, err
	}
	plans, runs, err := loadSnapshotPlans(conversationID)
	if err != nil {
		return nil, err
	}
	messages, _ := snapshot["messages"].([]any)
	timeline := make([]any, 0, len(messages))
	runIndexes := make(map[string]int)
	for index, raw := range messages {
		message, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		messageID, _ := message["id"].(string)
		meta, hasMeta := messageMeta[messageID]
		delete(message, "messageType")
		item := map[string]any{"kind": "message", "sequence": index + 1, "message": message}
		if hasMeta {
			switch meta.MessageType {
			case "plan_request":
				item = map[string]any{"kind": "plan_request", "sequence": index + 1, "request": map[string]any{
					"id": messageID, "messageId": messageID, "content": message["content"],
					"planId": meta.PlanID, "revision": meta.PlanRevision, "createdAt": meta.CreatedAt,
				}}
			case "plan_response":
				if plan, exists := plans[messageID]; exists {
					item = map[string]any{"kind": "plan", "sequence": index + 1, "plan": plan}
				}
			case "plan_execution":
				key := planRevisionKey(meta.PlanID, meta.PlanRevision)
				availableRuns := runs[key]
				var run snapshotPlanRun
				if len(availableRuns) > 0 {
					runIndex := runIndexes[key]
					if runIndex >= len(availableRuns) {
						runIndex = len(availableRuns) - 1
					}
					run = availableRuns[runIndex]
					runIndexes[key]++
				}
				execution := map[string]any{
					"id": messageID, "messageId": messageID, "content": message["content"],
					"planId": meta.PlanID, "revision": meta.PlanRevision, "createdAt": meta.CreatedAt,
				}
				if run.ID > 0 {
					execution["run"] = run
				}
				item = map[string]any{"kind": "plan_execution", "sequence": index + 1, "execution": execution}
			}
		}
		timeline = append(timeline, item)
	}
	return map[string]any{"type": "CONVERSATION_SNAPSHOT", "timeline": timeline}, nil
}

func mergePersistedRunErrors(snapshot map[string]any, runErrors []ChatRunError) {
	if len(runErrors) == 0 {
		return
	}
	messages, ok := snapshot["messages"].([]any)
	if !ok {
		messages = []any{}
	}
	byUserMessage := make(map[string][]ChatRunError)
	for _, runErr := range runErrors {
		key := "m" + strconv.FormatInt(runErr.UserMessageID, 10)
		byUserMessage[key] = append(byUserMessage[key], runErr)
	}
	merged := make([]any, 0, len(messages)+len(runErrors))
	var pending []ChatRunError
	flushPending := func() {
		for _, runErr := range pending {
			merged = append(merged, runErrorSnapshotMessage(runErr))
		}
		pending = nil
	}
	for _, raw := range messages {
		message, ok := raw.(map[string]any)
		role, _ := message["role"].(string)
		if ok && role == "user" {
			flushPending()
		}
		merged = append(merged, raw)
		if !ok || role != "user" {
			continue
		}
		messageID, _ := message["id"].(string)
		pending = append(pending, byUserMessage[messageID]...)
		delete(byUserMessage, messageID)
	}
	flushPending()
	for _, runErr := range runErrors {
		key := "m" + strconv.FormatInt(runErr.UserMessageID, 10)
		if _, pending := byUserMessage[key]; pending {
			merged = append(merged, runErrorSnapshotMessage(runErr))
		}
	}
	snapshot["messages"] = merged
}

func runErrorSnapshotMessage(runErr ChatRunError) map[string]any {
	return map[string]any{
		"id":   "error-" + strconv.FormatInt(runErr.ID, 10),
		"role": "error",
		"runError": map[string]any{
			"code":    runErr.Code,
			"message": runErr.Message,
		},
	}
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
