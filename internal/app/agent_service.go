package app

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	agentcore "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	agentevent "trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/server/agui/adapter"
	aguirunner "trpc.group/trpc-go/trpc-agent-go/server/agui/runner"
	aguitranslator "trpc.group/trpc-go/trpc-agent-go/server/agui/translator"
	"trpc.group/trpc-go/trpc-agent-go/session"
	"trpc.group/trpc-go/trpc-agent-go/skill"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	aguitypes "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
)

// settingsSvc / todoSvc 由 main 装配时赋值,便于 Agent 服务复用。
var (
	settingsSvc *SettingsService
	todoSvc     *TodoService
)

const maxAgentToolIterations = 40

var errConversationBusy = errors.New("conversation_busy: 当前会话仍在处理中，请等待本轮完成后再发送")

// AgentService 是基于 trpc-agent-go 的对话 Agent 服务:
//   - 每次 Chat 按会话所选 Provider/Model 动态构建 LLMAgent,会话可并行
//   - 办公能力(待办/统计/日志)以 function calling 暴露
//   - <数据目录>/skills 下的 SKILL.md 由框架加载,新增即插即用
//
// 流式增量经事件 agent.chunk 推给前端,开始/结束为 agent.start / agent.done。
type AgentService struct {
	notify         func(name string, data any)
	sessions       session.Service
	snapshotter    aguirunner.MessagesSnapshotter
	snapshotRun    runner.Runner
	memory         *memoryRuntime
	attachments    *ChatAttachmentService
	permissions    *PermissionService
	webSearch      *WebSearchService
	artifacts      *ArtifactService
	historyMu      sync.Mutex
	historyGate    chan struct{}
	activeRunMu    sync.Mutex
	activeRuns     map[int64]struct{}
	activeRequests map[string]activeChatRequest
	codingCommands *codingCommandManager
	closeOnce      sync.Once
	closeErr       error
}

type activeChatRequest struct {
	conversationID int64
	cancel         context.CancelFunc
}

func (s *AgentService) codingCommandManager() *codingCommandManager {
	s.activeRunMu.Lock()
	defer s.activeRunMu.Unlock()
	if s.codingCommands == nil {
		s.codingCommands = newCodingCommandManager()
	}
	return s.codingCommands
}

func (s *AgentService) beginConversationRun(conversationID int64) bool {
	if conversationID <= 0 {
		return false
	}
	s.activeRunMu.Lock()
	defer s.activeRunMu.Unlock()
	if s.activeRuns == nil {
		s.activeRuns = make(map[int64]struct{})
	}
	if _, running := s.activeRuns[conversationID]; running {
		return false
	}
	s.activeRuns[conversationID] = struct{}{}
	return true
}

func (s *AgentService) finishConversationRun(conversationID int64) {
	if conversationID <= 0 {
		return
	}
	s.activeRunMu.Lock()
	delete(s.activeRuns, conversationID)
	s.activeRunMu.Unlock()
}

func (s *AgentService) beginChatRequest(requestID string, cancel context.CancelFunc) {
	if strings.TrimSpace(requestID) == "" {
		return
	}
	s.activeRunMu.Lock()
	if s.activeRequests == nil {
		s.activeRequests = make(map[string]activeChatRequest)
	}
	s.activeRequests[requestID] = activeChatRequest{cancel: cancel}
	s.activeRunMu.Unlock()
}

func (s *AgentService) setChatRequestConversation(requestID string, conversationID int64) {
	if strings.TrimSpace(requestID) == "" || conversationID <= 0 {
		return
	}
	s.activeRunMu.Lock()
	if active, ok := s.activeRequests[requestID]; ok {
		active.conversationID = conversationID
		s.activeRequests[requestID] = active
	}
	s.activeRunMu.Unlock()
}

func (s *AgentService) finishChatRequest(requestID string) {
	if strings.TrimSpace(requestID) == "" {
		return
	}
	s.activeRunMu.Lock()
	delete(s.activeRequests, requestID)
	s.activeRunMu.Unlock()
}

// CancelChat stops the active request identified by the client-generated request ID.
func (s *AgentService) CancelChat(req ChatCancelRequest) bool {
	if strings.TrimSpace(req.RequestID) == "" {
		return false
	}
	s.activeRunMu.Lock()
	active, ok := s.activeRequests[req.RequestID]
	if ok && req.ConversationID > 0 && active.conversationID > 0 && active.conversationID != req.ConversationID {
		ok = false
	}
	s.activeRunMu.Unlock()
	if !ok {
		logInfo(withLogContext(context.Background(), req.ConversationID, req.RequestID), "chat.cancel", "accepted", false)
		return false
	}
	active.cancel()
	conversationID := active.conversationID
	if conversationID == 0 {
		conversationID = req.ConversationID
	}
	logInfo(withLogContext(context.Background(), conversationID, req.RequestID), "chat.cancel", "accepted", true)
	return true
}

// SetNotify 注入事件广播回调(main 装配时设置)。
//
//wails:ignore
func (s *AgentService) SetNotify(fn func(name string, data any)) { s.notify = fn }

func (s *AgentService) emit(name string, data any) {
	if s.notify != nil {
		s.notify(name, data)
	}
}

// systemInstruction 是 Miel 办公助手的人设与能力说明。
const systemInstruction = `你是 Miel,运行在本地的办公 Agent,通过工具管理用户的待办与里程碑,
并基于操作日志生成周报与办公文档。规则:
1. 使用与用户相同的语言回复。
2. 需要操作待办、提醒、办公产出、生词本或联网检索时,先用 skill_load 加载对应的 todo、reminder、office、vocabulary 或 websearch skill,
   再严格按照 skill 文档通过 skill_run 执行命令。
3. 涉及截止时间但用户未给出具体日期时应追问;修改或删除待办前先查询并核对 ID。
4. 用户询问待办、统计、进度或周报时必须执行 skill 命令读取真实数据,不要编造。
5. 回复保持简洁,尽量用 Markdown 结构化。
6. 用户 skills 目录中的文档型 Skill 加载后按文档使用受控工具；只有明确提供可执行入口的 Skill 才通过 skill_run 的 run 命令执行。
	不要把 Markdown 代码块当成 Skill 入口，也不要用 execute_command 绕过可执行 Skill 的依赖初始化。
	外部 Skill 成功时，skill_run 会返回 outputPaths 和 artifacts；这些已是最终交付，直接向用户报告，
	不要为检查、移动、重复发布或重复执行而继续调用任何工具。
7. memory_search 用于查询与当前请求有关的长期记忆;仅当用户明确要求记住时调用 memory_add。
   不保存凭据、密钥、密码、隐私秘密、模型推测或工具输出。
8. 需要查看或修改工作区文件时使用 list_directory、read_file、write_file;
   需要运行工作区本地命令时使用 execute_command;需要获取网页时使用 fetch_url。
   execute_command 不可执行已安装 Skill 目录中的脚本；这类调用必须使用 skill_run 的 run 命令。
9. 当工作区中的文件是用户要求的最终交付物时，使用 publish_artifact 发布；不要发布临时文件、日志或普通编辑。`

