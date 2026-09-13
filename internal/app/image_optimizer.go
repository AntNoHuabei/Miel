package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	modelImagePolicyVersion = "v1"
	modelImageMaxDimension  = 2560
	modelImageMaxBytes      = 5 << 20
)

var modelImageCacheMu sync.Mutex

type optimizedImage struct {
	Data     []byte
	MIMEType string
	Format   string
	Filename string
	Width    int
	Height   int
}

func optimizeImageFile(path, name string) (optimizedImage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return optimizedImage{}, fmt.Errorf("读取图片失败: %w", err)
	}
	return optimizeImageData(data, name)
}

func optimizeImageData(data []byte, name string) (optimizedImage, error) {
	meta, decoded, err := decodeImageForModel(data)
	if err != nil {
		return optimizedImage{}, err
	}
	if len(data) <= modelImageMaxBytes && max(meta.width, meta.height) <= modelImageMaxDimension {
		return optimizedImage{
			Data: data, MIMEType: meta.mimeType, Format: imageFormat(meta.mimeType),
			Filename: nameWithExtension(name, meta.extension), Width: meta.width, Height: meta.height,
		}, nil
	}

	hash := sha256.Sum256(data)
	if cached, ok := loadOptimizedImageCache(hash, name); ok {
		return cached, nil
	}

	preferPNG := meta.mimeType == "image/png" || imageHasTransparency(decoded)
	optimized, err := encodeModelImage(decoded, meta, name, preferPNG)
	if err != nil {
		return optimizedImage{}, err
	}
	saveOptimizedImageCache(hash, optimized)
	return optimized, nil
}

func decodeImageForModel(data []byte) (chatImageMetadata, image.Image, error) {
	if len(data) == 0 {
		return chatImageMetadata{}, nil, errors.New("图片内容为空")
	}
	mimeType, extension := detectChatImageType(data)
	if mimeType == "" {
		return chatImageMetadata{}, nil, errors.New("仅支持 PNG、JPEG、WebP 和非动画 GIF")
	}
	if mimeType == "image/gif" {
		decodedGIF, err := gif.DecodeAll(bytes.NewReader(data))
		if err != nil {
			return chatImageMetadata{}, nil, fmt.Errorf("GIF 图片无效: %w", err)
		}
		if len(decodedGIF.Image) != 1 {
			return chatImageMetadata{}, nil, errors.New("暂不支持动画 GIF")
		}
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return chatImageMetadata{}, nil, fmt.Errorf("图片格式无效: %w", err)
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > maxChatAttachmentPixels {
		return chatImageMetadata{}, nil, errors.New("图片尺寸无效或超过 5000 万像素")
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return chatImageMetadata{}, nil, fmt.Errorf("图片解码失败: %w", err)
	}
	return chatImageMetadata{mimeType: mimeType, extension: extension, width: config.Width, height: config.Height}, decoded, nil
}

func encodeModelImage(src image.Image, meta chatImageMetadata, name string, preferPNG bool) (optimizedImage, error) {
	targetWidth, targetHeight := modelImageDimensions(meta.width, meta.height)
	for attempt := 0; attempt < 18; attempt++ {
		resized := resizeModelImage(src, targetWidth, targetHeight)
		if preferPNG {
			if data, err := encodeModelPNG(resized); err == nil && len(data) <= modelImageMaxBytes {
				return newOptimizedImage(data, "image/png", ".png", name, targetWidth, targetHeight), nil
			}
		} else {
			for _, quality := range []int{85, 75, 65, 55} {
				if data, err := encodeJPEG(resized, quality); err == nil && len(data) <= modelImageMaxBytes {
					return newOptimizedImage(data, "image/jpeg", ".jpg", name, targetWidth, targetHeight), nil
				}
			}
		}
		if targetWidth == 1 && targetHeight == 1 {
			break
		}
		targetWidth = max(1, int(float64(targetWidth)*0.85))
		targetHeight = max(1, int(float64(targetHeight)*0.85))
	}

	// A highly detailed transparent PNG may remain large even after scaling. Flatten it
	// against white only as the final fallback so the model still receives an image.
	for _, quality := range []int{85, 75, 65, 55} {
		resized := resizeModelImage(src, targetWidth, targetHeight)
		if data, err := encodeJPEG(resized, quality); err == nil && len(data) <= modelImageMaxBytes {
			return newOptimizedImage(data, "image/jpeg", ".jpg", name, targetWidth, targetHeight), nil
		}
	}
	return optimizedImage{}, errors.New("图片压缩后仍超过模型大小限制")
}

func modelImageDimensions(width, height int) (int, int) {
	scale := float64(modelImageMaxDimension) / float64(max(width, height))
	if scale > 1 {
		scale = 1
	}
	return max(1, int(float64(width)*scale)), max(1, int(float64(height)*scale))
}

func resizeModelImage(src image.Image, width, height int) image.Image {
	if src.Bounds().Dx() == width && src.Bounds().Dy() == height {
		return src
	}
	target := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(target, target.Bounds(), src, src.Bounds(), draw.Over, nil)
	return target
}

func encodeModelPNG(src image.Image) ([]byte, error) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, src); err != nil {
		return nil, err
	}
	return encoded.Bytes(), nil
}

