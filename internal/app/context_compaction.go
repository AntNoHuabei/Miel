package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/session"
)

const contextCompactionThreshold = 0.8

func estimateTextTokens(text string) int {
	count := len([]rune(text))
	if count == 0 {
		return 0
	}
	// A conservative local estimate for mixed Chinese, prose, and source code.
	return (count + 1) / 2
}

func estimateConversationTokens(messages []ChatMessage) int {
	total := 0
	for _, message := range messages {
		total += 4 + estimateTextTokens(message.Content)
		for range message.Attachments {
			total += 256
		}
	}
	return total
}

func (s *AgentService) contextWindowForProvider(provider Provider) int {
	if settingsSvc != nil {
		if cached, ok := settingsSvc.cachedProviderCapability(provider, provider.Model); ok && cached.ContextWindow > 0 {
			return cached.ContextWindow
		}
		if strings.EqualFold(strings.TrimSpace(provider.Kind), "herdsman") || strings.EqualFold(strings.TrimSpace(provider.Kind), "openrouter") {
			models, err := settingsSvc.DiscoverProviderModels(ProviderInput{
				ID: provider.ID, Name: provider.Name, Kind: provider.Kind, BaseURL: provider.BaseURL,
				APIKey: provider.APIKey, Model: provider.Model,
			})
			if err == nil {
				for _, item := range models {
					if strings.EqualFold(item.ID, provider.Model) && item.ContextWindow > 0 {
						return item.ContextWindow
					}
				}
			}
		}
	}
	if window, ok := modelContextWindow(provider.Kind, provider.Model); ok {
		return window
	}
	return 0
}

func loadConversationContextUsage(conversationID int64) (*ConversationContextUsage, error) {
	if conversationID <= 0 {
		return nil, nil
	}
	var usage ConversationContextUsage
	var estimated int
	err := store.QueryRow(`
		SELECT used_tokens, context_window, model, estimated, updated_at
		FROM conversation_context_usage WHERE conversation_id = ?`, conversationID).
		Scan(&usage.UsedTokens, &usage.ContextWindow, &usage.Model, &estimated, &usage.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load context usage: %w", err)
	}
	usage.Estimated = estimated != 0
	return &usage, nil
}

func saveConversationContextUsage(conversationID int64, usage ConversationContextUsage) error {
	if conversationID <= 0 {
		return nil
	}
	_, err := store.Exec(`
		INSERT INTO conversation_context_usage
			(conversation_id, used_tokens, context_window, model, estimated, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(conversation_id) DO UPDATE SET
			used_tokens = excluded.used_tokens,
			context_window = excluded.context_window,
			model = excluded.model,
			estimated = excluded.estimated,
			updated_at = excluded.updated_at`,
		conversationID, usage.UsedTokens, usage.ContextWindow, usage.Model,
		boolToInt(usage.Estimated), usage.UpdatedAt)
	if err != nil {
		return fmt.Errorf("save context usage: %w", err)
	}
	return nil
}

func (s *AgentService) shouldAutoCompact(conversationID int64, provider Provider, pending []ChatMessage) (bool, error) {
	window := s.contextWindowForProvider(provider)
	if window <= 0 {
		return false, nil
	}
	usage, err := loadConversationContextUsage(conversationID)
	if err != nil {
		return false, err
	}
	if usage != nil && usage.ContextWindow == window && usage.UsedTokens > 0 {
		return float64(usage.UsedTokens) >= float64(window)*contextCompactionThreshold, nil
	}
	return float64(estimateConversationTokens(pending)) >= float64(window)*contextCompactionThreshold, nil
}

func (s *AgentService) compactConversationLocked(ctx context.Context, conversationID int64, provider Provider, model model.Model, requestID, trigger string) error {
	if conversationID <= 0 {
		return errors.New("压缩上下文需要已有会话")
	}
	if s.sessions == nil {
		return errors.New("AG-UI 会话存储未初始化")
	}
	s.emit("agent.compaction", map[string]any{
		"conversationId": conversationID, "requestId": requestID, "phase": "start", "trigger": trigger,
	})
	key := aguiSessionKey(conversationID)
	sess, err := s.sessions.GetSession(ctx, key)
	if err != nil {
		s.emit("agent.compaction", map[string]any{
			"conversationId": conversationID, "requestId": requestID, "phase": "error", "trigger": trigger, "message": err.Error(),
		})
		return fmt.Errorf("读取会话摘要失败: %w", err)
	}
	if sess == nil {
		s.emit("agent.compaction", map[string]any{
			"conversationId": conversationID, "requestId": requestID, "phase": "error", "trigger": trigger, "message": "会话不存在",
		})
		return errors.New("会话不存在")
	}
	s.emit("agent.compaction", map[string]any{
		"conversationId": conversationID, "requestId": requestID, "phase": "running", "trigger": trigger,
	})
	if err := s.sessions.CreateSessionSummary(withSummaryModel(ctx, model), sess, session.SummaryFilterKeyAllContents, true); err != nil {
		s.emit("agent.compaction", map[string]any{
			"conversationId": conversationID, "requestId": requestID, "phase": "error", "message": err.Error(),
		})
		return fmt.Errorf("压缩会话上下文失败: %w", err)
	}
	history, err := s.loadMessages(conversationID)
	if err != nil {
		return err
	}
	window := s.contextWindowForProvider(provider)
	summaryText, _ := s.sessions.GetSessionSummaryText(ctx, sess)
	used := estimateTextTokens(summaryText)
	// The runner keeps the newest events after the summary boundary visible.
	// Include a small recent tail so the meter tracks the next effective prompt.
	start := len(history) - 4
	if start < 0 {
		start = 0
	}
	used += estimateConversationTokens(history[start:])
	if used == 0 {
		used = estimateConversationTokens(history)
	}
	if window > 0 && used >= window {
		used = window - 1
	}
	usage := ConversationContextUsage{UsedTokens: used, ContextWindow: window, Model: provider.Model, Estimated: true, UpdatedAt: time.Now().Unix()}
	if err := saveConversationContextUsage(conversationID, usage); err != nil {
		return err
	}
	s.emit("agent.compaction", map[string]any{
		"conversationId": conversationID, "requestId": requestID, "phase": "done", "trigger": trigger, "usage": usage,
	})
	return nil
}
