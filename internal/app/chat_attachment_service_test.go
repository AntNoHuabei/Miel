package app

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	aguitypes "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"trpc.group/trpc-go/trpc-agent-go/session/inmemory"
)

func newAttachmentTestService(t *testing.T) *ChatAttachmentService {
	t.Helper()
	return &ChatAttachmentService{
		db:      newTodoTestDB(t),
		rootDir: t.TempDir(),
		drafts:  make(map[string]*chatAttachmentPayload),
	}
}

func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.NRGBA{R: 220, G: 20, B: 60, A: 255})
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestValidateChatImageFormats(t *testing.T) {
	pngData := testPNG(t, 2, 3)
	jpegImage := image.NewRGBA(image.Rect(0, 0, 2, 2))
	var jpegOut bytes.Buffer
	if err := jpeg.Encode(&jpegOut, jpegImage, nil); err != nil {
		t.Fatal(err)
	}
	gifImage := image.NewPaletted(image.Rect(0, 0, 2, 2), color.Palette{color.Black, color.White})
	var gifOut bytes.Buffer
	if err := gif.Encode(&gifOut, gifImage, nil); err != nil {
		t.Fatal(err)
	}
	webpData, err := base64.StdEncoding.DecodeString("UklGRrIBAABXRUJQVlA4TKUBAAAvSsAYAA8w//M///MfeJAkbXvaSG7m8Q3GfYSBJekwQztm/IcZlgwnmWImn2BK7aFmBtnVir6q//8VOkFE/xm4baTIu8c48ArEo6+B3zFKYln3pqClSCKX0begFTAXFOLXHSyF8cCNcZEG4OywuA4KVVfJCiArU7GAgJI8+lJP/OKMT/fBAjevg1cYB7YVkFuWga2lyPi5I0HFy5YTpWIHg0RZpkniRVW9odHAKOwosWuOGdxIyn2OvaCDvhg/we6TwadPBPbqBV58MsLmMJ8yZnOWk8SRz4N+QoyPL+MnamzMvcE1rHNEr91F9GKZPVUcS9w7PhhH36suB9qPeYb/oLk6cuTiJ0wOK3m5h1cKjW6EVZCYMK7dxcKCBdgP9HkKr9gkAO2P8GKZGWVdIAatQa+1IDpt6qyorVwdy01xdW8Jkfk6xjEXmVQQ+HQdFr6OKhIN34dXWq0+0qr6EJSCeeVLH9+gvGTLyqM65PQ44ihzlTXxQKjKbAvshXgir7Lil9w4L2bvMycmjQcqXaMCO6BlY28i+FOLzbfI1vEqxAhotocAAA==")
	if err != nil {
		t.Fatal(err)
	}

	for name, test := range map[string]struct {
		data []byte
		mime string
	}{
		"png":  {pngData, "image/png"},
		"jpeg": {jpegOut.Bytes(), "image/jpeg"},
		"gif":  {gifOut.Bytes(), "image/gif"},
		"webp": {webpData, "image/webp"},
	} {
		t.Run(name, func(t *testing.T) {
			metadata, _, err := validateChatImage(test.data)
			if err != nil {
				t.Fatal(err)
			}
			if metadata.mimeType != test.mime {
				t.Fatalf("mime = %q, want %q", metadata.mimeType, test.mime)
			}
		})
	}
}

