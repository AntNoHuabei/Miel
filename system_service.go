package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/AntNoHuabei/blankmind/internal/app"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// setupSystemTray 创建系统托盘:常驻入口(显示/隐藏主窗口、退出)。
func setupSystemTray(inst *application.App, win application.Window) {
	tray := inst.SystemTray.New()
	tray.SetTooltip("BlankMind - 本地办公 Agent")
	if icon, err := os.ReadFile(filepath.Join("build", "appicon.png")); err == nil {
		tray.SetIcon(icon)
	} else {
		log.Println("tray icon not loaded:", err)
	}
	menu := application.NewMenu()
	menu.Add("打开 BlankMind").OnClick(func(*application.Context) { win.Show() })
	menu.Add("隐藏窗口").OnClick(func(*application.Context) { win.Hide() })
	menu.AddSeparator()
	menu.Add("退出").OnClick(func(*application.Context) { inst.Quit() })
	tray.SetMenu(menu)
	tray.Show()
}

// setupGlobalHotkey 注册“全局截图”快捷键:后台/托盘态触发截屏并广播
// screenshot.captured(前端弹处理菜单)。快捷键由 resolveHotkey 提供
// (默认 Ctrl+Alt+S,可在设置中覆盖)。
func setupGlobalHotkey(inst *application.App, win application.Window,
	svc *app.ScreenshotService, resolveHotkey func() string) {
	hotkey := resolveHotkey()
	err := inst.GlobalShortcut.Register(hotkey, func() {
		// 回调在独立 goroutine;窗口操作需切回主线程。
		application.InvokeSync(func() { win.Show() })
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
