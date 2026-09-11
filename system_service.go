package main

import (
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/AntNoHuabei/blankmind/internal/app"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/w32"
)

// setupSystemTray 创建系统托盘:常驻入口(显示/隐藏主窗口、退出)。
func setupSystemTray(inst *application.App, quick *quickWindowController) {
	tray := inst.SystemTray.New()
	tray.SetTooltip("BlankMind - 本地办公 Agent")
	if icon, err := os.ReadFile(filepath.Join("build", "appicon.png")); err == nil {
		tray.SetIcon(icon)
	} else {
		log.Println("tray icon not loaded:", err)
	}
	menu := application.NewMenu()
	menu.Add("打开 BlankMind").OnClick(func(*application.Context) { quick.showMain() })
	menu.Add("浮动助手").OnClick(func(*application.Context) { quick.show("chat") })
	menu.Add("从粘贴板生成待办").OnClick(func(*application.Context) { quick.show("clipboard") })
	menu.Add("隐藏窗口").OnClick(func(*application.Context) { quick.hideAll() })
	menu.AddSeparator()
	menu.Add("退出").OnClick(func(*application.Context) { inst.Quit() })
	tray.SetMenu(menu)
	tray.Show()
}

func setupQuickHotkeys(inst *application.App, quick *quickWindowController) {
	shortcuts := []struct {
		key  string
		mode string
	}{
		{key: "alt+s", mode: "chat"},
		{key: "alt+t", mode: "clipboard"},
	}
	for _, shortcut := range shortcuts {
		shortcut := shortcut
		if err := inst.GlobalShortcut.Register(shortcut.key, func() {
			quick.show(shortcut.mode)
		}); err != nil {
			log.Printf("register global hotkey %s failed: %v", shortcut.key, err)
		}
	}
}

type quickWindowController struct {
	inst  *application.App
	main  application.Window
	quick application.Window

	mu    sync.Mutex
	mode  string
	ready bool
}

func newQuickWindowController(inst *application.App, main, quick application.Window) *quickWindowController {
	return &quickWindowController{inst: inst, main: main, quick: quick}
}

func (c *quickWindowController) show(mode string) {
	if mode != "clipboard" {
		mode = "chat"
	}
	c.mu.Lock()
	c.mode = mode
	ready := c.ready
	c.mu.Unlock()

	c.main.Hide()
	if x, y, ok := w32.GetCursorPos(); ok {
		if screen := c.inst.Screen.ScreenNearestPhysicalPoint(application.Point{X: x, Y: y}); screen != nil {
			c.quick.SetScreen(screen)
		}
	}
	c.quick.Center()
	c.quick.Show()
	c.quick.Focus()
	if ready {
		c.emit(mode)
	}
}

func (c *quickWindowController) showMain() {
	c.quick.Hide()
	c.main.Show()
	c.main.Focus()
}

func (c *quickWindowController) hideAll() {
	c.quick.Hide()
	c.main.Hide()
}

func (c *quickWindowController) runtimeReady() {
	c.mu.Lock()
	c.ready = true
	mode := c.mode
	c.mu.Unlock()
	if mode != "" {
		c.emit(mode)
	}
}

func (c *quickWindowController) emit(mode string) {
	if mode == "clipboard" {
		c.inst.Event.Emit("clipboard.todo.show", "clipboard")
		return
	}
	c.inst.Event.Emit("quickchat.show", "chat")
}

// setupGlobalHotkey 注册“全局截图”快捷键:后台/托盘态触发截屏并广播
// screenshot.captured(前端弹处理菜单)。快捷键由 resolveHotkey 提供
// (默认 Ctrl+Alt+S,可在设置中覆盖)。
func setupGlobalHotkey(inst *application.App, quick *quickWindowController,
	svc *app.ScreenshotService, resolveHotkey func() string) {
	hotkey := resolveHotkey()
	err := inst.GlobalShortcut.Register(hotkey, func() {
		quick.showMain()
		res, cerr := svc.Capture()
		if cerr != nil {
			log.Println("capture failed:", cerr)
			return
		}
		svc.EmitEvent("screenshot.captured", map[string]any{
			"id":        res.ID,
			"path":      res.Path,
			"dataUri":   res.DataURI,
			"width":     res.Width,
			"height":    res.Height,
			"createdAt": res.CreatedAt,
		})
	})
	if err != nil {
		log.Println("register global hotkey failed:", err)
	}
}