const plainChatInstruction = `你是 Miel。当前模型不支持工具调用，本轮只能进行普通对话。规则:
1. 使用与用户相同的语言回复。
2. 回复保持简洁，尽量用 Markdown 结构化。
3. 不要声称已经读取或修改待办、记忆、文件、工作区或其它本地数据。
4. 当用户要求执行本地操作时，明确说明当前模型仅支持纯聊天，并建议切换到支持 Agent 工具的模型。`

const planInstruction = `你是 Miel 的计划代理。你的任务是根据用户目标和工作区事实制定可执行计划，而不是实施计划。规则:
1. 使用与用户相同的语言，只输出完整的 Markdown 计划。
2. 必须先通过可用的只读工具核对仓库事实；没有工作区或无需检查时可直接规划。
3. 不得声称已经修改文件、运行实现命令、提交、推送或执行任何有副作用的操作。
4. 计划至少包含：目标摘要、实现变更、公开接口或数据变化、测试与验收、明确假设。
5. 当前请求若是对已有计划的修改意见，输出替代旧版本的完整计划，不要只输出差异。
6. 保持计划决策完整且紧凑，让执行代理无需再次选择实现方向。`

const executionInstruction = `你是 Miel 的执行代理。用户已经明确批准一个计划。规则:
1. 严格完成运行上下文中的已批准计划，并持续工作到验证结束。
2. 批准计划不等于工具授权；所有工具仍须遵守当前权限模式。
3. 普通实现细节可按仓库实际情况调整。若必须改变需求范围、公开行为、数据结构或外部副作用，先调用 request_plan_revision，随后在最终回复中给出完整替代计划并停止继续修改。
4. 已经完成的工作不要回滚；如需修订，清楚说明当前现场状态。
5. 使用与用户相同的语言，最终简洁说明改动、验证和任何剩余风险。`

const codingInstruction = `你是 Miel 的 Coding Agent，负责在用户选择的工作区内完成软件工程任务。规则:
1. 使用与用户相同的语言，先读取和搜索仓库事实，再修改代码。
2. 优先使用 read_code_file、search_code 和 apply_patch；修改前使用 SHA-256 防止覆盖并发改动。
3. 使用 start_command、read_command、write_command、stop_command 运行和管理测试、构建及开发服务。
4. 使用 git_inspect 和 inspect_changes 审查改动；保留用户已有修改，不擅自回滚。
5. 不处理待办、提醒、生词本、办公 Skills 或办公记忆；不要声称拥有这些能力。
6. 持续工作到实现与验证完成，最终简洁说明改动、验证和剩余风险。`

const codingPlanInstruction = `你是 Miel 的 Coding 计划代理，只制定软件工程计划，不实施。规则:
1. 使用与用户相同的语言，只输出完整 Markdown 计划。
2. 使用只读代码、搜索和 Git 工具核对仓库事实。
3. 不修改文件、不运行命令、不执行 Skill，也不声称已提交或推送。
4. 计划覆盖实现、接口或数据变化、测试验收和明确假设，并做到无需执行者再次选择方向。`

const codingExecutionInstruction = `你是 Miel 的 Coding 执行代理。严格执行运行上下文中的已批准计划，持续到测试和变更审查完成。工具仍服从当前权限模式。若必须改变需求范围、公开行为、数据结构或外部副作用，调用 request_plan_revision，给出完整替代计划并暂停。保留用户已有修改。`

// ChatRequest 一次对话入参。
type ChatRequest struct {
	ConversationID      int64    `json:"conversationId"` // 0 = 新建会话
	Message             string   `json:"message"`
	Reasoning           string   `json:"reasoning"` // "" 默认/关闭 | off | on | low | medium | high | xhigh | max
	RequestID           string   `json:"requestId"`
	AttachmentIDs       []string `json:"attachmentIds"`
	WorkspacePath       string   `json:"workspacePath"`
	PermissionSessionID string   `json:"permissionSessionId"`
	Mode                string   `json:"mode"`         // chat | plan；execute 仅供 ExecutePlan 内部使用
	AgentProfile        string   `json:"agentProfile"` // work | coding
	PlanID              int64    `json:"planId"`
	PlanRevision        int      `json:"planRevision"`
}

// ChatCancelRequest identifies the active request a user wants to stop.
type ChatCancelRequest struct {
	ConversationID int64  `json:"conversationId"`
	RequestID      string `json:"requestId"`
}

// applyReasoning 把思考档位映射到 GenerationConfig。
// 档位取值:"" 服务商默认/关闭 | off | on | low | medium | high | xhigh | max。
// 与内置模型目录(catalog.json)联动:关闭可切换模型时显式下发 false;
// 目录标注 none / always 的模型不发参数;effort 模型只在其声明的 levels 内
// 下发 reasoning_effort。
//   - qwen -> enable_thinking(bool);deepseek/hunyuan/glm/minimax -> thinking.type=enabled
//   - deepseek effort 模型另附 reasoning_effort;openai(o 系)只发 reasoning_effort
//   - Herdsman 按动态能力下发 thinking_enabled / reasoning_effort
//   - OpenRouter 与自定义兼容网关:reasoning_effort 尽力透传
func applyReasoning(gc *model.GenerationConfig, kind, model, level string) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	model = strings.ToLower(strings.TrimSpace(model))
	lv := strings.ToLower(strings.TrimSpace(level))
	if kind == "herdsman" {
		switch lv {
		case "":
			return
		case "off", "none":
			off := false
			gc.ThinkingEnabled = &off
			return
		case "on":
			on := true
			gc.ThinkingEnabled = &on
			return
		case "low", "medium", "high", "xhigh", "max":
			on := true
			gc.ThinkingEnabled = &on
			gc.ReasoningEffort = &lv
			return
		default:
			return
		}
	}
	spec := modelReasoning(kind, model)
	if lv == "" || lv == "off" || lv == "none" {
		// nil 表示采用服务商默认值，并不代表关闭。仅对明确支持逐请求
		// 开关的模型下发 false；DeepSeek v4 的 effort 模型也支持该开关。
		if spec.Type == ReasoningToggle || (kind == "deepseek" && spec.Type == ReasoningEffort) {
			off := false
			gc.ThinkingEnabled = &off
		}
		return
	}
	eff := lv
	if eff == "on" {
		eff = "high"
	}

	isCompatibleGateway := kind == "custom" || kind == "openrouter"
	if (spec.Type == ReasoningNone || spec.Type == ReasoningAlways) && !isCompatibleGateway {
		// 模型不支持/思考常开,无需下发任何参数;兼容网关走尽力透传。
		return
	}
	inLevels := false
	for _, l := range spec.Levels {
		if l == eff {
			inLevels = true
			break
		}
	}

	switch kind {
	case "qwen", "hunyuan", "glm", "minimax":
		// 布尔/对象式思考开关,无档位概念
		on := true
		gc.ThinkingEnabled = &on
	case "deepseek":
		on := true
		gc.ThinkingEnabled = &on
		// DeepSeek effort 模型(v4 系)附档位
		if spec.Type == ReasoningEffort && inLevels {
			gc.ReasoningEffort = &eff
		}
	case "openai", "volcengine-plan":
		// 仅对目录明确声明的档位透传，避免向不支持的模型发送参数。
		if spec.Type == ReasoningEffort && inLevels {
			gc.ReasoningEffort = &eff
		}
	default:
		// 自定义兼容网关模型不在目录内:reasoning_effort 尽力透传,
		// 模型不支持时由服务商反馈;其它未登记 kind 不发(目录为准)。
		if isCompatibleGateway && (eff == "low" || eff == "medium" || eff == "high" || eff == "xhigh") {
			gc.ReasoningEffort = &eff
		}
	}
}