func encodeJPEG(src image.Image, quality int) ([]byte, error) {
	canvas := image.NewRGBA(image.Rect(0, 0, src.Bounds().Dx(), src.Bounds().Dy()))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(canvas, canvas.Bounds(), src, src.Bounds().Min, draw.Over)
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, canvas, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return encoded.Bytes(), nil
}

func imageHasTransparency(src image.Image) bool {
	for y := src.Bounds().Min.Y; y < src.Bounds().Max.Y; y++ {
		for x := src.Bounds().Min.X; x < src.Bounds().Max.X; x++ {
			if color.NRGBAModel.Convert(src.At(x, y)).(color.NRGBA).A < 255 {
				return true
			}
		}
	}
	return false
}

func newOptimizedImage(data []byte, mimeType, extension, name string, width, height int) optimizedImage {
	return optimizedImage{
		Data: data, MIMEType: mimeType, Format: imageFormat(mimeType),
		Filename: nameWithExtension(name, extension), Width: width, Height: height,
	}
}

func imageFormat(mimeType string) string {
	return strings.TrimPrefix(mimeType, "image/")
}

func nameWithExtension(name, extension string) string {
	base := strings.TrimSpace(filepath.Base(name))
	if base == "" || base == "." {
		base = "image"
	}
	base = strings.TrimSuffix(base, filepath.Ext(base))
	return base + extension
}

func modelImageCacheDir() string {
	return filepath.Join(appDirectories.Path(DirectoryAttachments), "model-cache")
}

func modelImageCachePrefix(hash [sha256.Size]byte) string {
	return modelImagePolicyVersion + "-" + hex.EncodeToString(hash[:])
}

func loadOptimizedImageCache(hash [sha256.Size]byte, name string) (optimizedImage, bool) {
	modelImageCacheMu.Lock()
	defer modelImageCacheMu.Unlock()
	prefix := modelImageCachePrefix(hash)
	for _, extension := range []string{".png", ".jpg"} {
		path := filepath.Join(modelImageCacheDir(), prefix+extension)
		data, err := os.ReadFile(path)
		if err != nil || len(data) == 0 || len(data) > modelImageMaxBytes {
			continue
		}
		meta, _, err := decodeImageForModel(data)
		if err != nil || max(meta.width, meta.height) > modelImageMaxDimension {
			continue
		}
		return optimizedImage{
			Data: data, MIMEType: meta.mimeType, Format: imageFormat(meta.mimeType),
			Filename: nameWithExtension(name, meta.extension), Width: meta.width, Height: meta.height,
		}, true
	}
	return optimizedImage{}, false
}

func saveOptimizedImageCache(hash [sha256.Size]byte, optimized optimizedImage) {
	if len(optimized.Data) == 0 {
		return
	}
	modelImageCacheMu.Lock()
	defer modelImageCacheMu.Unlock()
	if err := os.MkdirAll(modelImageCacheDir(), 0o700); err != nil {
		return
	}
	path := filepath.Join(modelImageCacheDir(), modelImageCachePrefix(hash)+optimizedExtension(optimized.MIMEType))
	tmp, err := os.CreateTemp(modelImageCacheDir(), ".tmp-image-*")
	if err != nil {
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(optimized.Data); err != nil {
		_ = tmp.Close()
		return
	}
	if err := tmp.Close(); err != nil {
		return
	}
	_ = os.Rename(tmpPath, path)
}

func optimizedExtension(mimeType string) string {
	if mimeType == "image/png" {
		return ".png"
	}
	return ".jpg"
}

func removeOptimizedImageCache(data []byte) {
	hash := sha256.Sum256(data)
	modelImageCacheMu.Lock()
	defer modelImageCacheMu.Unlock()
	prefix := modelImageCachePrefix(hash)
	for _, extension := range []string{".png", ".jpg"} {
		_ = os.Remove(filepath.Join(modelImageCacheDir(), prefix+extension))
	}
}

func cleanupOptimizedImageCache() {
	modelImageCacheMu.Lock()
	defer modelImageCacheMu.Unlock()
	entries, err := os.ReadDir(modelImageCacheDir())
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), modelImagePolicyVersion+"-") {
			continue
		}
		_ = os.Remove(filepath.Join(modelImageCacheDir(), entry.Name()))
	}
}
