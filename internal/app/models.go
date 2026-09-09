package app

// Todo 待办项(含里程碑标记)。
type Todo struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Deadline    int64  `json:"deadline"` // unix 秒;0 表示无截止时间
	IsMilestone bool   `json:"isMilestone"`
	Status      string `json:"status"` // pending | doing | done
	Source      string `json:"source"` // manual | chat | screenshot
	CreatedAt   int64  `json:"createdAt"`
	DoneAt      int64  `json:"doneAt"`
}

// TodoInput 创建 / 更新待办的入参。
type TodoInput struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Deadline    int64  `json:"deadline"`
	IsMilestone bool   `json:"isMilestone"`
	Status      string `json:"status"`
	Source      string `json:"source"`
}

// TodoStats 提供给前端快捷视图的汇总(角标/里程碑倒计时)。
type TodoStats struct {
	Total      int64 `json:"total"`
	Pending    int64 `json:"pending"`
	Done       int64 `json:"done"`
	Milestones int64 `json:"milestones"`
	Overdue    int64 `json:"overdue"` // 逾期未完成
	DueSoon    int64 `json:"dueSoon"` // 24 小时内到期未完成
}

// Provider 模型服务商配置。
// Kind 对应 trpc-agent-go 的 provider 体系:
//
//	openai / anthropic / ollama / deepseek / qwen / hunyuan / custom
//
// 其中 custom 指任意 OpenAI 兼容自定义服务商(baseUrl + apiKey + model)。
type Provider struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	BaseURL    string `json:"baseUrl"`
	APIKey     string `json:"apiKey"`
	Model      string `json:"model"`
	Multimodal bool   `json:"multimodal"` // 是否支持图片输入(截图转待办/问答需要)
	IsDefault  bool   `json:"isDefault"`
	CreatedAt  int64  `json:"createdAt"`
}

// ProviderInput 保存 Provider 的入参(前端设置页表单)。
type ProviderInput struct {
	ID         int64                `json:"id"`
	Name       string               `json:"name"`
	Kind       string               `json:"kind"`
	BaseURL    string               `json:"baseUrl"`
	APIKey     string               `json:"apiKey"`
	Model      string               `json:"model"` // 当前使用模型
	Multimodal bool                 `json:"multimodal"`
	IsDefault  bool                 `json:"isDefault"`
	Models     []ProviderModelInput `json:"models"` // 启用的模型集合(留空=仅 Model)
}

// ProviderModel 服务商启用的单个模型(内置或自定义)。
type ProviderModel struct {
	Model  string `json:"model"`
	Label  string `json:"label"`
	Custom bool   `json:"custom"`
}

// ProviderModelInput 前端提交的启用模型条目。
type ProviderModelInput struct {
	Model  string `json:"model"`
	Label  string `json:"label"`
	Custom bool   `json:"custom"`
}

// ModelOption 模型切换下拉的扁平选项(provider × 启用模型)。
type ModelOption struct {
	ProviderID   int64  `json:"providerId"`
	ProviderName string `json:"providerName"`
	Kind         string `json:"kind"`
	Model        string `json:"model"`
	Label        string `json:"label"`
	Custom       bool   `json:"custom"`
	IsDefault    bool   `json:"isDefault"` // 是否为当前使用模型
}

// ProviderTemplate 内置厂商模板(设置向导里供选择)。
type ProviderTemplate struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	BaseURL    string `json:"baseUrl"`
	Model      string `json:"model"`
	Multimodal bool   `json:"multimodal"`
	DocsURL    string `json:"docsUrl"`
}

// Event 操作日志条目,是周报自动汇总的事实来源。
type Event struct {
	ID      int64  `json:"id"`
	TS      int64  `json:"ts"`
	Type    string `json:"type"` // todo.created / todo.done / screenshot.added / report.generated ...
	Summary string `json:"summary"`
	RefID   int64  `json:"refId"`
}

// Setting 应用设置项(key-value)。
type Setting struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Conversation 会话元信息。
type Conversation struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

// ChatMessage 会话消息。
type ChatMessage struct {
	ID             int64  `json:"id"`
	ConversationID int64  `json:"conversationId"`
	Role           string `json:"role"` // user | assistant | tool
	Content        string `json:"content"`
	CreatedAt      int64  `json:"createdAt"`
}

// Screenshot 截图记录。
type Screenshot struct {
	ID        int64  `json:"id"`
	Path      string `json:"path"`
	Note      string `json:"note"`
	CreatedAt int64  `json:"createdAt"`
}

// 待办状态常量。
const (
	TodoStatusPending = "pending"
	TodoStatusDoing   = "doing"
	TodoStatusDone    = "done"
)

// 事件类型常量。
const (
	EventTodoCreated     = "todo.created"
	EventTodoDone        = "todo.done"
	EventTodoDeleted     = "todo.deleted"
	EventScreenshotAdded = "screenshot.added"
	EventReportGenerated = "report.generated"
)
