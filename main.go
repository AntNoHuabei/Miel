package main

import (
	"embed"
	"log"

	"github.com/AntNoHuabei/blankmind/internal/app"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:frontend/dist
var assets embed.FS

func init() {
	application.RegisterEvent[string]("time")
	application.RegisterEvent[string]("todos.changed")
	application.RegisterEvent[string]("reminders.changed")
	application.RegisterEvent[map[string]any]("screenshot.captured")
	application.RegisterEvent[map[string]any]("screenshot.saved")
	application.RegisterEvent[map[string]any]("screenshot.processed")
	application.RegisterEvent[map[string]any]("agent.chunk")
	application.RegisterEvent[map[string]any]("agent.done")
	application.RegisterEvent[map[string]any]("agent.start")
	application.RegisterEvent[map[string]any]("agent.agui")
	application.RegisterEvent[string]("conversations.changed")
	application.RegisterEvent[string]("models.changed")
	application.RegisterEvent[int64]("todo.source.changed")
	application.RegisterEvent[string]("quickchat.show")
	application.RegisterEvent[string]("clipboard.todo.show")
	application.RegisterEvent[string]("memory.changed")
	application.RegisterEvent[app.MemoryStatus]("memory.status")
}

// main 只负责:装配业务(app.Bootstrap)→ 接事件总线 → 建窗/托盘/热键 → 运行。
func main() {
	svcs, err := app.Bootstrap()
	if err != nil {
		log.Fatal("bootstrap:", err)
	}
	windowTheme := app.NewWindowThemeService()

	// 事件总线:服务发事件 → wails 应用实例(创建后生效)
	var wailsApp *application.App
	app.Emit = func(name string, data any) {
		if wailsApp != nil {
			wailsApp.Event.Emit(name, data)
		}
	}

	instance := application.New(application.Options{
		Name:        "BlankMind",
		Description: "A local-first AI office agent",
		Services: []application.Service{
			application.NewService(svcs.Settings),
			application.NewService(svcs.Memory),
			application.NewService(windowTheme),
			application.NewService(svcs.Todo),
			application.NewService(svcs.Agent),
			application.NewService(svcs.Screenshot),
			application.NewService(svcs.Clipboard),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
	})
	wailsApp = instance

	mainWin := instance.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "BlankMind",
		Width:            1000,
		Height:           618,
		Frameless:        true,
		BackgroundColour: application.NewRGB(6, 7, 15),
		URL:              "/",
	})
	quickWin := instance.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "quick-assistant",
		Title:            "BlankMind",
		Width:            680,
		Height:           560,
		MinWidth:         520,
		MinHeight:        420,
		MaxWidth:         900,
		MaxHeight:        760,
		AlwaysOnTop:      true,
		Frameless:        true,
		Hidden:           true,
		HideOnFocusLost:  true,
		HideOnEscape:     true,
		BackgroundColour: application.NewRGB(246, 247, 249),
		URL:              "/?window=quick",
		Windows: application.WindowsWindow{
			HiddenOnTaskbar: true,
		},
	})
	quickController := newQuickWindowController(instance, mainWin, quickWin)
	app.BindWindowThemeService(windowTheme, uintptr(mainWin.NativeWindow()))
	mainWin.OnWindowEvent(events.Common.WindowRuntimeReady, func(_ *application.WindowEvent) {
		app.BindWindowThemeService(windowTheme, uintptr(mainWin.NativeWindow()))
	})
	app.BindWindowThemeService(windowTheme, uintptr(quickWin.NativeWindow()))
	quickWin.OnWindowEvent(events.Common.WindowRuntimeReady, func(_ *application.WindowEvent) {
		app.BindWindowThemeService(windowTheme, uintptr(quickWin.NativeWindow()))
		quickController.runtimeReady()
	})

	setupSystemTray(instance, quickController)
	setupQuickHotkeys(instance, quickController)
	setupGlobalHotkey(instance, quickController, svcs.Screenshot, func() string {
		if h, err := svcs.Settings.GetSetting(app.SettingCaptureHotkey); err == nil && h != "" {
			return h
		}
		return "ctrl+alt+s"
	})

	if err := instance.Run(); err != nil {
		log.Fatal(err)
	}
}