// 思考规格类型常量。
const (
	ReasoningToggle = "toggle" // 可开关(thinking / enable_thinking)
	ReasoningEffort = "effort" // 开关 + 档位(reasoning_effort)
	ReasoningAlways = "always" // 思考常开,无需参数
	ReasoningNone   = "none"   // 不支持思考参数
)

// ChatResult 一次对话的结果(前端可据此刷新会话)。
type ChatResult struct {
	ConversationID int64        `json:"conversationId"`
	Answer         string       `json:"answer"`
	Metrics        *ChatMetrics `json:"metrics,omitempty"`
}

// Chat 执行一轮 Agent 对话;流式增量通过事件推给前端。
func (s *AgentService) Chat(callCtx context.Context, req ChatRequest) (result ChatResult, retErr error) {
	msg := strings.TrimSpace(req.Message)
	convID := req.ConversationID
	if callCtx == nil {
		callCtx = context.Background()
	}
	runCtx, cancelRun := context.WithCancel(callCtx)
	s.beginChatRequest(req.RequestID, cancelRun)
	defer func() {
		s.finishChatRequest(req.RequestID)
		cancelRun()
	}()
	wasStopped := func() bool { return errors.Is(runCtx.Err(), context.Canceled) }
	mode := normalizeChatMode(req.Mode)
	if mode == "" {
		return ChatResult{}, errors.New("不支持的聊天模式")
	}
	profile := normalizeAgentProfile(req.AgentProfile)
	if profile == "" {
		return ChatResult{}, errors.New("不支持的 Agent 模式")
	}
	lockedConversationID := int64(0)
	var execution *planExecutionContext
	var executionRunID int64
	chatStartedAt := time.Now()
	logCtx := withLogContext(runCtx, convID, req.RequestID)
	logInfo(logCtx, "chat.start", "has_text", msg != "", "attachment_count", len(req.AttachmentIDs))
	var userMessageID int64
	runErrorForwarded := false
	runErrorPersisted := false
	defer func() {
		if wasStopped() {
			if executionRunID > 0 {
				if err := finishPlanRun(executionRunID, execution.Plan.ID, "interrupted", "用户已停止生成"); err != nil {
					log.Println("finish interrupted plan run:", err)
				}
			}
			logInfo(logCtx, "chat.finish", "status", "stopped", "duration_ms", time.Since(chatStartedAt).Milliseconds())
			result = ChatResult{ConversationID: convID}
			retErr = nil
			return
		}
		if retErr != nil && executionRunID > 0 {
			if err := finishPlanRun(executionRunID, execution.Plan.ID, "failed", retErr.Error()); err != nil {
				log.Println("finish failed plan run:", err)
			}
		}
		if retErr == nil {
			logInfo(logCtx, "chat.finish", "status", "done", "duration_ms", time.Since(chatStartedAt).Milliseconds())
			return
		}
		logError(logCtx, "chat.finish", retErr, "status", "failed", "duration_ms", time.Since(chatStartedAt).Milliseconds())
		runErr := normalizeChatRunError("", retErr.Error())
		if !runErrorForwarded {
			s.emitChatRunError(convID, req.RequestID, runErr)
		}
		if !runErrorPersisted {
			if _, err := s.saveChatRunError(convID, userMessageID, req.RequestID, runErr); err != nil {
				log.Println("save chat run error:", err)
			}
		}
	}()
	if msg == "" && len(req.AttachmentIDs) == 0 {
		return ChatResult{}, errors.New("消息不能为空")
	}
	if store == nil {
		return ChatResult{}, errors.New("存储未初始化")
	}
	if convID > 0 {
		if !s.beginConversationRun(convID) {
			return ChatResult{}, errConversationBusy
		}
		lockedConversationID = convID
		defer func() { s.finishConversationRun(lockedConversationID) }()
	}
	if mode == "execute" {
		if convID <= 0 {
			return ChatResult{}, errors.New("执行计划需要已有会话")
		}
		var err error
		execution, err = loadPlanExecution(convID, req.PlanID, req.PlanRevision)
		if err != nil {
			return ChatResult{}, err
		}
		profile = normalizeAgentProfile(execution.Revision.AgentProfile)
		if profile == "" {
			profile = AgentProfileWork
		}
		selected, selectErr := profileModelForConversation(convID, profile)
		if selectErr != nil {
			return ChatResult{}, selectErr
		}
		if _, selectErr = store.Exec(`UPDATE conversations SET agent_profile = ?, provider_id = ?, model = ?, updated_at = ? WHERE id = ?`, profile, selected.ProviderID, selected.Model, now(), convID); selectErr != nil {
			return ChatResult{}, selectErr
		}
	} else if mode == "plan" && req.PlanID > 0 {
		if _, err := loadPlanExecution(convID, req.PlanID, req.PlanRevision); err != nil {
			return ChatResult{}, err
		}
	}
	if convID > 0 && mode != "execute" {
		var storedProfile string
		if err := store.QueryRow(`SELECT agent_profile FROM conversations WHERE id = ?`, convID).Scan(&storedProfile); err != nil {
			return ChatResult{}, err
		}
		if normalizeAgentProfile(storedProfile) != profile {
			return ChatResult{}, errors.New("Agent 模式已变化，请刷新后重试")
		}
	}
	workspacePath := ""
	if settingsSvc != nil {
		workspacePath = settingsSvc.agentWorkspacePath(req.WorkspacePath)
	}
	p, err := providerForConversationProfile(convID, profile)
	if err != nil {
		return ChatResult{}, err
	}
	toolsEnabled := true
	if supported, known := settingsSvc.providerSupportsTools(p); known {
		toolsEnabled = supported
	}
	if mode == "execute" && !toolsEnabled {
		return ChatResult{}, errors.New("当前对话模型不支持工具调用，请切换模型后重试")
	}
	if len(req.AttachmentIDs) > 0 {
		if s.attachments == nil {
			return ChatResult{}, errors.New("图片附件服务未初始化")
		}
		if !providerSupportsVision(p) {
			return ChatResult{}, errors.New("当前模型不支持图片输入，请切换到支持图片的模型")
		}
		if _, err := s.attachments.validateDrafts(req.AttachmentIDs); err != nil {
			return ChatResult{}, err
		}
	}
	m, err := buildChatModel(p)
	if err != nil {
		return ChatResult{}, err
	}
	memoryConfig := MemoryConfig{}
	memoryEnabled := false
	if s.memory != nil && settingsSvc != nil {
		if cfg, cfgErr := settingsSvc.memoryConfig(); cfgErr == nil {
			memoryConfig = cfg
			memoryEnabled = profile == AgentProfileWork && cfg.Enabled && toolsEnabled && mode != "plan"
		} else {
			log.Println("load memory config:", cfgErr)
		}
	}

	messageType := "chat"
	if mode == "plan" {
		messageType = "plan_request"
	} else if mode == "execute" {
		messageType = "plan_execution"
	}
	setupCtx, cancelSetup := context.WithTimeout(runCtx, 15*time.Second)
	defer cancelSetup()
	logInfo(logCtx, "chat.persist.start")
	convID, userMessageID, err = s.saveUserMessageWithMeta(setupCtx, convID, msg, req.AttachmentIDs, messageType, p, profile, req.PlanID, req.PlanRevision)
	if err != nil {
		return ChatResult{}, err
	}
	s.setChatRequestConversation(req.RequestID, convID)
	if lockedConversationID == 0 {
		if !s.beginConversationRun(convID) {
			return ChatResult{}, errConversationBusy
		}
		lockedConversationID = convID
		defer func() { s.finishConversationRun(lockedConversationID) }()
	}
	if mode == "execute" {
		executionRunID, err = beginPlanRun(execution.Plan.ID, execution.Revision.Revision, req.RequestID, p, profile)
		if err != nil {
			return ChatResult{}, err
		}
	}
	logCtx = withLogContext(logCtx, convID, req.RequestID)
	logInfo(logCtx, "chat.persist.finish", "message_id", userMessageID)
	logInfo(logCtx, "chat.input.saved", "message_id", userMessageID)
	s.emit("agent.input.saved", map[string]any{
		"conversationId": convID,
		"messageId":      userMessageID,
		"requestId":      req.RequestID,
	})

	// 以库中历史(含刚保存的用户消息)seed 本次运行,保持多轮上下文
	hist, err := s.loadMessages(convID)
	if err != nil {
		return ChatResult{}, err
	}
	if s.sessions == nil {
		return ChatResult{}, errors.New("AG-UI 会话存储未初始化")
	}
	// AG-UI 每轮会自行记录最新用户消息;这里只迁移此前的旧格式历史。
	if err := s.ensureAGUIHistory(setupCtx, convID, hist[:len(hist)-1]); err != nil {
		return ChatResult{}, err
	}
	cancelSetup()

	// 思考档位:按服务商机制映射(布尔/对象式 thinking 或 reasoning_effort 级别)
	gc := model.GenerationConfig{Stream: true}
	applyReasoning(&gc, p.Kind, p.Model, req.Reasoning)

	sourceContext := &todoToolSource{input: todoSourceInput{
		Kind: "conversation", TextContent: msg, ConversationID: convID, MessageID: userMessageID,
	}}
	var agentTools []tool.Tool
	deviation := &planRevisionSignal{}
	publisher := &runArtifactService{ArtifactService: s.artifacts, scope: artifactScope{ConversationID: convID, RequestID: req.RequestID, UserMessageID: userMessageID}}
	if toolsEnabled {
		if profile == AgentProfileCoding && mode == "plan" {
			agentTools = codingReadOnlyTools(s.permissions, workspacePath, req.PermissionSessionID)
		} else if profile == AgentProfileCoding {
			agentTools = codingAgentTools(s.permissions, workspacePath, req.PermissionSessionID, convID, s.codingCommandManager())
			if mode == "execute" {
				agentTools = append(agentTools, newPlanRevisionTool(deviation))
			}
		} else if mode == "plan" {
			agentTools = planAgentTools(s.permissions, workspacePath, req.PermissionSessionID)
		} else {
			agentTools = chatAgentTools(sourceContext, nil, s.permissions, workspacePath, req.PermissionSessionID, s.webSearch)
			if memoryEnabled {
				agentTools = chatAgentTools(sourceContext, s.memory.Tools(), s.permissions, workspacePath, req.PermissionSessionID, s.webSearch)
			}
			if mode == "execute" {
				agentTools = append(agentTools, newPlanRevisionTool(deviation))
			}
		}
	}
	if toolsEnabled && profile == AgentProfileWork && mode != "plan" && s.artifacts != nil {
		for index, candidate := range agentTools {
			if callable, ok := candidate.(tool.CallableTool); ok && candidate.Declaration().Name == "skill_run" {
				agentTools[index] = &artifactTool{CallableTool: callable, publication: publisher}
			}
		}
		if s.permissions != nil && strings.TrimSpace(req.PermissionSessionID) != "" {
			env := permissionToolEnv{service: s.permissions, workspace: workspacePath, sessionID: req.PermissionSessionID}
			agentTools = append(agentTools, newPublishArtifactTool(env, publisher))
		}
	}
	instruction := instructionWithContext(workspacePath)
	if profile == AgentProfileCoding && mode == "plan" {
		instruction = codingInstructionWithContext(codingPlanInstruction, workspacePath)
	} else if profile == AgentProfileCoding && mode == "execute" {
		instruction = codingInstructionWithContext(codingExecutionInstruction, workspacePath)
	} else if profile == AgentProfileCoding {
		instruction = codingInstructionWithContext(codingInstruction, workspacePath)
	} else if mode == "plan" {
		instruction = planInstructionWithContext(workspacePath)
	} else if mode == "execute" {
		instruction = executionInstructionWithContext(workspacePath)
	} else if !toolsEnabled {
		instruction = plainChatInstruction
	}
	opts := []llmagent.Option{
		llmagent.WithModel(m),
		llmagent.WithInstruction(instruction),
		llmagent.WithGenerationConfig(gc),
		llmagent.WithMaxToolIterations(maxAgentToolIterations),
	}
	var skillRepo skill.Repository
	// skills/<name>/SKILL.md 即插即用。纯聊天模型不注册任何 Skill 工具。
	if toolsEnabled && profile == AgentProfileWork && mode != "plan" {
		if repo, err := newManagedSkillRepository(skillsDir()); err == nil {
			skillRepo = repo
		} else {
			log.Println("load skills repo:", err)
		}
	}
	opts = append(opts, chatCapabilityOptions(toolsEnabled, agentTools, skillRepo, memoryEnabled)...)
	agent := llmagent.New("blankmind", opts...)
	runnerOpts := []runner.Option{runner.WithSessionService(s.sessions)}
	if s.artifacts != nil {
		runnerOpts = append(runnerOpts, runner.WithArtifactService(publisher))
	}
	if memoryEnabled {
		runnerOpts = append(runnerOpts, runner.WithMemoryService(s.memory))
	}
	baseR := runner.NewRunner("blankmind-app", agent, runnerOpts...)
	defer baseR.Close()

	// AG-UI 协议化:内部 runner 事件经 agui/runner 翻译成标准 AG-UI 事件
	// (RUN/Text/ToolCall/Reasoning…),逐个经 agent.agui 推给前端;
	// 同时保留 agent.chunk 纯文本流供现有 UI 与落库。
	var traceUsage *model.Usage
	var translatedErrorCode string
	callbacks := aguitranslator.NewCallbacks().RegisterBeforeTranslate(
		func(_ context.Context, ev *agentevent.Event) (*agentevent.Event, error) {
			if ev != nil && ev.ExecutionTrace != nil && ev.ExecutionTrace.Usage != nil {
				traceUsage = cloneMetricsUsage(ev.ExecutionTrace.Usage)
			}
			if ev != nil && ev.Response != nil && ev.Response.Error != nil && ev.Response.Error.Code != nil {
				translatedErrorCode = strings.TrimSpace(*ev.Response.Error.Code)
			}
			return nil, nil
		},
	)
	aguiR := aguirunner.New(baseR,
		aguirunner.WithAppName(aguiAppName),
		aguirunner.WithSessionService(s.sessions),
		aguirunner.WithReasoningContentEnabled(true),
		aguirunner.WithToolCallDeltaStreamingEnabled(true),
		aguirunner.WithTranslateCallbacks(callbacks),
		aguirunner.WithRunOptionResolver(func(context.Context, *adapter.RunAgentInput) ([]agentcore.RunOption, error) {
			runOpts := []agentcore.RunOption{
				agentcore.WithExecutionTraceEnabled(true),
				agentcore.WithLateContextMessages(runLateContextMessages(time.Now(), execution)),
			}
			if fields := modelRequestCacheFields(p, convID); len(fields) > 0 {
				runOpts = append(runOpts, agentcore.WithModelRequestExtraFields(fields))
			}
			return runOpts, nil
		}),
	)

	runLogCtx := withLogContext(runCtx, convID, req.RequestID)
	ctx := WithPermissionContext(runLogCtx, s.permissions, req.PermissionSessionID, workspacePath)
	threadID := "conv-" + strconv.FormatInt(convID, 10)
	aguiMsgs := make([]aguitypes.Message, 0, len(hist))
	for _, h := range hist {
		aguiMessage, err := aguiMessageFromChatMessage(h)
		if err != nil {
			return ChatResult{}, err
		}
		aguiMsgs = append(aguiMsgs, aguiMessage)
	}
	input := &adapter.RunAgentInput{
		ThreadID: threadID,
		RunID:    "run-" + strconv.FormatInt(time.Now().UnixNano(), 10),
		Messages: aguiMsgs,
	}
	startedAt := time.Now()
	logInfo(ctx, "model.run.start", "provider", p.Kind, "model", p.Model, "max_tool_iterations", maxAgentToolIterations, "tools_enabled", toolsEnabled)
	aguiEvents, err := aguiR.Run(ctx, input)
	if err != nil {
		if wasStopped() {
			return ChatResult{ConversationID: convID}, nil
		}
		logError(ctx, "model.run.start_failed", err, "provider", p.Kind, "model", p.Model)
		return ChatResult{}, err
	}

	var answer strings.Builder
	var answerMessageID string
	var runErr *ChatRunError
	toolCallsObserved := 0
	s.emit("agent.start", map[string]any{"conversationId": convID, "requestId": req.RequestID})
	for ev := range aguiEvents {
		if ev == nil {
			continue
		}
		isRunError := false
		if failed, ok := ev.(*aguievents.RunErrorEvent); ok && strings.TrimSpace(failed.Message) != "" {
			code := translatedErrorCode
			if failed.Code != nil && strings.TrimSpace(*failed.Code) != "" {
				code = strings.TrimSpace(*failed.Code)
			}
			normalized := normalizeChatRunError(code, failed.Message)
			failed.Code = &normalized.Code
			runErr = &normalized
			isRunError = true
		}
		if b, mErr := ev.ToJSON(); mErr == nil {
			var event map[string]any
			if json.Unmarshal(b, &event) == nil {
				eventType, _ := event["type"].(string)
				switch eventType {
				case "TOOL_CALL_START":
					toolCallsObserved++
					logInfo(ctx, "tool.start", "tool_call_number", toolCallsObserved, "tool_call_id", event["toolCallId"], "tool", event["toolCallName"])
				case "TOOL_CALL_END":
					logInfo(ctx, "tool.arguments.complete", "tool_call_id", event["toolCallId"])
				case "TOOL_CALL_RESULT":
					logInfo(ctx, "tool.result.received", "tool_call_id", event["toolCallId"])
				case "RUN_ERROR":
					logError(ctx, "model.run.failed", errors.New(sanitizeLogText(fmt.Sprint(event["message"]))), "code", event["code"], "tool_calls_observed", toolCallsObserved, "duration_ms", time.Since(startedAt).Milliseconds())
				}
				if event["type"] == "CUSTOM" && event["name"] == "tool.artifacts" && s.artifacts != nil {
					if value, ok := event["value"].(map[string]any); ok {
						toolCallID, _ := value["toolCallId"].(string)
						if refs, err := s.artifacts.linkRefs(convID, req.RequestID, toolCallID); err == nil {
							value["artifacts"] = refs
						}
					}
				}
				redactAGUIBinaryContent(event)
				s.emit("agent.agui", map[string]any{
					"conversationId": convID,
					"requestId":      req.RequestID,
					"event":          event,
				})
				if isRunError {
					runErrorForwarded = true
				}
			}
		}
		if te, ok := ev.(*aguievents.TextMessageContentEvent); ok && te.Delta != "" {
			answer.WriteString(te.Delta)
			answerMessageID = te.MessageID
			s.emit("agent.chunk", map[string]any{
				"conversationId": convID,
				"requestId":      req.RequestID,
				"delta":          te.Delta,
			})
		}
	}
	out := strings.TrimSpace(answer.String())
	deviationRequested, deviationReason := deviation.snapshot()
	var metrics *ChatMetrics
	var assistantMessageID int64
	if out != "" && (!wasStopped() || mode == "chat") {
		built := buildChatMetrics(p.Model, traceUsage, time.Since(startedAt))
		metrics = &built
		messageType := "chat"
		if mode == "plan" || (mode == "execute" && deviationRequested) {
			messageType = "plan_response"
		}
		assistantMessageID, err = s.saveMessageWithType(convID, "assistant", out, messageType, answerMessageID, metrics, profile, req.PlanID, req.PlanRevision)
		if err != nil {
			return ChatResult{}, err
		}
	}
	if runErr != nil && !wasStopped() {
		if s.artifacts != nil {
			_ = s.artifacts.finishRun(publisher.scope, answerMessageID)
		}
		if _, err := s.saveChatRunError(convID, userMessageID, req.RequestID, *runErr); err != nil {
			log.Println("save chat run error:", err)
		} else {
			runErrorPersisted = true
		}
		s.emit("conversations.changed", "updated")
		return ChatResult{}, errors.New(runErr.Message)
	}
	if wasStopped() {
		if s.artifacts != nil {
			_ = s.artifacts.finishRun(publisher.scope, answerMessageID)
		}
		s.emit("agent.done", map[string]any{"conversationId": convID, "requestId": req.RequestID, "answer": out, "metrics": metrics})
		s.emit("conversations.changed", "updated")
		return ChatResult{ConversationID: convID, Answer: out, Metrics: metrics}, nil
	}
	if err := finalChatError(out, nil); err != nil {
		if s.artifacts != nil {
			_ = s.artifacts.finishRun(publisher.scope, answerMessageID)
		}
		return ChatResult{}, err
	}
	if mode == "plan" {
		if _, err := savePlanRevision(convID, req.PlanID, req.PlanRevision, userMessageID, assistantMessageID, out, p, profile); err != nil {
			return ChatResult{}, err
		}
	}
	if mode == "execute" {
		if deviationRequested {
			if _, err := savePlanRevision(convID, execution.Plan.ID, execution.Revision.Revision, userMessageID, assistantMessageID, out, p, profile); err != nil {
				return ChatResult{}, err
			}
			if err := finishPlanRun(executionRunID, execution.Plan.ID, "paused", deviationReason); err != nil {
				return ChatResult{}, err
			}
		} else if err := finishPlanRun(executionRunID, execution.Plan.ID, "completed", ""); err != nil {
			return ChatResult{}, err
		}
		executionRunID = 0
	}
	if s.artifacts != nil {
		_ = s.artifacts.finishRun(publisher.scope, answerMessageID)
	}
	s.emit("agent.done", map[string]any{"conversationId": convID, "requestId": req.RequestID, "answer": out, "metrics": metrics})
	logInfo(ctx, "model.run.finish", "status", "done", "tool_calls_observed", toolCallsObserved, "duration_ms", time.Since(startedAt).Milliseconds())
	s.emit("conversations.changed", "updated")
	if mode == "chat" && memoryEnabled && memoryConfig.AutoExtract && msg != "" {
		if err := s.memory.enqueue(m, memoryConfig, msg, out); err != nil {
			log.Println("enqueue memory extraction:", err)
		}
	}
	return ChatResult{ConversationID: convID, Answer: out, Metrics: metrics}, nil
}

