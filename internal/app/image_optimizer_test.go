package app

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	aguitypes "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
)

func TestOptimizeImageDataKeepsSmallOriginal(t *testing.T) {
	data := testPNG(t, 12, 8)
	optimized, err := optimizeImageData(data, "source.png")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(optimized.Data, data) {
		t.Fatal("small image was unexpectedly re-encoded")
	}
	if optimized.MIMEType != "image/png" || optimized.Format != "png" || optimized.Filename != "source.png" {
		t.Fatalf("metadata = %#v", optimized)
	}
}

func TestOptimizeImageDataScalesLongEdgeAndKeepsPreviewSourceIndependent(t *testing.T) {
	data := testPNG(t, 3000, 100)
	original := append([]byte(nil), data...)
	optimized, err := optimizeImageData(data, "wide.png")
	if err != nil {
		t.Fatal(err)
	}
	if optimized.Width != modelImageMaxDimension || optimized.Height != 85 {
		t.Fatalf("dimensions = %dx%d, want %dx%d", optimized.Width, optimized.Height, modelImageMaxDimension, 85)
	}
	if optimized.MIMEType != "image/png" || optimized.Filename != "wide.png" {
		t.Fatalf("metadata = %#v", optimized)
	}
	if !bytes.Equal(data, original) {
		t.Fatal("optimizer modified the original input")
	}
	decoded, _, err := image.Decode(bytes.NewReader(optimized.Data))
	if err != nil || decoded.Bounds().Dx() != optimized.Width || decoded.Bounds().Dy() != optimized.Height {
		t.Fatalf("optimized image decode = %v, bounds = %v", err, decoded.Bounds())
	}
}

func TestOptimizeImageDataPreservesTransparencyWhenPNGFits(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 3000, 4))
	img.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 80})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	optimized, err := optimizeImageData(encoded.Bytes(), "transparent.png")
	if err != nil {
		t.Fatal(err)
	}
	if optimized.MIMEType != "image/png" {
		t.Fatalf("mime = %q, want image/png", optimized.MIMEType)
	}
	decoded, _, err := image.Decode(bytes.NewReader(optimized.Data))
	if err != nil {
		t.Fatal(err)
	}
	if alpha := color.NRGBAModel.Convert(decoded.At(0, 0)).(color.NRGBA).A; alpha == 0 || alpha == 255 {
		t.Fatalf("alpha = %d, want preserved transparency", alpha)
	}
}

func TestOptimizeImageDataUsesContentCache(t *testing.T) {
	oldDirectories := appDirectories
	appDirectories = NewDirectoryManager(t.TempDir())
	t.Cleanup(func() { appDirectories = oldDirectories })

	data := testPNG(t, 3000, 100)
	first, err := optimizeImageData(data, "first.png")
	if err != nil {
		t.Fatal(err)
	}
	second, err := optimizeImageData(data, "renamed.png")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Data, second.Data) {
		t.Fatal("cache result differs from the first optimization")
	}
	if second.Filename != "renamed.png" {
		t.Fatalf("cached filename = %q", second.Filename)
	}
	entries, err := os.ReadDir(filepath.Join(appDirectories.Path(DirectoryAttachments), "model-cache"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("cache entries = %d, err = %v", len(entries), err)
	}
}

func TestAGUIMessageUsesOptimizedImageMetadata(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "wide.png")
	if err := os.WriteFile(path, testPNG(t, 3000, 100), 0o600); err != nil {
		t.Fatal(err)
	}
	message, err := aguiMessageFromChatMessage(ChatMessage{
		ID: 1, Role: "user", Content: "describe",
		Attachments: []MessageAttachment{{FilePath: path, MIMEType: "image/png", OriginalName: "wide.png"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	contents := message.Content.([]aguitypes.InputContent)
	imageContent := contents[1]
	if imageContent.MimeType != "image/png" || imageContent.Filename != "wide.png" {
		t.Fatalf("image metadata = %#v", imageContent)
	}
	decoded, err := base64.StdEncoding.DecodeString(imageContent.Data)
	if err != nil {
		t.Fatal(err)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(decoded))
	if err != nil || config.Width != modelImageMaxDimension {
		t.Fatalf("optimized image config = %#v, err = %v", config, err)
	}
}
