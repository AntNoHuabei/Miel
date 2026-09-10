//go:build windows

package app

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/w32"
)

type nativeThemeColour struct {
	dark       bool
	background uint32
	text       uint32
	border     uint32
}

var nativeThemeColours = map[string]nativeThemeColour{
	"light":  {background: rgbHex("#ffffff"), text: rgbHex("#1f2329"), border: rgbHex("#f0f0f0")},
	"dark":   {dark: true, background: rgbHex("#1a1f2a"), text: rgbHex("#e8edf5"), border: rgbHex("#2a3140")},
	"forest": {background: rgbHex("#ffffff"), text: rgbHex("#203126"), border: rgbHex("#e2ecdf")},
	"nebula": {dark: true, background: rgbHex("#1b1730"), text: rgbHex("#f0ebff"), border: rgbHex("#2c2547")},
}

// applyNativeWindowTheme 使用 DWM 更新 Windows 原生标题栏。
func applyNativeWindowTheme(handle uintptr, themeID string) error {
	theme, ok := nativeThemeColours[strings.ToLower(strings.TrimSpace(themeID))]
	if !ok {
		return fmt.Errorf("unsupported window theme: %s", themeID)
	}
	w32.SetTheme(handle, theme.dark)
	w32.SetTitleBarColour(handle, theme.background)
	w32.SetTitleTextColour(handle, theme.text)
	w32.SetBorderColour(handle, theme.border)
	return nil
}

func rgbHex(value string) uint32 {
	hex := strings.TrimPrefix(strings.TrimSpace(value), "#")
	if len(hex) != 6 {
		return 0
	}
	r, _ := strconv.ParseUint(hex[0:2], 16, 8)
	g, _ := strconv.ParseUint(hex[2:4], 16, 8)
	b, _ := strconv.ParseUint(hex[4:6], 16, 8)
	return w32.RGB(byte(r), byte(g), byte(b))
}
