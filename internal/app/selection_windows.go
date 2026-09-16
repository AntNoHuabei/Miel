//go:build windows

package app

import (
	"errors"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

const (
	selectionVKControl = 0x11
	selectionVKMenu    = 0x12
	selectionVKShift   = 0x10
	selectionVKC       = 0x43
	selectionKeyUp     = 0x0002
)

var (
	selectionUser32             = windows.NewLazySystemDLL("user32.dll")
	selectionKeybdEventProc     = selectionUser32.NewProc("keybd_event")
	clipboardSequenceNumberProc = selectionUser32.NewProc("GetClipboardSequenceNumber")
)

// ReadSelectedText copies the foreground application's current text selection.
// A clipboard sequence change is required so an empty selection cannot reuse
// stale clipboard text.
func ReadSelectedText() (string, error) {
	before, _, _ := clipboardSequenceNumberProc.Call()
	keyEvent(selectionVKMenu, selectionKeyUp)
	keyEvent(selectionVKShift, selectionKeyUp)
	keyEvent(selectionVKControl, selectionKeyUp)
	time.Sleep(30 * time.Millisecond)
	keyEvent(selectionVKControl, 0)
	keyEvent(selectionVKC, 0)
	keyEvent(selectionVKC, selectionKeyUp)
	keyEvent(selectionVKControl, selectionKeyUp)

	deadline := time.Now().Add(900 * time.Millisecond)
	for time.Now().Before(deadline) {
		after, _, _ := clipboardSequenceNumberProc.Call()
		if after != before {
			payload, err := readWindowsClipboard()
			if err != nil {
				return "", err
			}
			if payload.kind != "clipboard_text" || strings.TrimSpace(payload.text) == "" {
				return "", errors.New("所选内容不是文本")
			}
			return payload.text, nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return "", errors.New("没有检测到选中的文本")
}

func keyEvent(key, flags uintptr) {
	selectionKeybdEventProc.Call(key, 0, flags, 0) //nolint:errcheck
}