func cloneMetricsUsage(usage *model.Usage) *model.Usage {
	if usage == nil {
		return nil
	}
	cloned := *usage
	if usage.TimingInfo != nil {
		timing := *usage.TimingInfo
		cloned.TimingInfo = &timing
	}
	return &cloned
}

func buildChatMetrics(modelName string, usage *model.Usage, elapsed time.Duration) ChatMetrics {
	metrics := ChatMetrics{Model: modelName, DurationMs: elapsed.Milliseconds()}
	if usage == nil {
		return metrics
	}
	metrics.PromptTokens = usage.PromptTokens
	metrics.CompletionTokens = usage.CompletionTokens
	metrics.TotalTokens = usage.TotalTokens
	metrics.ReasoningTokens = usage.CompletionTokensDetails.ReasoningTokens
	metrics.CachedTokens = usage.PromptTokensDetails.CachedTokens
	generationDuration := elapsed
	if usage.TimingInfo != nil {
		metrics.FirstTokenMs = usage.TimingInfo.FirstTokenDuration.Milliseconds()
		if usage.TimingInfo.FirstTokenDuration > 0 && usage.TimingInfo.FirstTokenDuration < elapsed {
			generationDuration -= usage.TimingInfo.FirstTokenDuration
		}
	}
	if usage.CompletionTokens > 0 && generationDuration > 0 {
		metrics.TokensPerSecond = float64(usage.CompletionTokens) / generationDuration.Seconds()
	}
	return metrics
}

