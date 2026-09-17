package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

const (
	planStatusPending     = "pending"
	planStatusExecuting   = "executing"
	planStatusCompleted   = "completed"
	planStatusFailed      = "failed"
	planStatusInterrupted = "interrupted"
	planStatusAbandoned   = "abandoned"
)

type Plan struct {
	ID               int64  `json:"id"`
	ConversationID   int64  `json:"conversationId"`
	Status           string `json:"status"`
	CurrentRevision  int    `json:"currentRevision"`
	ApprovedRevision int    `json:"approvedRevision"`
	CreatedAt        int64  `json:"createdAt"`
	UpdatedAt        int64  `json:"updatedAt"`
}

type PlanRevision struct {
	ID                 int64  `json:"id"`
	PlanID             int64  `json:"planId"`
	Revision           int    `json:"revision"`
	Content            string `json:"content"`
	UserMessageID      int64  `json:"userMessageId"`
	AssistantMessageID int64  `json:"assistantMessageId"`
	ProviderID         int64  `json:"providerId"`
	Model              string `json:"model"`
	AgentProfile       string `json:"agentProfile"`
	CreatedAt          int64  `json:"createdAt"`
}

type PlanActionRequest struct {
	PlanID              int64  `json:"planId"`
	Revision            int    `json:"revision"`
	Instruction         string `json:"instruction"`
	Reasoning           string `json:"reasoning"`
	RequestID           string `json:"requestId"`
	WorkspacePath       string `json:"workspacePath"`
	PermissionSessionID string `json:"permissionSessionId"`
	AgentProfile        string `json:"agentProfile"`
}

type SetConversationModelRequest struct {
	ConversationID int64  `json:"conversationId"`
	Profile        string `json:"profile"`
	ProviderID     int64  `json:"providerId"`
	Model          string `json:"model"`
}

type planExecutionContext struct {
	Plan     Plan
	Revision PlanRevision
}

func normalizeChatMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "chat":
		return "chat"
	case "plan":
		return "plan"
	case "execute":
		return "execute"
	default:
		return ""
	}
}

func providerForConversation(conversationID int64) (Provider, error) {
	if settingsSvc == nil {
		return Provider{}, errors.New("设置服务未初始化")
	}
	if conversationID <= 0 {
		return providerForConversationProfile(0, AgentProfileWork)
	}
	var profile string
	if err := store.QueryRow("SELECT agent_profile FROM conversations WHERE id = ?", conversationID).Scan(&profile); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Provider{}, errors.New("会话不存在")
		}
		return Provider{}, err
	}
	return providerForConversationProfile(conversationID, profile)
}

func configuredProvider(providerID int64, modelName string) (Provider, error) {
	modelName = strings.TrimSpace(modelName)
	var provider Provider
	var providerMultimodal, modelMultimodal, isDefault int
	err := store.QueryRow(`
		SELECT p.id, p.name, p.kind, p.base_url, p.api_key, ?,
			p.multimodal, pm.multimodal, p.is_default, p.created_at
		FROM providers p
		JOIN provider_models pm ON pm.provider_id = p.id AND pm.model = ?
		WHERE p.id = ?`, modelName, modelName, providerID).Scan(
		&provider.ID, &provider.Name, &provider.Kind, &provider.BaseURL, &provider.APIKey,
		&provider.Model, &providerMultimodal, &modelMultimodal, &isDefault, &provider.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Provider{}, errors.New("当前对话模型已被删除或停用，请重新选择模型")
	}
	if err != nil {
		return Provider{}, err
	}
	provider.Multimodal = modelMultimodal != 0 || providerMultimodal != 0
	provider.IsDefault = isDefault != 0
	if resolveSecret(&provider) {
		_, _ = store.Exec("UPDATE providers SET api_key = '' WHERE id = ?", provider.ID)
	}
	return provider, nil
}

