package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const SettingAgentPermissionMode = "agent.permission.mode"

type PermissionMode string

const (
	PermissionAsk  PermissionMode = "ask"
	PermissionAuto PermissionMode = "auto"
	PermissionFull PermissionMode = "full"
)

type ApprovalDecision string

const (
	ApprovalOnce    ApprovalDecision = "once"
	ApprovalSession ApprovalDecision = "session"
	ApprovalDeny    ApprovalDecision = "deny"
)

type ApprovalRequest struct {
	ID               string    `json:"id"`
	SessionID        string    `json:"sessionId"`
	Tool             string    `json:"tool"`
	Operation        string    `json:"operation"`
	WorkspacePath    string    `json:"workspacePath"`
	Target           string    `json:"target"`
	RiskLevel        string    `json:"riskLevel"`
	OutsideWorkspace bool      `json:"outsideWorkspace"`
	ScopeRoot        string    `json:"scopeRoot"`
	ExpiresAt        time.Time `json:"expiresAt"`
}

type PermissionState struct {
	Mode    PermissionMode    `json:"mode"`
	Pending []ApprovalRequest `json:"pending"`
}

type permissionContextKey string

const (
	permissionServiceKey   permissionContextKey = "blankmind.permission.service"
	permissionSessionKey   permissionContextKey = "blankmind.permission.session"
	permissionWorkspaceKey permissionContextKey = "blankmind.permission.workspace"
)

type pendingApproval struct {
	request ApprovalRequest
	result  chan ApprovalDecision
}

type sessionGrant struct {
	Tool string
	Root string
}

type PermissionService struct {
	settings *SettingsService
	notify   func(string, any)

	mu      sync.Mutex
	pending map[string]*pendingApproval
	grants  map[string][]sessionGrant
}

func NewPermissionService(settings *SettingsService) *PermissionService {
	return &PermissionService{
		settings: settings,
		pending:  make(map[string]*pendingApproval),
		grants:   make(map[string][]sessionGrant),
	}
}

// SetNotify injects the Wails event broadcaster.
//
//wails:ignore
func (s *PermissionService) SetNotify(fn func(string, any)) { s.notify = fn }

func (s *PermissionService) emit(name string, data any) {
	if s.notify != nil {
		s.notify(name, data)
	}
}

func (s *PermissionService) mode() PermissionMode {
	if s == nil || s.settings == nil {
		return PermissionAsk
	}
	v, err := s.settings.GetSetting(SettingAgentPermissionMode)
	if err != nil {
		return PermissionAsk
	}
	switch PermissionMode(strings.ToLower(strings.TrimSpace(v))) {
	case PermissionAuto, PermissionFull:
		return PermissionMode(strings.ToLower(strings.TrimSpace(v)))
	default:
		return PermissionAsk
	}
}

func (s *PermissionService) GetPermissionState() (PermissionState, error) {
	if s == nil {
		return PermissionState{Mode: PermissionAsk, Pending: []ApprovalRequest{}}, nil
	}
	s.mu.Lock()
	pending := make([]ApprovalRequest, 0, len(s.pending))
	for _, item := range s.pending {
		pending = append(pending, item.request)
	}
	s.mu.Unlock()
	return PermissionState{Mode: s.mode(), Pending: pending}, nil
}

func (s *PermissionService) SetPermissionMode(mode PermissionMode) error {
	if s == nil || s.settings == nil {
		return errors.New("权限服务未初始化")
	}
	switch mode {
	case PermissionAsk, PermissionAuto, PermissionFull:
	default:
		return fmt.Errorf("不支持的权限模式: %q", mode)
	}
	if err := s.settings.SetSetting(SettingAgentPermissionMode, string(mode)); err != nil {
		return err
	}
	s.emit("permission.changed", PermissionState{Mode: mode, Pending: []ApprovalRequest{}})
	return nil
}

func WithPermissionContext(ctx context.Context, service *PermissionService, sessionID, workspace string) context.Context {
	ctx = context.WithValue(ctx, permissionServiceKey, service)
	ctx = context.WithValue(ctx, permissionSessionKey, strings.TrimSpace(sessionID))
	ctx = context.WithValue(ctx, permissionWorkspaceKey, strings.TrimSpace(workspace))
	return ctx
}

func permissionContext(ctx context.Context) (*PermissionService, string, string) {
	if ctx == nil {
		return nil, "", ""
	}
	s, _ := ctx.Value(permissionServiceKey).(*PermissionService)
	sessionID, _ := ctx.Value(permissionSessionKey).(string)
	workspace, _ := ctx.Value(permissionWorkspaceKey).(string)
	return s, sessionID, workspace
}

