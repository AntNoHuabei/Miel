package main

import (
	"embed"
	"log"

	"github.com/AntNoHuabei/blankmind/internal/app"
	"github.com/wailsapp/wails/v3/pkg/application"
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
}

// main 只负责:装配业务(app.Bootstrap)→ 接事件总线 → 建窗/托盘/热键 → 运行。
func main() {
	svcs, err := app.Bootstrap()
	if err != nil {
		log.Fatal("bootstrap:", err)
	}

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
			application.NewService(svcs.Todo),
			application.NewService(svcs.Agent),
			application.NewService(svcs.Screenshot),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})
	wailsApp = instance

	mainWin := instance.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "BlankMind",
		Width:  1000,
		Height: 618,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(6, 7, 15),
		URL:              "/",
	})

	setupSystemTray(instance, mainWin)
	setupGlobalHotkey(instance, mainWin, svcs.Screenshot, func() string {
		if h, err := svcs.Settings.GetSetting(app.SettingCaptureHotkey); err == nil && h != "" {
			return h
		}
		return "ctrl+alt+s"
	})

	if err := instance.Run(); err != nil {
		log.Fatal(err)
	}
}