// SetConversationModel changes only the selected conversation. New conversations still inherit the global default.
func (s *AgentService) SetConversationModel(req SetConversationModelRequest) error {
	if req.ConversationID <= 0 {
		return errors.New("会话 ID 无效")
	}
	if !s.beginConversationRun(req.ConversationID) {
		return errConversationBusy
	}
	defer s.finishConversationRun(req.ConversationID)
	provider, err := configuredProvider(req.ProviderID, req.Model)
	if err != nil {
		return err
	}
	profile := normalizeAgentProfile(req.Profile)
	if profile == "" {
		return errors.New("不支持的 Agent 模式")
	}
	tx, err := store.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err = tx.Exec(`INSERT INTO conversation_profile_models (conversation_id, profile, provider_id, model) VALUES (?, ?, ?, ?)
		ON CONFLICT(conversation_id, profile) DO UPDATE SET provider_id = excluded.provider_id, model = excluded.model`, req.ConversationID, profile, provider.ID, provider.Model); err != nil {
		return err
	}
	result, err := tx.Exec(`UPDATE conversations SET provider_id = CASE WHEN agent_profile = ? THEN ? ELSE provider_id END,
		model = CASE WHEN agent_profile = ? THEN ? ELSE model END, updated_at = ? WHERE id = ?`,
		profile, provider.ID, profile, provider.Model, now(), req.ConversationID)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errors.New("会话不存在")
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.emit("conversations.changed", "updated")
	return nil
}

// RevisePlan creates a complete new revision using the conversation's currently selected model.
func (s *AgentService) RevisePlan(ctx context.Context, req PlanActionRequest) (ChatResult, error) {
	if strings.TrimSpace(req.Instruction) == "" {
		return ChatResult{}, errors.New("请输入计划修改意见")
	}
	execution, err := loadPlanExecution(0, req.PlanID, req.Revision)
	if err != nil {
		return ChatResult{}, err
	}
	return s.Chat(ctx, ChatRequest{
		ConversationID: execution.Plan.ConversationID, Message: req.Instruction,
		Reasoning: req.Reasoning, RequestID: req.RequestID, WorkspacePath: req.WorkspacePath,
		PermissionSessionID: req.PermissionSessionID, Mode: "plan",
		AgentProfile: req.AgentProfile,
		PlanID:       req.PlanID, PlanRevision: req.Revision,
	})
}

// ExecutePlan immediately executes the exact approved revision.
func (s *AgentService) ExecutePlan(ctx context.Context, req PlanActionRequest) (ChatResult, error) {
	execution, err := loadPlanExecution(0, req.PlanID, req.Revision)
	if err != nil {
		return ChatResult{}, err
	}
	return s.Chat(ctx, ChatRequest{
		ConversationID: execution.Plan.ConversationID,
		Message:        fmt.Sprintf("执行已批准计划 v%d", req.Revision),
		Reasoning:      req.Reasoning, RequestID: req.RequestID, WorkspacePath: req.WorkspacePath,
		PermissionSessionID: req.PermissionSessionID, Mode: "execute", AgentProfile: execution.Revision.AgentProfile,
		PlanID: req.PlanID, PlanRevision: req.Revision,
	})
}

// AbandonPlan closes an active plan without changing conversation history.
func (s *AgentService) AbandonPlan(req PlanActionRequest) error {
	execution, err := loadPlanExecution(0, req.PlanID, req.Revision)
	if err != nil {
		return err
	}
	if !s.beginConversationRun(execution.Plan.ConversationID) {
		return errConversationBusy
	}
	defer s.finishConversationRun(execution.Plan.ConversationID)
	result, err := store.Exec(`
		UPDATE plans SET status = ?, updated_at = ?
		WHERE id = ? AND current_revision = ? AND status IN (?, ?, ?)`,
		planStatusAbandoned, now(), req.PlanID, req.Revision,
		planStatusPending, planStatusFailed, planStatusInterrupted)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errors.New("计划状态已变化，请刷新后重试")
	}
	s.emit("conversations.changed", "updated")
	return nil
}