func knowledgeOnlySkillOptions(repo skill.Repository) []llmagent.Option {
	return []llmagent.Option{
		llmagent.WithSkills(repo),
		llmagent.WithSkillToolProfile(llmagent.SkillToolProfileKnowledgeOnly),
		llmagent.WithWorkspaceExecSurfaceEnabled(false),
	}
}

func chatCapabilityOptions(toolsEnabled bool, agentTools []tool.Tool, repo skill.Repository, preloadMemory bool) []llmagent.Option {
	if !toolsEnabled {
		return nil
	}
	opts := []llmagent.Option{llmagent.WithTools(agentTools)}
	if preloadMemory {
		opts = append(opts, llmagent.WithPreloadMemory(8))
	}
	if repo != nil {
		opts = append(opts, knowledgeOnlySkillOptions(repo)...)
	}
	return opts
}

func finalChatError(answer string, runErr error) error {
	if runErr != nil {
		return runErr
	}
	if strings.TrimSpace(answer) != "" {
		return nil
	}
	return errors.New("模型未返回有效内容")
}

func instructionWithContext(workspace string) string {
	instruction := systemInstruction
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return instruction + "\n当前未选择工作区；相对文件路径和相对命令 workdir 不可用。"
	}
	return instruction + "\n当前工作区的完整绝对路径:" + workspace +
		"\n文件工具的相对路径和命令的相对 workdir 均以该工作区为基准；操作工作区内容时优先使用相对路径。"
}

