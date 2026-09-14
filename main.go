package main

import (
	"embed"
	"log"
	"net/http"
	"strings"

	"github.com/AntNoHuabei/Miel/internal/app"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:frontend/dist
var assets embed.FS

type wailsChatAttachmentPicker struct {
	instance        *application.App
	quickController *quickWindowController
}

type wailsWorkspaceDirectoryPicker struct {
	instance        *application.App
	quickController *quickWindowController
}

type wailsSkillPicker struct {
	instance        *application.App
	quickController *quickWindowController
}

type wailsArtifactPicker struct {
	instance        *application.App
	quickController *quickWindowController
}

func (p wailsArtifactPicker) PickArtifact() (string, error) {
	dialog := p.instance.Dialog.OpenFile().SetTitle("导入产物").CanChooseFiles(true).CanChooseDirectories(false)
	if window := p.quickController.currentWindow(); window != nil {
		dialog.AttachToWindow(window)
	}
	path, err := dialog.PromptForSingleSelection()
	if isDialogCancelledError(err) {
		return "", nil
	}
	return path, err
}

func (p wailsArtifactPicker) PickArtifactExport(name string) (string, error) {
	dialog := p.instance.Dialog.SaveFile().SetMessage("另存产物").SetFilename(name)
	if window := p.quickController.currentWindow(); window != nil {
		dialog.AttachToWindow(window)
	}
	path, err := dialog.PromptForSingleSelection()
	if isDialogCancelledError(err) {
		return "", nil
	}
	return path, err
}

func (p wailsSkillPicker) PickSkillPath() (string, error) {
	dialog := p.instance.Dialog.OpenFile().
		SetTitle("导入 Skill").
		CanChooseFiles(true).
		CanChooseDirectories(true).
		AddFilter("Skill 文件", "*.zip;SKILL.md")
	if window := p.quickController.currentWindow(); window != nil {
		dialog.AttachToWindow(window)
	}
	path, err := dialog.PromptForSingleSelection()
	if isDialogCancelledError(err) {
		return "", nil
	}
	return path, err
}

func (p wailsWorkspaceDirectoryPicker) PickWorkspaceDirectory() (string, error) {
	dialog := p.instance.Dialog.OpenFile().
		SetTitle("选择工作区").
		CanChooseFiles(false).
		CanChooseDirectories(true)
	if window := p.quickController.currentWindow(); window != nil {
		dialog.AttachToWindow(window)
	}
	path, err := dialog.PromptForSingleSelection()
	if isDialogCancelledError(err) {
		return "", nil
	}
	return path, err
}

func (p wailsChatAttachmentPicker) PickChatImages() ([]string, error) {
	dialog := p.instance.Dialog.OpenFile().
		SetTitle("选择图片").
		CanChooseFiles(true).
		AddFilter("图片", "*.png;*.jpg;*.jpeg;*.webp;*.gif")
	if window := p.quickController.currentWindow(); window != nil {
		dialog.AttachToWindow(window)
	}
	paths, err := dialog.PromptForMultipleSelection()
	if isDialogCancelledError(err) {
		return []string{}, nil
	}
	return paths, err
}

func isDialogCancelledError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(text, "cancel")
}

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
	application.RegisterEvent[map[string]any]("agent.input.saved")
	application.RegisterEvent[map[string]any]("agent.agui")
	application.RegisterEvent[app.ApprovalRequest]("permission.requested")
	application.RegisterEvent[map[string]any]("permission.resolved")
	application.RegisterEvent[app.ApprovalRequest]("permission.expired")
	application.RegisterEvent[app.ApprovalRequest]("permission.cancelled")
	application.RegisterEvent[app.PermissionState]("permission.changed")
	application.RegisterEvent[string]("conversations.changed")
	application.RegisterEvent[string]("models.changed")
	application.RegisterEvent[int64]("todo.source.changed")
	application.RegisterEvent[string]("quickchat.show")
	application.RegisterEvent[string]("clipboard.todo.show")
	application.RegisterEvent[string]("memory.changed")
	application.RegisterEvent[string]("artifacts.changed")
	application.RegisterEvent[app.MemoryStatus]("memory.status")
}

// main 只负责:装配业务(app.Bootstrap)→ 接事件总线 → 建窗/托盘/热键 → 运行。
func main() {
	logFile, logErr := app.DefaultDirectoryManager().ConfigureLogging()
	if logErr != nil {
		log.Printf("configure application logging: %v", logErr)
	} else {
		defer logFile.Close()
	}
	activation := &deferredMainActivation{}
	var artifactHandler http.Handler
	instance := application.New(application.Options{
		Name:        "Miel",
		Description: "A local-first AI office agent",
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "com.antnohuabei.blankmind",
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				log.Printf("second instance launch requested from %s", data.WorkingDir)
				activation.request()
			},
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
			Middleware: func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if strings.HasPrefix(r.URL.Path, "/artifacts/") && artifactHandler != nil {
						artifactHandler.ServeHTTP(w, r)
						return
					}
					next.ServeHTTP(w, r)
				})
			},
		},
	})

	svcs, err := app.Bootstrap()
	if err != nil {
		log.Fatal("bootstrap:", err)
	}
	windowTheme := app.NewWindowThemeService()
	for _, service := range []application.Service{
		application.NewService(svcs.Directories),
		application.NewService(svcs.Settings),
		application.NewService(svcs.Permissions),
		application.NewService(svcs.Memory),
		application.NewService(windowTheme),
		application.NewService(svcs.Todo),
		application.NewService(svcs.Agent),
		application.NewService(svcs.Screenshot),
		application.NewService(svcs.Clipboard),
		application.NewService(svcs.ChatAttachments),
		application.NewService(svcs.Skills),
		application.NewService(svcs.Artifacts),
	} {
		instance.RegisterService(service)
	}

	// 事件总线:服务发事件 → wails 应用实例(创建后生效)
	app.Emit = func(name string, data any) {
		instance.Event.Emit(name, data)
	}
	artifactHandler = svcs.Artifacts

	mainWin := instance.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Miel",
		Width:            1000,
		Height:           618,
		Frameless:        true,
		BackgroundColour: application.NewRGB(6, 7, 15),
		URL:              "/",
	})
	quickWin := instance.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "quick-assistant",
		Title:            "Miel",
		Width:            680,
		Height:           560,
		MinWidth:         520,
		MinHeight:        420,
		MaxWidth:         900,
		MaxHeight:        760,
		AlwaysOnTop:      true,
		Frameless:        true,
		Hidden:           true,
		HideOnFocusLost:  false,
		HideOnEscape:     true,
		BackgroundColour: application.NewRGB(246, 247, 249),
		URL:              "/?window=quick",
		Windows: application.WindowsWindow{
			HiddenOnTaskbar: true,
		},
	})
	quickController := newQuickWindowController(instance, mainWin, quickWin)
	app.BindChatAttachmentPicker(svcs.ChatAttachments, wailsChatAttachmentPicker{
		instance:        instance,
		quickController: quickController,
	})
	app.BindSkillPicker(svcs.Skills, wailsSkillPicker{instance: instance, quickController: quickController})
	svcs.Artifacts.SetPicker(wailsArtifactPicker{instance: instance, quickController: quickController})
	app.BindWorkspaceDirectoryPicker(svcs.Settings, wailsWorkspaceDirectoryPicker{
		instance:        instance,
		quickController: quickController,
	})
	activation.bind(quickController.showMain)
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
