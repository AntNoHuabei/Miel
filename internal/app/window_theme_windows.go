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
	"light":    {background: rgbHex("#ffffff"), text: rgbHex("#1f2329"), border: rgbHex("#dfe3e8")},
	"dark":     {dark: true, background: rgbHex("#1a1f2a"), text: rgbHex("#e8edf5"), border: rgbHex("#343d4d")},
	"forest":   {background: rgbHex("#ffffff"), text: rgbHex("#203126"), border: rgbHex("#cfdccc")},
	"nebula":   {dark: true, background: rgbHex("#1b1730"), text: rgbHex("#f0ebff"), border: rgbHex("#3b3357")},
	"graphite": {background: rgbHex("#ffffff"), text: rgbHex("#242424"), border: rgbHex("#d5d5d2")},
	"celadon":  {background: rgbHex("#fbfdfc"), text: rgbHex("#1c302d"), border: rgbHex("#bdd4cf")},
	"cinnabar": {background: rgbHex("#fffdfc"), text: rgbHex("#332725"), border: rgbHex("#dfcfcb")},
	"amber":    {dark: true, background: rgbHex("#1d1b14"), text: rgbHex("#f4f0e5"), border: rgbHex("#45402f")},
	"abyss":    {dark: true, background: rgbHex("#11201e"), text: rgbHex("#e4f2f0"), border: rgbHex("#294541")},
	"contrast": {dark: true, background: rgbHex("#000000"), text: rgbHex("#ffffff"), border: rgbHex("#595959")},
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