func planInstructionWithContext(workspace string) string {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return planInstruction + "\n当前未选择工作区，只能依据会话上下文制定计划。"
	}
	return planInstruction + "\n当前工作区的完整绝对路径:" + workspace +
		"\n只读文件工具的相对路径均以该工作区为基准。"
}

func executionInstructionWithContext(workspace string) string {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return executionInstruction + "\n当前未选择工作区；相对文件路径和相对命令 workdir 不可用。"
	}
	return executionInstruction + "\n当前工作区的完整绝对路径:" + workspace +
		"\n文件工具的相对路径和命令的相对 workdir 均以该工作区为基准。"
}

func codingInstructionWithContext(instruction, workspace string) string {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return instruction + "\n当前未选择工作区；需要文件或命令操作时先要求用户选择工作区。"
	}
	return instruction + "\n当前工作区的完整绝对路径:" + workspace + "\n所有相对路径均以该工作区为基准。"
}

func lateContextMessages(current time.Time) []model.Message {
	return []model.Message{
		model.NewUserMessage("[运行上下文]\n当前本地时间:" + current.Format("2006-01-02 15:04:05 -07:00")),
	}
}

func runLateContextMessages(current time.Time, execution *planExecutionContext) []model.Message {
	messages := lateContextMessages(current)
	if execution == nil {
		return messages
	}
	messages = append(messages, model.NewUserMessage(fmt.Sprintf(
		"[已批准计划 v%d｜必须严格执行]\n%s",
		execution.Revision.Revision, execution.Revision.Content,
	)))
	return messages
}

func modelRequestCacheFields(provider Provider, conversationID int64) map[string]any {
	kind := strings.ToLower(strings.TrimSpace(provider.Kind))
	switch kind {
	case "herdsman":
		return map[string]any{
			"cache_prompt": true,
			"id_slot":      stableConversationSlot(provider.ID, conversationID),
		}
	case "openai":
		return map[string]any{
			"prompt_cache_key": stableConversationCacheKey(provider.ID, conversationID),
		}
	default:
		return nil
	}
}

