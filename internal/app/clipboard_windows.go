//go:build windows

package app

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math/bits"
	"runtime"
	"sync"
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
	clipboardCompressionRGB    = 0
	clipboardCompressionFields = 3
	clipboardCompressionAlpha  = 6
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
	clipboardRegisterFormatProc  = clipboardUser32.NewProc("RegisterClipboardFormatW")
	clipboardGlobalSizeProc      = clipboardKernel32.NewProc("GlobalSize")
	errClipboardOpenUnavailable  = errors.New("无法打开剪贴板,请稍后重试")
	errClipboardDataUnavailable  = errors.New("无法读取剪贴板内容")
	clipboardPNGFormatOnce       sync.Once
	clipboardPNGFormat           uint32
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

	if format := registeredPNGClipboardFormat(); format != 0 {
		available, _, _ := clipboardFormatAvailableProc.Call(uintptr(format))
		if available != 0 {
			raw, err := clipboardData(format, maxClipboardImageBytes)
			if err != nil {
				return clipboardPayload{}, err
			}
			if err := validatePNG(raw); err != nil {
				return clipboardPayload{}, fmt.Errorf("读取剪贴板 PNG 失败: %w", err)
			}
			return clipboardPayload{kind: "clipboard_image", imagePNG: raw}, nil
		}
	}

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

func registeredPNGClipboardFormat() uint32 {
	clipboardPNGFormatOnce.Do(func() {
		name, err := windows.UTF16PtrFromString("PNG")
		if err != nil {
			return
		}
		format, _, _ := clipboardRegisterFormatProc.Call(uintptr(unsafe.Pointer(name)))
		clipboardPNGFormat = uint32(format)
	})
	return clipboardPNGFormat
}

func validatePNG(data []byte) error {
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return err
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 50_000_000 {
		return errors.New("剪贴板图片尺寸无效或过大")
	}
	return nil
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
	if bitCount == 16 || compression == clipboardCompressionFields || compression == clipboardCompressionAlpha {
		img, err := decodeBitfieldDIB(dib)
		if err != nil {
			return nil, err
		}
		return encodePNG(img)
	}
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
	return encodePNG(img)
}

