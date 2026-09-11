package app

import (
	"errors"
	"strings"
	"sync"
)

// WindowThemeService 把前端皮肤选择同步给宿主窗口。
// 具体的窗口 API 由 Windows 实现绑定到原生 HWND。
type WindowThemeService struct {
	mu      sync.RWMutex
	handles map[uintptr]struct{}
	pending string
}

// NewWindowThemeService 构造窗口主题服务。
func NewWindowThemeService() *WindowThemeService {
	return &WindowThemeService{handles: make(map[uintptr]struct{})}
}

// BindWindowThemeService 绑定平台窗口句柄。窗口创建后由 main 装配一次。
func BindWindowThemeService(s *WindowThemeService, handle uintptr) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if handle != 0 {
		s.handles[handle] = struct{}{}
	}
	pending := s.pending
	s.mu.Unlock()
	if handle != 0 && pending != "" {
		_ = applyNativeWindowTheme(handle, pending)
	}
}

// SetTheme 同步前端皮肤 ID 到原生窗口。
func (s *WindowThemeService) SetTheme(themeID string) error {
	themeID = strings.TrimSpace(themeID)
	if themeID == "" {
		return errors.New("theme id 不能为空")
	}

	s.mu.Lock()
	s.pending = themeID
	handles := make([]uintptr, 0, len(s.handles))
	for handle := range s.handles {
		handles = append(handles, handle)
	}
	s.mu.Unlock()
	if len(handles) == 0 {
		// Wails creates the native HWND just before WindowRuntimeReady. Cache
		// the requested theme so startup calls do not fail during that gap.
		return nil
	}
	var firstErr error
	for _, handle := range handles {
		if err := applyNativeWindowTheme(handle, themeID); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