func TestValidateChatImageRejectsAnimatedGIFAndInvalidContent(t *testing.T) {
	frame := image.NewPaletted(image.Rect(0, 0, 1, 1), color.Palette{color.Black})
	var animated bytes.Buffer
	if err := gif.EncodeAll(&animated, &gif.GIF{
		Image: []*image.Paletted{frame, frame}, Delay: []int{0, 1},
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := validateChatImage(animated.Bytes()); err == nil || !strings.Contains(err.Error(), "动画 GIF") {
		t.Fatalf("animated GIF error = %v", err)
	}
	if _, _, err := validateChatImage([]byte("not an image")); err == nil {
		t.Fatal("invalid image was accepted")
	}
}

func TestValidateChatImageRejectsSizeAndPixelLimits(t *testing.T) {
	tooLarge := make([]byte, maxChatAttachmentBytes+1)
	if _, _, err := validateChatImage(tooLarge); err == nil || !strings.Contains(err.Error(), "20 MB") {
		t.Fatalf("size limit error = %v", err)
	}

	oversizedHeader := pngHeader(10_000, 5_001)
	if _, _, err := validateChatImage(oversizedHeader); err == nil || !strings.Contains(err.Error(), "5000 万像素") {
		t.Fatalf("pixel limit error = %v", err)
	}
}

func TestAttachmentDraftPersistsWithMessageAndLoadsPreview(t *testing.T) {
	service := newAttachmentTestService(t)
	draft, err := service.stageBytes(testPNG(t, 12, 8), "renamed.txt")
	if err != nil {
		t.Fatal(err)
	}
	if draft.MIMEType != "image/png" {
		t.Fatalf("mime = %q, want image/png", draft.MIMEType)
	}
	if !strings.HasPrefix(draft.ThumbnailDataURI, "data:image/png;base64,") {
		t.Fatalf("thumbnail = %q", draft.ThumbnailDataURI)
	}
	if _, err := service.db.Exec("INSERT INTO conversations (id, title, created_at, updated_at) VALUES (1, 'image', 1, 1)"); err != nil {
		t.Fatal(err)
	}
	result, err := service.db.Exec("INSERT INTO messages (conversation_id, role, content, created_at) VALUES (1, 'user', '', 1)")
	if err != nil {
		t.Fatal(err)
	}
	messageID, _ := result.LastInsertId()
	tx, err := service.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	finalize, err := service.attachDrafts(tx, messageID, []string{draft.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		finalize(false)
		t.Fatal(err)
	}
	finalize(true)

	var filePath, thumbnailPath string
	if err := service.db.QueryRow(
		"SELECT file_path, thumbnail_path FROM message_attachments WHERE id = ?", draft.ID,
	).Scan(&filePath, &thumbnailPath); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filePath, thumbnailPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("persisted file %q: %v", path, err)
		}
	}
	dataURI, err := service.GetImageDataURI(draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(dataURI, "data:image/png;base64,") {
		t.Fatalf("data URI = %q", dataURI)
	}
}

func TestSavePureImageMessageUsesImageTitleAndPreservesOrder(t *testing.T) {
	attachmentService := newAttachmentTestService(t)
	oldStore := store
	store = attachmentService.db
	t.Cleanup(func() { store = oldStore })
	agent := &AgentService{attachments: attachmentService}
	first, err := attachmentService.stageBytes(testPNG(t, 3, 4), "first.png")
	if err != nil {
		t.Fatal(err)
	}
	second, err := attachmentService.stageBytes(testPNG(t, 5, 6), "second.png")
	if err != nil {
		t.Fatal(err)
	}
	conversationID, _, err := agent.saveUserMessage(0, "", []string{first.ID, second.ID})
	if err != nil {
		t.Fatal(err)
	}
	var title string
	if err := store.QueryRow("SELECT title FROM conversations WHERE id = ?", conversationID).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != "图片对话" {
		t.Fatalf("title = %q, want 图片对话", title)
	}
	messages, err := agent.loadMessages(conversationID)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || len(messages[0].Attachments) != 2 {
		t.Fatalf("messages = %#v", messages)
	}
	if messages[0].Attachments[0].OriginalName != "first.png" || messages[0].Attachments[1].OriginalName != "second.png" {
		t.Fatalf("attachment order = %#v", messages[0].Attachments)
	}
	multimodal, err := aguiMessageFromChatMessage(messages[0])
	if err != nil {
		t.Fatal(err)
	}
	contents, ok := multimodal.Content.([]aguitypes.InputContent)
	if !ok || len(contents) != 2 || contents[0].MimeType != "image/png" || contents[1].MimeType != "image/png" {
		t.Fatalf("multimodal content = %#v", multimodal.Content)
	}
}

func TestAGUIMessagePlacesTextBeforeImages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(path, testPNG(t, 2, 2), 0o600); err != nil {
		t.Fatal(err)
	}
	message, err := aguiMessageFromChatMessage(ChatMessage{
		ID: 1, Role: "user", Content: "describe",
		Attachments: []MessageAttachment{{FilePath: path, MIMEType: "image/png", OriginalName: "image.png"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	contents := message.Content.([]aguitypes.InputContent)
	if len(contents) != 2 || contents[0].Type != aguitypes.InputContentTypeText || contents[0].Text != "describe" {
		t.Fatalf("content order = %#v", contents)
	}
	if contents[1].Type != aguitypes.InputContentTypeBinary || contents[1].Data == "" {
		t.Fatalf("image content = %#v", contents[1])
	}
}

func TestAttachmentCountLimit(t *testing.T) {
	service := newAttachmentTestService(t)
	ids := make([]string, 0, maxChatAttachmentCount+1)
	for range maxChatAttachmentCount + 1 {
		draft, err := service.stageBytes(testPNG(t, 1, 1), "image.png")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, draft.ID)
	}
	if _, err := service.validateDrafts(ids); err == nil || !strings.Contains(err.Error(), "最多") {
		t.Fatalf("count limit error = %v", err)
	}
}

func TestAttachmentTotalSizeLimit(t *testing.T) {
	service := newAttachmentTestService(t)
	service.drafts = map[string]*chatAttachmentPayload{
		"one":   {ChatAttachmentDraft: ChatAttachmentDraft{ID: "one", SizeBytes: 18 << 20}},
		"two":   {ChatAttachmentDraft: ChatAttachmentDraft{ID: "two", SizeBytes: 18 << 20}},
		"three": {ChatAttachmentDraft: ChatAttachmentDraft{ID: "three", SizeBytes: 18 << 20}},
	}
	if _, err := service.validateDrafts([]string{"one", "two", "three"}); err == nil || !strings.Contains(err.Error(), "50 MB") {
		t.Fatalf("total size limit error = %v", err)
	}
}

func TestDeleteConversationRemovesOwnedAttachments(t *testing.T) {
	attachmentService := newAttachmentTestService(t)
	oldStore := store
	store = attachmentService.db
	t.Cleanup(func() { store = oldStore })

	draft, err := attachmentService.stageBytes(testPNG(t, 4, 4), "source.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Exec("INSERT INTO conversations (id, title, created_at, updated_at) VALUES (8, 'image', 1, 1)"); err != nil {
		t.Fatal(err)
	}
	result, err := store.Exec("INSERT INTO messages (conversation_id, role, content, created_at) VALUES (8, 'user', 'look', 1)")
	if err != nil {
		t.Fatal(err)
	}
	messageID, _ := result.LastInsertId()
	tx, _ := store.Begin()
	finalize, err := attachmentService.attachDrafts(tx, messageID, []string{draft.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		finalize(false)
		t.Fatal(err)
	}
	finalize(true)
	var filePath, thumbnailPath string
	if err := store.QueryRow("SELECT file_path, thumbnail_path FROM message_attachments WHERE id = ?", draft.ID).Scan(&filePath, &thumbnailPath); err != nil {
		t.Fatal(err)
	}

	sessions := inmemory.NewSessionService()
	t.Cleanup(func() { _ = sessions.Close() })
	agent, err := newAgentService(sessions)
	if err != nil {
		t.Fatal(err)
	}
	agent.attachments = attachmentService
	if err := agent.DeleteConversation(8); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filePath, thumbnailPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("attachment file still exists %q: %v", path, err)
		}
	}
	if countFiles(t, attachmentService.fileDir()) != 0 || countFiles(t, attachmentService.thumbnailDir()) != 0 {
		t.Fatal("owned attachment files were not removed")
	}
}

func TestRedactAGUIBinaryContent(t *testing.T) {
	payload := map[string]any{
		"content": []any{
			map[string]any{"type": "text", "text": "hello"},
			map[string]any{"type": "binary", "mimeType": "image/png", "data": "secret"},
			map[string]any{"type": "binary", "mimeType": "application/pdf", "data": "keep"},
		},
	}
	redactAGUIBinaryContent(payload)
	contents := payload["content"].([]any)
	if _, exists := contents[1].(map[string]any)["data"]; exists {
		t.Fatal("image data was not redacted")
	}
	if contents[2].(map[string]any)["data"] != "keep" {
		t.Fatal("non-image data was redacted")
	}
}

func countFiles(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) != "" {
			count++
		}
	}
	return count
}

func pngHeader(width, height uint32) []byte {
	result := append([]byte(nil), []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}...)
	ihdr := make([]byte, 17)
	copy(ihdr[:4], "IHDR")
	binary.BigEndian.PutUint32(ihdr[4:8], width)
	binary.BigEndian.PutUint32(ihdr[8:12], height)
	ihdr[12] = 8
	ihdr[13] = 6
	chunk := make([]byte, 4+len(ihdr)+4)
	binary.BigEndian.PutUint32(chunk[:4], uint32(len(ihdr)-4))
	copy(chunk[4:], ihdr)
	binary.BigEndian.PutUint32(chunk[4+len(ihdr):], crc32.ChecksumIEEE(ihdr))
	return append(result, chunk...)
}
