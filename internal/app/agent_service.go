package app

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/server/agui/adapter"
	aguirunner "trpc.group/trpc-go/trpc-agent-go/server/agui/runner"
	"trpc.group/trpc-go/trpc-agent-go/session"
	"trpc.group/trpc-go/trpc-agent-go/skill"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	aguitypes "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
)

// settingsSvc / todoSvc 由 main 装配时赋值,便于 Agent 服务复用。
var (
	settingsSvc *SettingsService
	todoSvc     *TodoService
)

// AgentService 是基于 trpc-agent-go 的对话 Agent 服务:
//   - 每次 Chat 按“默认 Provider”动态构建 LLMAgent,会话可并行
//   - 办公能力(待办/统计/日志)以 function calling 暴露
//   - <数据目录>/skills 下的 SKILL.md 由框架加载,新增即插即用
//
// 流式增量经事件 agent.chunk 推给前端,开始/结束为 agent.start / agent.done。
type AgentService struct {
	notify      func(name string, data any)
	sessions    session.Service
	snapshotter aguirunner.MessagesSnapshotter
	snapshotRun runner.Runner
	memory      *memoryRuntime
	historyMu   sync.Mutex
	closeOnce   sync.Once
	closeErr    error
}

// SetNotify 注入事件广播回调(main 装配时设置)。
func (s *AgentService) SetNotify(fn func(name string, data any)) { s.notify = fn }

func (s *AgentService) emit(name string, data any) {
	if s.notify != nil {
		s.notify(name, data)
	}
}

// systemInstruction 是 BlankMind 办公助手的人设与能力说明。
const systemInstruction = `你是 BlankMind,运行在本地的办公 Agent,通过工具管理用户的待办与里程碑,
并基于操作日志生成周报与办公文档。规则:
1. 使用与用户相同的语言回复。
2. 当用户表达待办、任务或里程碑时,调用 create_todo 落库;涉及截止时间应追问具体日期。
3. 用户询问待办/统计/进度时,调用工具读取真实数据,不要编造。
4. 周报必须基于 list_events 与 todo_stats 的真实记录。
5. 回复保持简洁,尽量用 Markdown 结构化。
6. 可加载用户 skills 目录中的技能来完成任务。
7. 用户要求“生成周报/周总结/本周汇报”时调用 generate_weekly_report。
8. 用户要求生成文档(纪要/草稿/方案)或表格(排期/清单/预算)时,
   调用 create_document / create_table 并保存为文件;需要导出待办时用 export_todos。
9. memory_search 用于查询与当前请求有关的长期记忆;仅当用户明确要求记住时调用 memory_add。
   不保存凭据、密钥、密码、隐私秘密、模型推测或工具输出。`

// ChatRequest 一次对话入参。
type ChatRequest struct {
	ConversationID int64  `json:"conversationId"` // 0 = 新建会话
	Message        string `json:"message"`
	Reasoning      string `json:"reasoning"` // 思考档位:"" 关闭 | low | medium | high | max | on
}