func stableConversationCacheKey(providerID, conversationID int64) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("blankmind-chat-cache-v1:%d:%d", providerID, conversationID)))
	return fmt.Sprintf("blankmind:v1:%x", digest[:16])
}

func stableConversationSlot(providerID, conversationID int64) int64 {
	digest := sha256.Sum256([]byte(fmt.Sprintf("blankmind-chat-slot-v1:%d:%d", providerID, conversationID)))
	slot := int64(uint32(digest[0])<<24|uint32(digest[1])<<16|uint32(digest[2])<<8|uint32(digest[3])) & 0x7fffffff
	if slot == 0 {
		return 1
	}
	return slot
}

func chatAgentTools(sourceContext *todoToolSource, memoryTools []tool.Tool, args ...any) []tool.Tool {
	var search *WebSearchService
	if len(args) >= 4 {
		search, _ = args[3].(*WebSearchService)
	}
	tools := []tool.Tool{newSkillRunTool(todoSvc, sourceContext, search)}
	if len(args) >= 3 {
		permissions, _ := args[0].(*PermissionService)
		workspace, _ := args[1].(string)
		sessionID, _ := args[2].(string)
		// Quick Assistant and legacy callers do not provide a permission session;
		// keep the new interactive tools out of those calls so they cannot wait
		// on an approval card that the surface does not render.
		if permissions != nil && strings.TrimSpace(sessionID) != "" {
			tools = append(tools, controlledWorkspaceTools(permissions, workspace, sessionID)...)
		}
	}
	return append(tools, memoryTools...)
}

// ServiceShutdown closes the snapshot runner and the persistent AG-UI session store.
func (s *AgentService) ServiceShutdown() error {
	s.closeOnce.Do(func() {
		if s.codingCommands != nil {
			s.codingCommands.stopConversation(0)
		}
		if s.snapshotRun != nil {
			s.closeErr = s.snapshotRun.Close()
		}
		if s.sessions != nil {
			if err := s.sessions.Close(); s.closeErr == nil {
				s.closeErr = err
			}
		}
	})
	return s.closeErr
}

// skillsDir 返回用户 skills 目录;不存在则创建。
func skillsDir() string {
	dir, err := appDirectories.Ensure(DirectorySkills)
	if err != nil {
		log.Println("create skills dir failed:", err)
		return appDirectories.Path(DirectorySkills)
	}
	return dir
}

