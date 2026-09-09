package app

import "context"

// Emit 是统一事件出口。main 会把它接到 wails 事件总线(broadcast);
// 业务服务(提醒、待办、Agent、截图)均经此向前端推送事件。
var Emit = func(_ string, _ any) {}

// Services 是装配产物,供 main 注册为 wails 服务。
type Services struct {
	Settings   *SettingsService
	Todo       *TodoService
	Agent      *AgentService
	Screenshot *ScreenshotService
	Reminder   *ReminderService
}

// Bootstrap 初始化存储与内置 skills,构造并装配各业务服务
// (服务间引用与事件出口都在这里收敛,main 只做注册)。
func Bootstrap() (*Services, error) {
	if err := openStore(); err != nil {
		return nil, err
	}
	ensureBuiltinSkills(skillsDir())

	settings := NewSettingsService(store)
	todo := NewTodoService(store)
	settingsSvc = settings
	todoSvc = todo

	agent, err := NewAgentService()
	if err != nil {
		return nil, err
	}
	shot := NewScreenshotService(store)
	rem := NewReminderService(store)

	// 服务 → 事件总线:间接引用 var Emit,main 注入后同样生效
	notify := func(name string, data any) { Emit(name, data) }
	todo.Notify = notify
	agent.SetNotify(notify)
	shot.SetNotify(notify)

	rem.Start(context.Background())
	return &Services{
		Settings:   settings,
		Todo:       todo,
		Agent:      agent,
		Screenshot: shot,
		Reminder:   rem,
	}, nil
}
