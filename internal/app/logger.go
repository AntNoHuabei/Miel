package app

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
)

type logContextKey string

const (
	logRequestIDKey      logContextKey = "request_id"
	logConversationIDKey logContextKey = "conversation_id"
)

func withLogContext(ctx context.Context, conversationID int64, requestID string) context.Context {
	ctx = context.WithValue(ctx, logConversationIDKey, conversationID)
	return context.WithValue(ctx, logRequestIDKey, strings.TrimSpace(requestID))
}

func logInfo(ctx context.Context, event string, args ...any) {
	slog.InfoContext(ctx, event, append(logContextAttrs(ctx), args...)...)
}

func logError(ctx context.Context, event string, err error, args ...any) {
	if err != nil {
		args = append(args, "error", sanitizeLogText(err.Error()))
	}
	slog.ErrorContext(ctx, event, append(logContextAttrs(ctx), args...)...)
}

func logContextAttrs(ctx context.Context) []any {
	if ctx == nil {
		return nil
	}
	attrs := make([]any, 0, 4)
	if requestID, _ := ctx.Value(logRequestIDKey).(string); requestID != "" {
		attrs = append(attrs, "request_id", requestID)
	}
	if conversationID, _ := ctx.Value(logConversationIDKey).(int64); conversationID > 0 {
		attrs = append(attrs, "conversation_id", conversationID)
	}
	return attrs
}

func sanitizeLogText(value string) string {
	value = strings.ReplaceAll(strings.ReplaceAll(value, "\r", " "), "\n", " ")
	if len(value) > 2048 {
		value = value[:2048] + "..."
	}
	return value
}

func logExecutable(path string) string { return filepath.Base(filepath.Clean(path)) }