func loadPlanExecution(conversationID, planID int64, revision int) (*planExecutionContext, error) {
	if planID <= 0 || revision <= 0 {
		return nil, errors.New("计划版本无效")
	}
	var result planExecutionContext
	err := store.QueryRow(`
		SELECT p.id, p.conversation_id, p.status, p.current_revision, p.approved_revision,
			p.created_at, p.updated_at, r.id, r.plan_id, r.revision, r.content,
			r.user_message_id, r.assistant_message_id, r.provider_id, r.model, r.agent_profile, r.created_at
		FROM plans p JOIN plan_revisions r ON r.plan_id = p.id AND r.revision = ?
		WHERE p.id = ?`, revision, planID).Scan(
		&result.Plan.ID, &result.Plan.ConversationID, &result.Plan.Status,
		&result.Plan.CurrentRevision, &result.Plan.ApprovedRevision,
		&result.Plan.CreatedAt, &result.Plan.UpdatedAt,
		&result.Revision.ID, &result.Revision.PlanID, &result.Revision.Revision,
		&result.Revision.Content, &result.Revision.UserMessageID,
		&result.Revision.AssistantMessageID, &result.Revision.ProviderID,
		&result.Revision.Model, &result.Revision.AgentProfile, &result.Revision.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("计划不存在")
	}
	if err != nil {
		return nil, err
	}
	if conversationID > 0 && result.Plan.ConversationID != conversationID {
		return nil, errors.New("计划不属于当前会话")
	}
	if result.Plan.CurrentRevision != revision {
		return nil, errors.New("计划已有新版本，请刷新后操作")
	}
	switch result.Plan.Status {
	case planStatusPending, planStatusFailed, planStatusInterrupted:
		return &result, nil
	default:
		return nil, fmt.Errorf("当前计划状态为 %s，不能执行此操作", result.Plan.Status)
	}
}

