package app

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
)

var chatHTTPStatusPattern = regexp.MustCompile(`\b(400|401|403|404|408|413|422|429|5[0-9]{2})\b`)

func normalizeChatRunError(code, message string) ChatRunError {
	code = strings.TrimSpace(code)
	message = strings.TrimSpace(message)
	if code == "" {
		code = providerErrorCodeFromMessage(message)
	}
	if code == "" {
		if match := chatHTTPStatusPattern.FindStringSubmatch(message); len(match) == 2 {
			code = match[1]
		}
	}
	if code == "" {
		lower := strings.ToLower(message)
		switch {
		case strings.Contains(lower, "max tool iterations"):
			code = "tool_iteration_limit"
		case strings.Contains(lower, "conversation_busy"):
			code = "conversation_busy"
		case strings.Contains(lower, "deadline exceeded") || strings.Contains(lower, "timed out") || strings.Contains(lower, "timeout"):
			code = "timeout"
		case strings.Contains(lower, "context canceled") || strings.Contains(lower, "context cancelled") || strings.Contains(lower, "request canceled"):
			code = "cancelled"
		case strings.Contains(lower, "connection refused") || strings.Contains(lower, "connection reset") || strings.Contains(lower, "no such host") || strings.Contains(lower, "dial tcp") || strings.Contains(lower, "tls handshake") || strings.Contains(lower, "unexpected eof"):
			code = "network_error"
		default:
			code = "unknown_error"
		}
	}
	if message == "" {
		message = "模型未返回错误详情"
	}
	return ChatRunError{Code: code, Message: message}
}

func providerErrorCodeFromMessage(message string) string {
	type errorPayload struct {
		Error struct {
			Code json.RawMessage `json:"code"`
		} `json:"error"`
	}
	for offset := strings.IndexByte(message, '{'); offset >= 0; {
		var payload errorPayload
		if json.Unmarshal([]byte(message[offset:]), &payload) == nil && len(payload.Error.Code) > 0 {
			var text string
			if json.Unmarshal(payload.Error.Code, &text) == nil {
				return strings.TrimSpace(text)
			}
			var number json.Number
			if json.Unmarshal(payload.Error.Code, &number) == nil {
				return number.String()
			}
			return ""
		}
		next := strings.IndexByte(message[offset+1:], '{')
		if next < 0 {
			break
		}
		offset += next + 1
	}
	return ""
}

func (s *AgentService) emitChatRunError(conversationID int64, requestID string, runErr ChatRunError) {
	event := aguievents.NewRunErrorEvent(runErr.Message, aguievents.WithErrorCode(runErr.Code))
	payload, err := event.ToJSON()
	if err != nil {
		return
	}
	var decoded map[string]any
	if json.Unmarshal(payload, &decoded) != nil {
		return
	}
	s.emit("agent.agui", map[string]any{
		"conversationId": conversationID,
		"requestId":      requestID,
		"event":          decoded,
	})
}

func (s *AgentService) saveChatRunError(conversationID, userMessageID int64, requestID string, runErr ChatRunError) (int64, error) {
	if conversationID <= 0 || userMessageID <= 0 {
		return 0, nil
	}
	tx, err := store.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck
	res, err := tx.Exec(`
		INSERT INTO chat_run_errors (
			conversation_id, user_message_id, request_id, code, message, created_at
		) VALUES (?, ?, ?, ?, ?, ?)`,
		conversationID, userMessageID, requestID, runErr.Code, runErr.Message, now())
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec("UPDATE conversations SET updated_at = ? WHERE id = ?", now(), conversationID); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func loadChatRunErrors(conversationID int64) ([]ChatRunError, error) {
	rows, err := store.Query(`
		SELECT id, conversation_id, user_message_id, request_id, code, message, created_at
		FROM chat_run_errors WHERE conversation_id = ? ORDER BY id`, conversationID)
	if err != nil {
		return nil, fmt.Errorf("load chat run errors: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	var result []ChatRunError
	for rows.Next() {
		var item ChatRunError
		if err := rows.Scan(&item.ID, &item.ConversationID, &item.UserMessageID, &item.RequestID, &item.Code, &item.Message, &item.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