// applyReasoning 把思考档位映射到 GenerationConfig。
// 档位取值:"" 关闭 | on | low | medium | high | max。
// 与内置模型目录(catalog.json)联动:关闭可切换模型时显式下发 false;
// 目录标注 none / always 的模型不发参数;effort 模型只在其声明的 levels 内
// 下发 reasoning_effort。
//   - qwen -> enable_thinking(bool);deepseek/hunyuan/glm/minimax -> thinking.type=enabled
//   - deepseek effort 模型另附 reasoning_effort;openai(o 系)只发 reasoning_effort
//   - 自定义兼容网关(custom):reasoning_effort 尽力透传
func applyReasoning(gc *model.GenerationConfig, kind, model, level string) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	model = strings.ToLower(strings.TrimSpace(model))
	lv := strings.ToLower(strings.TrimSpace(level))
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

	if (spec.Type == ReasoningNone || spec.Type == ReasoningAlways) && kind != "custom" {
		// 模型不支持/思考常开,无需下发任何参数(custom 走尽力透传)
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
	case "openai":
		// 目录仅对 o 系列标记 effort;非 o 系不发(避免 400)
		if spec.Type == ReasoningEffort && inLevels {
			gc.ReasoningEffort = &eff
		}
	default:
		// 自定义兼容网关(kind=custom)模型不在目录内:reasoning_effort 尽力透传,
		// 模型不支持时由服务商反馈;其它未登记 kind 不发(目录为准)。
		if kind == "custom" && (eff == "low" || eff == "medium" || eff == "high") {
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
	ConversationID int64  `json:"conversationId"`
	Answer         string `json:"answer"`
}

// Chat 执行一轮 Agent 对话;流式增量通过事件推给前端。
func (s *AgentService) Chat(req ChatRequest) (ChatResult, error) {
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		return ChatResult{}, errors.New("消息不能为空")
	}
	if store == nil {
		return ChatResult{}, errors.New("存储未初始化")
	}
	p, err := settingsSvc.DefaultProvider()
	if err != nil {
		return ChatResult{}, errors.New("尚未配置模型服务商,请先在设置中配置后重试")
	}
	m, err := buildModel(p)
	if err != nil {
		return ChatResult{}, err
	}
	memoryConfig := MemoryConfig{}
	memoryEnabled := false
	if s.memory != nil && settingsSvc != nil {
		if cfg, cfgErr := settingsSvc.memoryConfig(); cfgErr == nil {
			memoryConfig = cfg
			memoryEnabled = cfg.Enabled
		} else {
			log.Println("load memory config:", cfgErr)
		}
	}

	convID := req.ConversationID
	if convID == 0 {
		c, err := s.newConversation(msg)
		if err != nil {
			return ChatResult{}, err
		}
		convID = c.ID
	}
	if err := s.saveMessage(convID, "user", msg); err != nil {
		return ChatResult{}, err
	}

	// 以库中历史(含刚保存的用户消息)seed 本次运行,保持多轮上下文
	hist, err := s.loadMessages(convID)
	if err != nil {
		return ChatResult{}, err
	}
	if s.sessions == nil {
		return ChatResult{}, errors.New("AG-UI 会话存储未初始化")
	}
	// AG-UI 每轮会自行记录最新用户消息;这里只迁移此前的旧格式历史。
	if err := s.ensureAGUIHistory(context.Background(), convID, hist[:len(hist)-1]); err != nil {
		return ChatResult{}, err
	}

	// 思考档位:按服务商机制映射(布尔/对象式 thinking 或 reasoning_effort 级别)
	gc := model.GenerationConfig{Stream: true}
	applyReasoning(&gc, p.Kind, p.Model, req.Reasoning)

	opts := []llmagent.Option{
		llmagent.WithModel(m),
		llmagent.WithInstruction(systemInstruction),
		llmagent.WithTools(append(append(todoAgentTools(todoSvc), officeTools(todoSvc)...),
			reminderAgentTools()...)),
		llmagent.WithGenerationConfig(gc),
		llmagent.WithAddCurrentTime(true),
	}
	if memoryEnabled {
		opts = append(opts,
			llmagent.WithTools(s.memory.Tools()),
			llmagent.WithPreloadMemory(8),
		)
	}
	// skills/<name>/SKILL.md 即插即用
	if repo, err := skill.NewFSRepository(skillsDir()); err == nil {
		opts = append(opts, knowledgeOnlySkillOptions(repo)...)
	} else {
		log.Println("load skills repo:", err)
	}
	agent := llmagent.New("blankmind", opts...)
	runnerOpts := []runner.Option{runner.WithSessionService(s.sessions)}
	if memoryEnabled {
		runnerOpts = append(runnerOpts, runner.WithMemoryService(s.memory))
	}
	baseR := runner.NewRunner("blankmind-app", agent, runnerOpts...)
	defer baseR.Close()

	// AG-UI 协议化:内部 runner 事件经 agui/runner 翻译成标准 AG-UI 事件
	// (RUN/Text/ToolCall/Reasoning…),逐个经 agent.agui 推给前端;
	// 同时保留 agent.chunk 纯文本流供现有 UI 与落库。
	aguiR := aguirunner.New(baseR,
		aguirunner.WithAppName(aguiAppName),
		aguirunner.WithSessionService(s.sessions),
		aguirunner.WithReasoningContentEnabled(true),
		aguirunner.WithToolCallDeltaStreamingEnabled(true),
	)

	ctx := context.Background()
	threadID := "conv-" + strconv.FormatInt(convID, 10)
	aguiMsgs := make([]aguitypes.Message, 0, len(hist))
	for _, h := range hist {
		role := aguitypes.RoleUser
		if h.Role == "assistant" {
			role = aguitypes.RoleAssistant
		}
		aguiMsgs = append(aguiMsgs, aguitypes.Message{
			ID:      "m" + strconv.FormatInt(h.ID, 10),
			Role:    role,
			Content: h.Content,
		})
	}
	input := &adapter.RunAgentInput{
		ThreadID: threadID,
		RunID:    "run-" + strconv.FormatInt(time.Now().UnixNano(), 10),
		Messages: aguiMsgs,
	}
	aguiEvents, err := aguiR.Run(ctx, input)
	if err != nil {
		return ChatResult{}, err
	}

	var answer strings.Builder
	s.emit("agent.start", map[string]any{"conversationId": convID})
	for ev := range aguiEvents {
		if ev == nil {
			continue
		}
		if b, mErr := ev.ToJSON(); mErr == nil {
			var event map[string]any
			if json.Unmarshal(b, &event) == nil {
				s.emit("agent.agui", map[string]any{
					"conversationId": convID,
					"event":          event,
				})
			}
		}
		if te, ok := ev.(*aguievents.TextMessageContentEvent); ok && te.Delta != "" {
			answer.WriteString(te.Delta)
			s.emit("agent.chunk", map[string]any{
				"conversationId": convID,
				"delta":          te.Delta,
			})
		}
	}
	out := strings.TrimSpace(answer.String())
	if out == "" {
		return ChatResult{}, errors.New("模型未返回有效内容")
	}
	if err := s.saveMessage(convID, "assistant", out); err != nil {
		return ChatResult{}, err
	}
	s.emit("agent.done", map[string]any{"conversationId": convID, "answer": out})
	s.emit("conversations.changed", "updated")
	if memoryEnabled && memoryConfig.AutoExtract {
		if err := s.memory.enqueue(m, memoryConfig, msg, out); err != nil {
			log.Println("enqueue memory extraction:", err)
		}
	}
	return ChatResult{ConversationID: convID, Answer: out}, nil
}