func savePlanRevision(conversationID, requestedPlanID int64, expectedRevision int, userMessageID, assistantMessageID int64, content string, provider Provider, profiles ...string) (PlanRevision, error) {
	profile := AgentProfileWork
	if len(profiles) > 0 && normalizeAgentProfile(profiles[0]) != "" {
		profile = normalizeAgentProfile(profiles[0])
	}
	tx, err := store.Begin()
	if err != nil {
		return PlanRevision{}, err
	}
	defer tx.Rollback() //nolint:errcheck
	var plan Plan
	if requestedPlanID > 0 {
		err = tx.QueryRow(`SELECT id, conversation_id, status, current_revision, approved_revision, created_at, updated_at FROM plans WHERE id = ?`, requestedPlanID).
			Scan(&plan.ID, &plan.ConversationID, &plan.Status, &plan.CurrentRevision, &plan.ApprovedRevision, &plan.CreatedAt, &plan.UpdatedAt)
	} else {
		err = tx.QueryRow(`
			SELECT id, conversation_id, status, current_revision, approved_revision, created_at, updated_at
			FROM plans WHERE conversation_id = ? AND status IN (?, ?, ?, ?) ORDER BY id DESC LIMIT 1`,
			conversationID, planStatusPending, planStatusExecuting, planStatusFailed, planStatusInterrupted).
			Scan(&plan.ID, &plan.ConversationID, &plan.Status, &plan.CurrentRevision, &plan.ApprovedRevision, &plan.CreatedAt, &plan.UpdatedAt)
	}
	if errors.Is(err, sql.ErrNoRows) {
		result, createErr := tx.Exec(`INSERT INTO plans (conversation_id, status, current_revision, approved_revision, created_at, updated_at) VALUES (?, ?, 0, 0, ?, ?)`, conversationID, planStatusPending, now(), now())
		if createErr != nil {
			return PlanRevision{}, createErr
		}
		plan.ID, _ = result.LastInsertId()
		plan.ConversationID = conversationID
		plan.Status = planStatusPending
		plan.CurrentRevision = 0
	} else if err != nil {
		return PlanRevision{}, err
	}
	if plan.ConversationID != conversationID {
		return PlanRevision{}, errors.New("计划不属于当前会话")
	}
	if requestedPlanID > 0 && plan.CurrentRevision != expectedRevision {
		return PlanRevision{}, errors.New("计划已有新版本，请刷新后重试")
	}
	if plan.Status == planStatusCompleted || plan.Status == planStatusAbandoned {
		return PlanRevision{}, errors.New("该计划已结束，请创建新计划")
	}
	revision := plan.CurrentRevision + 1
	result, err := tx.Exec(`
		INSERT INTO plan_revisions (plan_id, revision, content, user_message_id, assistant_message_id, provider_id, model, agent_profile, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		plan.ID, revision, content, userMessageID, assistantMessageID, provider.ID, provider.Model, profile, now())
	if err != nil {
		return PlanRevision{}, err
	}
	revisionID, _ := result.LastInsertId()
	if _, err := tx.Exec(`UPDATE plans SET status = ?, current_revision = ?, approved_revision = 0, updated_at = ? WHERE id = ?`, planStatusPending, revision, now(), plan.ID); err != nil {
		return PlanRevision{}, err
	}
	if _, err := tx.Exec(`
		UPDATE messages SET plan_id = ?, plan_revision = ?
		WHERE id = ? OR (id = ? AND message_type = 'plan_request')`,
		plan.ID, revision, assistantMessageID, userMessageID); err != nil {
		return PlanRevision{}, err
	}
	if err := tx.Commit(); err != nil {
		return PlanRevision{}, err
	}
	return PlanRevision{ID: revisionID, PlanID: plan.ID, Revision: revision, Content: content, UserMessageID: userMessageID, AssistantMessageID: assistantMessageID, ProviderID: provider.ID, Model: provider.Model, AgentProfile: profile, CreatedAt: now()}, nil
}

func beginPlanRun(planID int64, revision int, requestID string, provider Provider, profiles ...string) (int64, error) {
	profile := AgentProfileWork
	if len(profiles) > 0 && normalizeAgentProfile(profiles[0]) != "" {
		profile = normalizeAgentProfile(profiles[0])
	}
	tx, err := store.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck
	result, err := tx.Exec(`
		UPDATE plans SET status = ?, approved_revision = ?, updated_at = ?
		WHERE id = ? AND current_revision = ? AND status IN (?, ?, ?)`,
		planStatusExecuting, revision, now(), planID, revision,
		planStatusPending, planStatusFailed, planStatusInterrupted)
	if err != nil {
		return 0, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return 0, errors.New("计划状态已变化，请刷新后重试")
	}
	result, err = tx.Exec(`
		INSERT INTO plan_runs (plan_id, revision, request_id, provider_id, model, agent_profile, status, started_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, planID, revision, requestID, provider.ID, provider.Model, profile, planStatusExecuting, now())
	if err != nil {
		return 0, err
	}
	runID, _ := result.LastInsertId()
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return runID, nil
}

func finishPlanRun(runID, planID int64, status, runError string) error {
	planStatus := status
	if status == "paused" {
		planStatus = planStatusPending
	}
	tx, err := store.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec(`UPDATE plan_runs SET status = ?, error = ?, completed_at = ? WHERE id = ?`, status, runError, now(), runID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE plans SET status = ?, updated_at = ? WHERE id = ?`, planStatus, now(), planID); err != nil {
		return err
	}
	return tx.Commit()
}

type planRevisionSignal struct {
	mu        sync.Mutex
	Requested bool
	Reason    string
}

func (s *planRevisionSignal) snapshot() (bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Requested, s.Reason
}

type planRevisionToolInput struct {
	Reason string `json:"reason"`
}

func newPlanRevisionTool(signal *planRevisionSignal) tool.Tool {
	return function.NewFunctionTool(func(_ context.Context, input planRevisionToolInput) (permissionToolResult, error) {
		reason := strings.TrimSpace(input.Reason)
		if reason == "" {
			reason = "执行中发现需要实质调整已批准计划"
		}
		signal.mu.Lock()
		signal.Requested = true
		signal.Reason = reason
		signal.mu.Unlock()
		return permissionToolResult{OK: true, Message: "执行已暂停。请在最终回复中输出完整的修订计划，不要继续调用其它工具。"}, nil
	}, function.WithName("request_plan_revision"), function.WithDescription("当执行必须实质偏离批准计划时调用。调用后停止实施，并在最终回复中给出完整替代计划。"))
}
