//go:build windows

// Package capture 提供 Windows 主屏捕获能力。
package capture

import (
	"fmt"
	"image"
	"syscall"
	"unsafe"
)

var (
	user32 = syscall.NewLazyDLL("user32.dll")
	gdi32  = syscall.NewLazyDLL("gdi32.dll")

	procGetDC              = user32.NewProc("GetDC")
	procReleaseDC          = user32.NewProc("ReleaseDC")
	procGetSystemMetrics   = user32.NewProc("GetSystemMetrics")
	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procCreateBitmap       = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject       = gdi32.NewProc("SelectObject")
	procBitBlt             = gdi32.NewProc("BitBlt")
	procGetDIBits          = gdi32.NewProc("GetDIBits")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procDeleteDC           = gdi32.NewProc("DeleteDC")
)

const (
	srcCopy      = 0x00CC0020
	biRGB        = 0
	dibRGBColors = 0
)

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [1]uint32
}

// Screen 捕获主屏为 image.Image(如需多显示器/选区可在此扩展)。
func Screen() (image.Image, error) {
	screenDC, _, _ := procGetDC.Call(0)
	if screenDC == 0 {
		return nil, fmt.Errorf("GetDC 失败")
	}
	defer procReleaseDC.Call(0, screenDC)

	sx, _, _ := procGetSystemMetrics.Call(0) // SM_CXSCREEN
	sy, _, _ := procGetSystemMetrics.Call(1) // SM_CYSCREEN
	w := int(uint32(sx))
	h := int(uint32(sy))
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("无效的屏幕尺寸 %dx%d", w, h)
	}

	memDC, _, _ := procCreateCompatibleDC.Call(screenDC)
	if memDC == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC 失败")
	}
	defer procDeleteDC.Call(memDC)

	bmp, _, _ := procCreateBitmap.Call(screenDC, uintptr(w), uintptr(h))
	if bmp == 0 {
		return nil, fmt.Errorf("CreateCompatibleBitmap 失败")
	}
	defer procDeleteObject.Call(bmp)

	oldObj, _, _ := procSelectObject.Call(memDC, bmp)
	defer procSelectObject.Call(memDC, oldObj)

	if r, _, _ := procBitBlt.Call(memDC, 0, 0, uintptr(w), uintptr(h),
		screenDC, 0, 0, srcCopy); r == 0 {
		return nil, fmt.Errorf("BitBlt 失败")
	}

	stride := w * 4
	buf := make([]byte, stride*h)
	bmi := bitmapInfo{
		Header: bitmapInfoHeader{
			Size:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
			Width:       int32(w),
			Height:      -int32(h), // 负值 = top-down,避免行反转
			Planes:      1,
			BitCount:    32,
			Compression: biRGB,
		},
	}
	if r, _, _ := procGetDIBits.Call(memDC, bmp, 0, uintptr(h),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bmi)), dibRGBColors); r == 0 {
		return nil, fmt.Errorf("GetDIBits 失败")
	}

	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		row := buf[y*stride : (y+1)*stride]
		for x := 0; x < w; x++ {
			i := x * 4
			off := img.PixOffset(x, y)
			img.Pix[off+0] = row[i+2] // BGRA → NRGBA
			img.Pix[off+1] = row[i+1]
			img.Pix[off+2] = row[i+0]
			img.Pix[off+3] = 255
		}
	}
	return img, nil
}