func encodePNG(img image.Image) ([]byte, error) {
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func decodeBitfieldDIB(dib []byte) (image.Image, error) {
	if len(dib) < 40 {
		return nil, errors.New("DIB 数据不完整")
	}
	headerSize := int(binary.LittleEndian.Uint32(dib[0:4]))
	if headerSize < 40 || headerSize > len(dib) {
		return nil, errors.New("DIB 头无效")
	}
	width := int64(int32(binary.LittleEndian.Uint32(dib[4:8])))
	signedHeight := int64(int32(binary.LittleEndian.Uint32(dib[8:12])))
	planes := binary.LittleEndian.Uint16(dib[12:14])
	bitCount := binary.LittleEndian.Uint16(dib[14:16])
	compression := binary.LittleEndian.Uint32(dib[16:20])
	if width <= 0 || signedHeight == 0 || planes != 1 || (bitCount != 16 && bitCount != 32) {
		return nil, errors.New("DIB 位图参数不受支持")
	}
	topDown := signedHeight < 0
	height := signedHeight
	if height < 0 {
		height = -height
	}
	if width*height > 50_000_000 {
		return nil, errors.New("剪贴板图片尺寸无效或过大")
	}

	redMask, greenMask, blueMask, alphaMask, externalMaskBytes, err := dibChannelMasks(dib, headerSize, bitCount, compression)
	if err != nil {
		return nil, err
	}
	colorsUsed := int64(binary.LittleEndian.Uint32(dib[32:36]))
	pixelOffset := int64(headerSize+externalMaskBytes) + colorsUsed*4
	rowStride := ((width*int64(bitCount) + 31) / 32) * 4
	pixelBytes := rowStride * height
	if pixelOffset < 0 || pixelBytes < 0 || pixelOffset+pixelBytes > int64(len(dib)) {
		return nil, errors.New("DIB 像素数据不完整")
	}

	img := image.NewNRGBA(image.Rect(0, 0, int(width), int(height)))
	bytesPerPixel := int(bitCount / 8)
	anyAlpha := false
	for sourceY := int64(0); sourceY < height; sourceY++ {
		targetY := sourceY
		if !topDown {
			targetY = height - 1 - sourceY
		}
		rowStart := pixelOffset + sourceY*rowStride
		for x := int64(0); x < width; x++ {
			offset := rowStart + x*int64(bytesPerPixel)
			var value uint32
			if bitCount == 16 {
				value = uint32(binary.LittleEndian.Uint16(dib[offset : offset+2]))
			} else {
				value = binary.LittleEndian.Uint32(dib[offset : offset+4])
			}
			alpha := uint8(0xff)
			if alphaMask != 0 {
				alpha = scaleDIBChannel(value, alphaMask)
				anyAlpha = anyAlpha || alpha != 0
			}
			pixel := img.Pix[int(targetY)*img.Stride+int(x)*4:]
			pixel[0] = scaleDIBChannel(value, redMask)
			pixel[1] = scaleDIBChannel(value, greenMask)
			pixel[2] = scaleDIBChannel(value, blueMask)
			pixel[3] = alpha
		}
	}
	// Some Windows producers declare an alpha mask but leave every alpha bit at
	// zero. Treat that inconsistent representation as opaque instead of blank.
	if alphaMask != 0 && !anyAlpha {
		for offset := 3; offset < len(img.Pix); offset += 4 {
			img.Pix[offset] = 0xff
		}
	}
	return img, nil
}

func dibChannelMasks(dib []byte, headerSize int, bitCount uint16, compression uint32) (
	red, green, blue, alpha uint32, externalBytes int, err error,
) {
	switch compression {
	case clipboardCompressionRGB:
		if bitCount == 16 {
			return 0x7c00, 0x03e0, 0x001f, 0, 0, nil
		}
		return 0x00ff0000, 0x0000ff00, 0x000000ff, 0, 0, nil
	case clipboardCompressionFields, clipboardCompressionAlpha:
		maskOffset := 40
		maskCount := 3
		if compression == clipboardCompressionAlpha {
			maskCount = 4
		}
		if headerSize == 40 {
			externalBytes = maskCount * 4
		} else if headerSize < 52 || (maskCount == 4 && headerSize < 56) {
			return 0, 0, 0, 0, 0, errors.New("DIB 位域掩码不完整")
		}
		if len(dib) < maskOffset+maskCount*4 {
			return 0, 0, 0, 0, 0, errors.New("DIB 位域掩码不完整")
		}
		red = binary.LittleEndian.Uint32(dib[maskOffset : maskOffset+4])
		green = binary.LittleEndian.Uint32(dib[maskOffset+4 : maskOffset+8])
		blue = binary.LittleEndian.Uint32(dib[maskOffset+8 : maskOffset+12])
		if maskCount == 4 {
			alpha = binary.LittleEndian.Uint32(dib[maskOffset+12 : maskOffset+16])
		} else if headerSize >= 56 {
			alpha = binary.LittleEndian.Uint32(dib[52:56])
		}
		if !validDIBMasks(red, green, blue, alpha) {
			return 0, 0, 0, 0, 0, errors.New("DIB 位域掩码无效")
		}
		return red, green, blue, alpha, externalBytes, nil
	default:
		return 0, 0, 0, 0, 0, fmt.Errorf("不支持的 DIB 压缩格式: %d", compression)
	}
}

func validDIBMasks(red, green, blue, alpha uint32) bool {
	if red == 0 || green == 0 || blue == 0 || red&green != 0 || red&blue != 0 || green&blue != 0 {
		return false
	}
	if alpha != 0 && alpha&(red|green|blue) != 0 {
		return false
	}
	for _, mask := range []uint32{red, green, blue, alpha} {
		if mask == 0 {
			continue
		}
		normalized := mask >> bits.TrailingZeros32(mask)
		if normalized&(normalized+1) != 0 {
			return false
		}
	}
	return true
}

func scaleDIBChannel(value, mask uint32) uint8 {
	if mask == 0 {
		return 0
	}
	shift := bits.TrailingZeros32(mask)
	maximum := mask >> shift
	component := (value & mask) >> shift
	return uint8((uint64(component)*255 + uint64(maximum)/2) / uint64(maximum))
}