func knowledgeOnlySkillOptions(repo skill.Repository) []llmagent.Option {
	return []llmagent.Option{
		llmagent.WithSkills(repo),
		llmagent.WithSkillToolProfile(llmagent.SkillToolProfileKnowledgeOnly),
		llmagent.WithWorkspaceExecSurfaceEnabled(false),
	}
}

// ServiceShutdown closes the snapshot runner and the persistent AG-UI session store.
func (s *AgentService) ServiceShutdown() error {
	s.closeOnce.Do(func() {
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
	dir := filepath.Join(dataDir(), "skills")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Println("create skills dir failed:", err)
	}
	return dir
}

// newConversation 创建会话(首条消息截断为标题)。
func (s *AgentService) newConversation(first string) (Conversation, error) {
	title := strings.TrimSpace(first)
	if r := []rune(title); len(r) > 24 {
		title = string(r[:24]) + "…"
	}
	res, err := store.Exec(
		"INSERT INTO conversations (title, created_at, updated_at) VALUES (?, ?, ?)",
		title, now(), now())
	if err != nil {
		return Conversation{}, err
	}
	id, _ := res.LastInsertId()
	s.emit("conversations.changed", "created")
	return Conversation{ID: id, Title: title, CreatedAt: now(), UpdatedAt: now()}, nil
}

// saveMessage 追加消息并更新时间戳。
func (s *AgentService) saveMessage(convID int64, role, content string) error {
	if _, err := store.Exec(`
		INSERT INTO messages (conversation_id, role, content, created_at) VALUES (?, ?, ?, ?)`,
		convID, role, content, now()); err != nil {
		return err
	}
	_, err := store.Exec("UPDATE conversations SET updated_at = ? WHERE id = ?", now(), convID)
	return err
}

// loadMessages 读取会话消息(正序)。
func (s *AgentService) loadMessages(convID int64) ([]ChatMessage, error) {
	rows, err := store.Query(`
		SELECT id, conversation_id, role, content, created_at FROM messages
		WHERE conversation_id = ? ORDER BY id ASC`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []ChatMessage
	for rows.Next() {
		var m ChatMessage
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &m.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	return items, rows.Err()
}

// ListConversations 返回会话列表(近期在前)。
func (s *AgentService) ListConversations() ([]Conversation, error) {
	if store == nil {
		return []Conversation{}, nil
	}
	rows, err := store.Query(
		"SELECT id, title, created_at, updated_at FROM conversations ORDER BY updated_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Conversation
	for rows.Next() {
		var c Conversation
		if err := rows.Scan(&c.ID, &c.Title, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	return items, rows.Err()
}

// DeleteConversation 删除会话及其消息。
func (s *AgentService) DeleteConversation(conversationID int64) error {
	if _, err := store.Exec("DELETE FROM messages WHERE conversation_id = ?", conversationID); err != nil {
		return err
	}
	if _, err := store.Exec("DELETE FROM conversations WHERE id = ?", conversationID); err != nil {
		return err
	}
	if s.sessions != nil {
		if err := s.sessions.DeleteSession(context.Background(), aguiSessionKey(conversationID)); err != nil {
			return err
		}
	}
	s.emit("conversations.changed", "deleted")
	return nil
}
