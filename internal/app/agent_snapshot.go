package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
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
func NewAgentService() (*AgentService, error) {
	db, err := sql.Open("sqlite", filepath.Join(dataDir(), "agui.db"))
	if err != nil {
		return nil, fmt.Errorf("open AG-UI session store: %w", err)
	}
	db.SetMaxOpenConns(1)
	sessions, err := sessionsqlite.NewService(db)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize AG-UI session store: %w", err)
	}
	return newAgentService(sessions)
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
	return &AgentService{sessions: sessions, snapshotter: snapshotter}, nil
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
	return result, nil
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
