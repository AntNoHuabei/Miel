//go:build windows

package app

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image/png"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/w32"
	"golang.org/x/image/bmp"
	"golang.org/x/sys/windows"
)

const (
	clipboardFormatUnicodeText = 13
	clipboardFormatDIB         = 8
	clipboardFormatDIBV5       = 17
	maxClipboardTextBytes      = 4 << 20
	maxClipboardImageBytes     = 20 << 20
	clipboardReadAttempts      = 41
	clipboardReadRetryDelay    = 25 * time.Millisecond
)

var (
	clipboardUser32              = windows.NewLazySystemDLL("user32.dll")
	clipboardKernel32            = windows.NewLazySystemDLL("kernel32.dll")
	clipboardOpenProc            = clipboardUser32.NewProc("OpenClipboard")
	clipboardCloseProc           = clipboardUser32.NewProc("CloseClipboard")
	clipboardFormatAvailableProc = clipboardUser32.NewProc("IsClipboardFormatAvailable")
	clipboardGetDataProc         = clipboardUser32.NewProc("GetClipboardData")
	clipboardGlobalSizeProc      = clipboardKernel32.NewProc("GlobalSize")
	errClipboardOpenUnavailable  = errors.New("无法打开剪贴板,请稍后重试")
	errClipboardDataUnavailable  = errors.New("无法读取剪贴板内容")
)

type clipboardPayload struct {
	kind     string
	text     string
	imagePNG []byte
}

func readWindowsClipboard() (clipboardPayload, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	return readClipboardWithRetry(readWindowsClipboardAttempt, clipboardReadAttempts, clipboardReadRetryDelay)
}

func readClipboardWithRetry(
	attempt func() (clipboardPayload, error), maxAttempts int, retryDelay time.Duration,
) (clipboardPayload, error) {
	for current := 1; current <= maxAttempts; current++ {
		payload, err := attempt()
		if err == nil {
			return payload, nil
		}
		if (!errors.Is(err, errClipboardOpenUnavailable) && !errors.Is(err, errClipboardDataUnavailable)) || current == maxAttempts {
			return clipboardPayload{}, err
		}
		if retryDelay > 0 {
			time.Sleep(retryDelay)
		}
	}
	return clipboardPayload{}, errClipboardDataUnavailable
}

func readWindowsClipboardAttempt() (clipboardPayload, error) {
	opened, _, _ := clipboardOpenProc.Call(0)
	if opened == 0 {
		return clipboardPayload{}, errClipboardOpenUnavailable
	}
	defer clipboardCloseProc.Call() //nolint:errcheck

	for _, format := range []uint32{clipboardFormatDIBV5, clipboardFormatDIB} {
		available, _, _ := clipboardFormatAvailableProc.Call(uintptr(format))
		if available == 0 {
			continue
		}
		raw, err := clipboardData(format, maxClipboardImageBytes)
		if err != nil {
			return clipboardPayload{}, err
		}
		encoded, err := dibToPNG(raw)
		if err != nil {
			return clipboardPayload{}, fmt.Errorf("读取剪贴板图片失败: %w", err)
		}
		if len(encoded) > maxClipboardImageBytes {
			return clipboardPayload{}, errors.New("剪贴板图片超过 20 MB")
		}
		return clipboardPayload{kind: "clipboard_image", imagePNG: encoded}, nil
	}

	available, _, _ := clipboardFormatAvailableProc.Call(clipboardFormatUnicodeText)
	if available == 0 {
		return clipboardPayload{}, errors.New("剪贴板中没有可用的文本或图片")
	}
	raw, err := clipboardData(clipboardFormatUnicodeText, maxClipboardTextBytes)
	if err != nil {
		return clipboardPayload{}, err
	}
	if len(raw)%2 != 0 {
		return clipboardPayload{}, errors.New("剪贴板文本格式无效")
	}
	utf16 := make([]uint16, len(raw)/2)
	for i := range utf16 {
		utf16[i] = binary.LittleEndian.Uint16(raw[i*2:])
	}
	text := syscall.UTF16ToString(utf16)
	if text == "" {
		return clipboardPayload{}, errors.New("剪贴板文本为空")
	}
	return clipboardPayload{kind: "clipboard_text", text: text}, nil
}

func clipboardData(format uint32, maxBytes int) ([]byte, error) {
	handle, _, _ := clipboardGetDataProc.Call(uintptr(format))
	if handle == 0 {
		return nil, errClipboardDataUnavailable
	}
	size, _, _ := clipboardGlobalSizeProc.Call(handle)
	if size == 0 {
		return nil, errors.New("剪贴板内容为空")
	}
	if size > uintptr(maxBytes) {
		return nil, fmt.Errorf("剪贴板内容超过 %d MB", maxBytes>>20)
	}
	ptr, err := lockClipboardMemory(handle)
	if err != nil {
		return nil, err
	}
	if ptr == nil {
		return nil, errors.New("无法锁定剪贴板内容")
	}
	defer w32.GlobalUnlock(w32.HGLOBAL(handle))
	return append([]byte(nil), unsafe.Slice((*byte)(ptr), int(size))...), nil
}

func lockClipboardMemory(handle uintptr) (pointer unsafe.Pointer, err error) {
	defer func() {
		if recover() != nil {
			pointer = nil
			err = errors.New("无法锁定剪贴板内容")
		}
	}()
	return w32.GlobalLock(w32.HGLOBAL(handle)), nil
}

func dibToPNG(dib []byte) ([]byte, error) {
	if len(dib) < 40 {
		return nil, errors.New("DIB 数据不完整")
	}
	headerSize := int(binary.LittleEndian.Uint32(dib[0:4]))
	if headerSize < 40 || headerSize > len(dib) {
		return nil, errors.New("DIB 头无效")
	}
	bitCount := binary.LittleEndian.Uint16(dib[14:16])
	compression := binary.LittleEndian.Uint32(dib[16:20])
	colorsUsed := binary.LittleEndian.Uint32(dib[32:36])
	paletteEntries := int(colorsUsed)
	if paletteEntries == 0 && bitCount <= 8 {
		paletteEntries = 1 << bitCount
	}
	extraMasks := 0
	if headerSize == 40 {
		switch compression {
		case 3:
			extraMasks = 12
		case 6:
			extraMasks = 16
		}
	}
	pixelOffset := 14 + headerSize + extraMasks + paletteEntries*4
	if pixelOffset-14 > len(dib) {
		return nil, errors.New("DIB 像素偏移无效")
	}
	bmpData := make([]byte, 14+len(dib))
	copy(bmpData[0:2], "BM")
	binary.LittleEndian.PutUint32(bmpData[2:6], uint32(len(bmpData)))
	binary.LittleEndian.PutUint32(bmpData[10:14], uint32(pixelOffset))
	copy(bmpData[14:], dib)
	config, err := bmp.DecodeConfig(bytes.NewReader(bmpData))
	if err != nil {
		return nil, err
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 50_000_000 {
		return nil, errors.New("剪贴板图片尺寸无效或过大")
	}
	img, err := bmp.Decode(bytes.NewReader(bmpData))
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