func (s *PermissionService) authorize(ctx context.Context, request ApprovalRequest) (ApprovalDecision, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if s == nil {
		return ApprovalOnce, nil
	}
	request.ID = uuid.NewString()
	request.SessionID = strings.TrimSpace(request.SessionID)
	request.ScopeRoot = filepath.Clean(strings.TrimSpace(request.ScopeRoot))
	request.ExpiresAt = time.Now().Add(5 * time.Minute)

	mode := s.mode()
	if approvesAutomatically(mode, request) {
		s.audit("permission.approved", request, "automatic")
		return ApprovalOnce, nil
	}
	if s.hasGrant(request.SessionID, request.Tool, request.ScopeRoot) {
		s.audit("permission.approved", request, "session")
		return ApprovalSession, nil
	}

	p := &pendingApproval{request: request, result: make(chan ApprovalDecision, 1)}
	s.mu.Lock()
	s.pending[request.ID] = p
	s.mu.Unlock()
	s.emit("permission.requested", request)
	s.audit("permission.requested", request, "pending")

	timer := time.NewTimer(time.Until(request.ExpiresAt))
	defer timer.Stop()
	select {
	case decision := <-p.result:
		s.audit("permission.resolved", request, string(decision))
		return decision, nil
	case <-timer.C:
		s.finishPending(request.ID)
		s.emit("permission.expired", request)
		s.audit("permission.expired", request, "timeout")
		return ApprovalDeny, nil
	case <-ctx.Done():
		s.finishPending(request.ID)
		s.emit("permission.cancelled", request)
		s.audit("permission.cancelled", request, "context")
		return ApprovalDeny, ctx.Err()
	}
}

func (s *PermissionService) ResolveApproval(id string, decision ApprovalDecision) error {
	if decision != ApprovalOnce && decision != ApprovalSession && decision != ApprovalDeny {
		return errors.New("无效的审批决定")
	}
	s.mu.Lock()
	p, ok := s.pending[strings.TrimSpace(id)]
	if ok {
		delete(s.pending, id)
	}
	s.mu.Unlock()
	if !ok {
		return errors.New("审批请求不存在或已处理")
	}
	if decision == ApprovalSession && p.request.SessionID != "" {
		s.mu.Lock()
		s.grants[p.request.SessionID] = append(s.grants[p.request.SessionID], sessionGrant{Tool: p.request.Tool, Root: p.request.ScopeRoot})
		s.mu.Unlock()
	}
	p.result <- decision
	s.emit("permission.resolved", map[string]any{"id": id, "decision": decision})
	return nil
}

func (s *PermissionService) finishPending(id string) {
	s.mu.Lock()
	delete(s.pending, id)
	s.mu.Unlock()
}

func (s *PermissionService) CancelPendingApprovals(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	s.mu.Lock()
	items := make([]*pendingApproval, 0)
	for id, p := range s.pending {
		if sessionID == "" || p.request.SessionID == sessionID {
			delete(s.pending, id)
			items = append(items, p)
		}
	}
	s.mu.Unlock()
	for _, p := range items {
		p.result <- ApprovalDeny
		s.emit("permission.cancelled", p.request)
	}
	return nil
}

func (s *PermissionService) RevokeSessionGrant(sessionID, grantID string) error {
	// grantID is the stable tool|root key exposed to the UI.
	key := strings.TrimSpace(grantID)
	s.mu.Lock()
	defer s.mu.Unlock()
	grants := s.grants[strings.TrimSpace(sessionID)]
	filtered := grants[:0]
	for _, grant := range grants {
		if grant.Tool+"|"+grant.Root != key {
			filtered = append(filtered, grant)
		}
	}
	s.grants[strings.TrimSpace(sessionID)] = filtered
	return nil
}

func (s *PermissionService) hasGrant(sessionID, tool, target string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, grant := range s.grants[strings.TrimSpace(sessionID)] {
		if grant.Tool == tool && (grant.Root == "network" || withinPath(target, grant.Root)) {
			return true
		}
	}
	return false
}

func (s *PermissionService) audit(kind string, request ApprovalRequest, detail string) {
	if store == nil {
		return
	}
	data, _ := json.Marshal(map[string]any{"tool": request.Tool, "operation": request.Operation, "target": request.Target, "detail": detail})
	_, _ = insertEvent(store, kind, string(data), 0)
}

func withinPath(target, root string) bool {
	target = filepath.Clean(target)
	root = filepath.Clean(root)
	if target == "." || root == "." || root == "" {
		return false
	}
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
