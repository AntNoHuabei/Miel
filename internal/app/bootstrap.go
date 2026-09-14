package app

import "context"

// Emit 是统一事件出口。main 会把它接到 wails 事件总线(broadcast);
// 业务服务(提醒、待办、Agent、截图)均经此向前端推送事件。
var Emit = func(_ string, _ any) {}

// Services 是装配产物,供 main 注册为 wails 服务。
type Services struct {
	Directories     *DirectoryService
	Settings        *SettingsService
	Permissions     *PermissionService
	Memory          *MemoryService
	Todo            *TodoService
	Agent           *AgentService
	Screenshot      *ScreenshotService
	Clipboard       *ClipboardService
	ChatAttachments *ChatAttachmentService
	Reminder        *ReminderService
	Skills          *SkillService
	Artifacts       *ArtifactService
}

// Bootstrap 初始化存储与内置 skills,构造并装配各业务服务
// (服务间引用与事件出口都在这里收敛,main 只做注册)。
func Bootstrap() (*Services, error) {
	if err := appDirectories.EnsureAll(); err != nil {
		return nil, err
	}
	if err := openStore(); err != nil {
		return nil, err
	}
	ensureBuiltinSkills(skillsDir())

	settings := NewSettingsService(store)
	todo := NewTodoService(store)
	settingsSvc = settings
	todoSvc = todo
	permissions := NewPermissionService(settings)

	memoryRuntime, err := newMemoryRuntime()
	if err != nil {
		return nil, err
	}
	memoryService := NewMemoryService(memoryRuntime, settings)
	chatAttachments := NewChatAttachmentService(store)
	artifacts, err := NewArtifactService(store, appDirectories)
	if err != nil {
		_ = memoryRuntime.Close()
		return nil, err
	}
	agent, err := NewAgentService(memoryRuntime, chatAttachments, permissions)
	if err != nil {
		_ = memoryRuntime.Close()
		return nil, err
	}
	agent.artifacts = artifacts
	artifacts.agent = agent
	if err := artifacts.ImportLegacy(); err != nil {
		_ = agent.ServiceShutdown()
		_ = memoryRuntime.Close()
		return nil, err
	}
	shot := NewScreenshotService(store, settings, memoryRuntime)
	clipboard := NewClipboardService(todo, settings, memoryRuntime)
	rem := NewReminderService(store)
	skills := NewSkillService(appDirectories)

	// 服务 → 事件总线:间接引用 var Emit,main 注入后同样生效
	notify := func(name string, data any) { Emit(name, data) }
	settings.setNotify(notify)
	todo.Notify = notify
	agent.SetNotify(notify)
	permissions.SetNotify(notify)
	memoryRuntime.setNotify(notify)
	shot.SetNotify(notify)
	artifacts.notify = notify

	rem.Start(context.Background())
	return &Services{
		Directories:     NewDirectoryService(appDirectories),
		Settings:        settings,
		Permissions:     permissions,
		Memory:          memoryService,
		Todo:            todo,
		Agent:           agent,
		Screenshot:      shot,
		Clipboard:       clipboard,
		ChatAttachments: chatAttachments,
		Reminder:        rem,
		Skills:          skills,
		Artifacts:       artifacts,
	}, nil
}