// newConversation 创建会话(首条消息截断为标题)。
func (s *AgentService) newConversation(first string) (Conversation, error) {
	title := strings.TrimSpace(first)
	if r := []rune(title); len(r) > 24 {
		title = string(r[:24]) + "…"
	}
	provider, _ := providerForConversationProfile(0, AgentProfileWork)
	tx, err := store.Begin()
	if err != nil {
		return Conversation{}, err
	}
	defer tx.Rollback() //nolint:errcheck
	res, err := tx.Exec(
		"INSERT INTO conversations (title, provider_id, model, agent_profile, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		title, provider.ID, provider.Model, AgentProfileWork, now(), now())
	if err != nil {
		return Conversation{}, err
	}
	id, _ := res.LastInsertId()
	if err := initializeConversationProfiles(context.Background(), tx, id, AgentProfileWork, provider); err != nil {
		return Conversation{}, err
	}
	if err := tx.Commit(); err != nil {
		return Conversation{}, err
	}
	s.emit("conversations.changed", "created")
	models := map[string]ProfileModel{AgentProfileWork: {ProviderID: provider.ID, Model: provider.Model}}
	return Conversation{ID: id, Title: title, ProviderID: provider.ID, Model: provider.Model, AgentProfile: AgentProfileWork, ProfileModels: models, CreatedAt: now(), UpdatedAt: now()}, nil
}

func (s *AgentService) saveUserMessage(conversationID int64, content string, attachmentIDs []string) (int64, int64, error) {
	return s.saveUserMessageWithMeta(context.Background(), conversationID, content, attachmentIDs, "chat", Provider{}, AgentProfileWork, 0, 0)
}

func (s *AgentService) saveUserMessageWithMeta(ctx context.Context, conversationID int64, content string, attachmentIDs []string, messageType string, provider Provider, profile string, planID int64, planRevision int) (int64, int64, error) {
	tx, err := store.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback() //nolint:errcheck
	createdConversation := conversationID == 0
	if createdConversation {
		title := strings.TrimSpace(content)
		if title == "" {
			title = "图片对话"
		}
		if r := []rune(title); len(r) > 24 {
			title = string(r[:24]) + "…"
		}
		result, err := tx.ExecContext(ctx,
			"INSERT INTO conversations (title, provider_id, model, agent_profile, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
			title, provider.ID, provider.Model, profile, now(), now())
		if err != nil {
			return 0, 0, err
		}
		conversationID, err = result.LastInsertId()
		if err != nil {
			return 0, 0, err
		}
		if err := initializeConversationProfiles(ctx, tx, conversationID, profile, provider); err != nil {
			return 0, 0, err
		}
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO messages (conversation_id, role, content, message_type, plan_id, plan_revision, agent_profile, created_at)
		VALUES (?, 'user', ?, ?, ?, ?, ?, ?)`,
		conversationID, content, messageType, planID, planRevision, profile, now())
	if err != nil {
		return 0, 0, err
	}
	messageID, err := result.LastInsertId()
	if err != nil {
		return 0, 0, err
	}
	finalize := func(bool) {}
	if len(attachmentIDs) > 0 {
		finalize, err = s.attachments.attachDrafts(tx, messageID, attachmentIDs)
		if err != nil {
			return 0, 0, err
		}
	}
	committed := false
	defer func() { finalize(committed) }()
	if _, err := tx.ExecContext(ctx, "UPDATE conversations SET updated_at = ? WHERE id = ?", now(), conversationID); err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	committed = true
	if createdConversation {
		s.emit("conversations.changed", "created")
	}
	return conversationID, messageID, nil
}

// saveMessage 追加消息并更新时间戳。
func (s *AgentService) saveMessage(convID int64, role, content, aguiMessageID string, metrics *ChatMetrics) (int64, error) {
	return s.saveMessageWithType(convID, role, content, "chat", aguiMessageID, metrics, AgentProfileWork, 0, 0)
}

func (s *AgentService) saveMessageWithType(convID int64, role, content, messageType, aguiMessageID string, metrics *ChatMetrics, profile string, planID int64, planRevision int) (int64, error) {
	tx, err := store.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck
	res, err := tx.Exec(`
		INSERT INTO messages (conversation_id, role, content, message_type, plan_id, plan_revision, agent_profile, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		convID, role, content, messageType, planID, planRevision, profile, now())
	if err != nil {
		return 0, err
	}
	messageID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if metrics != nil {
		if _, err := tx.Exec(`
			INSERT INTO message_metrics (
				message_id, agui_message_id, model, prompt_tokens, completion_tokens,
				total_tokens, reasoning_tokens, cached_tokens, duration_ms,
				first_token_ms, tokens_per_second
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			messageID, aguiMessageID, metrics.Model, metrics.PromptTokens,
			metrics.CompletionTokens, metrics.TotalTokens, metrics.ReasoningTokens,
			metrics.CachedTokens, metrics.DurationMs, metrics.FirstTokenMs,
			metrics.TokensPerSecond); err != nil {
			return 0, err
		}
	}
	if _, err := tx.Exec("UPDATE conversations SET updated_at = ? WHERE id = ?", now(), convID); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return messageID, nil
}

// loadMessages 读取会话消息(正序)。
func (s *AgentService) loadMessages(convID int64) ([]ChatMessage, error) {
	rows, err := store.Query(`
		SELECT id, conversation_id, role, content, message_type, plan_id, plan_revision, agent_profile, created_at FROM messages
		WHERE conversation_id = ? ORDER BY id ASC`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []ChatMessage
	for rows.Next() {
		var m ChatMessage
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &m.MessageType, &m.PlanID, &m.PlanRevision, &m.AgentProfile, &m.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	attachments, err := loadConversationAttachments(convID)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].Attachments = attachments[items[i].ID]
	}
	return items, nil
}

func loadConversationAttachments(conversationID int64) (map[int64][]MessageAttachment, error) {
	rows, err := store.Query(`
		SELECT ma.id, ma.message_id, ma.kind, ma.file_path, ma.thumbnail_path,
			ma.mime_type, ma.original_name, ma.width, ma.height, ma.size_bytes,
			ma.position, ma.created_at
		FROM message_attachments ma
		JOIN messages m ON m.id = ma.message_id
		WHERE m.conversation_id = ?
		ORDER BY ma.message_id, ma.position`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck
	result := make(map[int64][]MessageAttachment)
	for rows.Next() {
		var item MessageAttachment
		if err := rows.Scan(
			&item.ID, &item.MessageID, &item.Kind, &item.FilePath, &item.ThumbnailPath,
			&item.MIMEType, &item.OriginalName, &item.Width, &item.Height,
			&item.SizeBytes, &item.Position, &item.CreatedAt,
		); err != nil {
			return nil, err
		}
		result[item.MessageID] = append(result[item.MessageID], item)
	}
	return result, rows.Err()
}

func aguiMessageFromChatMessage(message ChatMessage) (aguitypes.Message, error) {
	role := aguitypes.RoleUser
	if message.Role == "assistant" {
		role = aguitypes.RoleAssistant
	}
	result := aguitypes.Message{
		ID: "m" + strconv.FormatInt(message.ID, 10), Role: role, Content: message.Content,
	}
	if message.Role != "user" || len(message.Attachments) == 0 {
		return result, nil
	}
	contents := make([]aguitypes.InputContent, 0, len(message.Attachments)+1)
	if message.Content != "" {
		contents = append(contents, aguitypes.InputContent{Type: aguitypes.InputContentTypeText, Text: message.Content})
	}
	for _, attachment := range message.Attachments {
		data, err := readLimitedFile(attachment.FilePath, maxChatAttachmentBytes)
		if err != nil {
			return aguitypes.Message{}, fmt.Errorf("读取聊天图片 %q 失败: %w", attachment.OriginalName, err)
		}
		optimized, err := optimizeImageData(data, attachment.OriginalName)
		if err != nil {
			return aguitypes.Message{}, fmt.Errorf("优化聊天图片 %q 失败: %w", attachment.OriginalName, err)
		}
		contents = append(contents, aguitypes.InputContent{
			Type: aguitypes.InputContentTypeBinary, MimeType: optimized.MIMEType,
			Data: base64.StdEncoding.EncodeToString(optimized.Data), Filename: optimized.Filename,
		})
	}
	result.Content = contents
	return result, nil
}

// ListConversations 返回会话列表(近期在前)。
func (s *AgentService) ListConversations() ([]Conversation, error) {
	if store == nil {
		return []Conversation{}, nil
	}
	rows, err := store.Query(
		`SELECT id, title, provider_id, model, agent_profile, created_at, updated_at,
		COALESCE((SELECT provider_id FROM conversation_profile_models WHERE conversation_id = conversations.id AND profile = 'work'), 0),
		COALESCE((SELECT model FROM conversation_profile_models WHERE conversation_id = conversations.id AND profile = 'work'), ''),
		COALESCE((SELECT provider_id FROM conversation_profile_models WHERE conversation_id = conversations.id AND profile = 'coding'), 0),
		COALESCE((SELECT model FROM conversation_profile_models WHERE conversation_id = conversations.id AND profile = 'coding'), '')
		FROM conversations ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Conversation
	for rows.Next() {
		var c Conversation
		var work, coding ProfileModel
		if err := rows.Scan(&c.ID, &c.Title, &c.ProviderID, &c.Model, &c.AgentProfile, &c.CreatedAt, &c.UpdatedAt, &work.ProviderID, &work.Model, &coding.ProviderID, &coding.Model); err != nil {
			return nil, err
		}
		c.ProfileModels = map[string]ProfileModel{AgentProfileWork: work, AgentProfileCoding: coding}
		items = append(items, c)
	}
	return items, rows.Err()
}

// DeleteConversation 删除会话及其消息。
func (s *AgentService) DeleteConversation(conversationID int64) error {
	if s.codingCommands != nil {
		s.codingCommands.stopConversation(conversationID)
	}
	attachments, err := loadConversationAttachments(conversationID)
	if err != nil {
		return err
	}
	tx, err := store.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec(`DELETE FROM plan_runs WHERE plan_id IN (SELECT id FROM plans WHERE conversation_id = ?)`, conversationID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM plan_revisions WHERE plan_id IN (SELECT id FROM plans WHERE conversation_id = ?)`, conversationID); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM plans WHERE conversation_id = ?", conversationID); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM conversation_profile_models WHERE conversation_id = ?", conversationID); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM chat_run_errors WHERE conversation_id = ?", conversationID); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM artifact_links WHERE conversation_id = ?", conversationID); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		DELETE FROM message_metrics WHERE message_id IN (
			SELECT id FROM messages WHERE conversation_id = ?
		)`, conversationID); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		DELETE FROM message_attachments WHERE message_id IN (
			SELECT id FROM messages WHERE conversation_id = ?
		)`, conversationID); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM messages WHERE conversation_id = ?", conversationID); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM conversations WHERE id = ?", conversationID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	for _, items := range attachments {
		for _, attachment := range items {
			if data, readErr := os.ReadFile(attachment.FilePath); readErr == nil {
				removeOptimizedImageCache(data)
			}
			_ = os.Remove(attachment.FilePath)
			_ = os.Remove(attachment.ThumbnailPath)
		}
	}
	if s.sessions != nil {
		if err := s.sessions.DeleteSession(context.Background(), aguiSessionKey(conversationID)); err != nil {
			return err
		}
	}
	s.emit("conversations.changed", "deleted")
	return nil
}
