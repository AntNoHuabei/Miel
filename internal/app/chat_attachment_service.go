package app

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/gif"
	_ "image/jpeg"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	maxChatAttachmentCount  = 8
	maxChatAttachmentBytes  = 20 << 20
	maxChatAttachmentTotal  = 50 << 20
	maxChatAttachmentPixels = 50_000_000
	chatThumbnailMaxSize    = 160
)

// ChatAttachmentDraft is returned to a composer after an image has been staged.
type ChatAttachmentDraft struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	MIMEType         string `json:"mimeType"`
	SizeBytes        int64  `json:"size"`
	Width            int    `json:"width"`
	Height           int    `json:"height"`
	ThumbnailDataURI string `json:"thumbnailDataUri"`
}

type chatAttachmentPayload struct {
	ChatAttachmentDraft
	draftPath          string
	draftThumbnailPath string
	extension          string
	claimed            bool
}

type chatAttachmentPicker interface {
	PickChatImages() ([]string, error)
}

// ChatAttachmentService owns chat image drafts and persisted attachment files.
type ChatAttachmentService struct {
	db      *sql.DB
	rootDir string
	picker  chatAttachmentPicker
	mu      sync.Mutex
	drafts  map[string]*chatAttachmentPayload
}

func NewChatAttachmentService(db *sql.DB) *ChatAttachmentService {
	service := &ChatAttachmentService{
		db:      db,
		rootDir: appDirectories.Path(DirectoryChatAttachments),
		drafts:  make(map[string]*chatAttachmentPayload),
	}
	_ = service.cleanupOwnedFiles()
	return service
}

// BindChatAttachmentPicker connects the service to Wails' native file dialog.
//
//wails:ignore
func BindChatAttachmentPicker(service *ChatAttachmentService, picker chatAttachmentPicker) {
	if service == nil {
		return
	}
	service.mu.Lock()
	service.picker = picker
	service.mu.Unlock()
}

func (s *ChatAttachmentService) draftDir() string     { return filepath.Join(s.rootDir, ".draft") }
func (s *ChatAttachmentService) fileDir() string      { return filepath.Join(s.rootDir, "files") }
func (s *ChatAttachmentService) thumbnailDir() string { return filepath.Join(s.rootDir, "thumbnails") }

// PickImages opens a Wails native multi-file picker and stages the selection.
func (s *ChatAttachmentService) PickImages() ([]ChatAttachmentDraft, error) {
	s.mu.Lock()
	picker := s.picker
	s.mu.Unlock()
	if picker == nil {
		return nil, errors.New("图片选择器未初始化")
	}
	paths, err := picker.PickChatImages()
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return []ChatAttachmentDraft{}, nil
	}
	if len(paths) > maxChatAttachmentCount {
		return nil, fmt.Errorf("一次最多选择 %d 张图片", maxChatAttachmentCount)
	}
	var total int64
	for _, path := range paths {
		info, statErr := os.Stat(path)
		if statErr != nil {
			return nil, fmt.Errorf("读取图片失败: %w", statErr)
		}
		total += info.Size()
	}
	if total > maxChatAttachmentTotal {
		return nil, errors.New("所选图片总大小不能超过 50 MB")
	}

	created := make([]ChatAttachmentDraft, 0, len(paths))
	createdIDs := make([]string, 0, len(paths))
	for _, path := range paths {
		draft, stageErr := s.stageFile(path, filepath.Base(path))
		if stageErr != nil {
			s.DiscardDrafts(createdIDs) //nolint:errcheck
			return nil, stageErr
		}
		created = append(created, draft)
		createdIDs = append(createdIDs, draft.ID)
	}
	return created, nil
}

// PasteImage stages the Windows clipboard image. Text-only clipboards return nil.
func (s *ChatAttachmentService) PasteImage() (*ChatAttachmentDraft, error) {
	payload, err := readWindowsClipboard()
	if err != nil {
		return nil, err
	}
	if payload.kind != "clipboard_image" {
		return nil, nil
	}
	draft, err := s.stageBytes(payload.imagePNG, "粘贴的图片.png")
	if err != nil {
		return nil, err
	}
	return &draft, nil
}

// DiscardDrafts removes composer drafts that have not been sent.
func (s *ChatAttachmentService) DiscardDrafts(ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		draft, ok := s.drafts[id]
		if !ok || draft.claimed {
			continue
		}
		_ = os.Remove(draft.draftPath)
		_ = os.Remove(draft.draftThumbnailPath)
		delete(s.drafts, id)
	}
	return nil
}

// GetImageDataURI loads the original image on demand for the preview overlay.
func (s *ChatAttachmentService) GetImageDataURI(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", errors.New("图片 ID 不能为空")
	}
	s.mu.Lock()
	if draft, ok := s.drafts[id]; ok {
		path := draft.draftPath
		mimeType := draft.MIMEType
		s.mu.Unlock()
		data, err := readLimitedFile(path, maxChatAttachmentBytes)
		if err != nil {
			return "", err
		}
		return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
	}
	s.mu.Unlock()
	var path, mimeType string
	if err := s.db.QueryRow(
		"SELECT file_path, mime_type FROM message_attachments WHERE id = ?", id,
	).Scan(&path, &mimeType); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", errors.New("图片不存在或已被删除")
		}
		return "", err
	}
	data, err := readLimitedFile(path, maxChatAttachmentBytes)
	if err != nil {
		return "", err
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

func (s *ChatAttachmentService) stageFile(path, name string) (ChatAttachmentDraft, error) {
	data, err := readLimitedFile(path, maxChatAttachmentBytes)
	if err != nil {
		return ChatAttachmentDraft{}, err
	}
	return s.stageBytes(data, name)
}

func (s *ChatAttachmentService) stageBytes(data []byte, name string) (ChatAttachmentDraft, error) {
	meta, decoded, err := validateChatImage(data)
	if err != nil {
		return ChatAttachmentDraft{}, err
	}
	id := uuid.NewString()
	if err := os.MkdirAll(s.draftDir(), 0o700); err != nil {
		return ChatAttachmentDraft{}, fmt.Errorf("创建图片草稿目录失败: %w", err)
	}
	draftPath := filepath.Join(s.draftDir(), id+meta.extension)
	thumbnailPath := filepath.Join(s.draftDir(), id+".thumb.png")
	if err := os.WriteFile(draftPath, data, 0o600); err != nil {
		return ChatAttachmentDraft{}, fmt.Errorf("保存图片草稿失败: %w", err)
	}
	thumbnail, err := makeChatThumbnail(decoded, meta.width, meta.height)
	if err != nil {
		_ = os.Remove(draftPath)
		return ChatAttachmentDraft{}, fmt.Errorf("生成图片缩略图失败: %w", err)
	}
	if err := os.WriteFile(thumbnailPath, thumbnail, 0o600); err != nil {
		_ = os.Remove(draftPath)
		return ChatAttachmentDraft{}, fmt.Errorf("保存图片缩略图失败: %w", err)
	}
	draft := ChatAttachmentDraft{
		ID: id, Name: strings.TrimSpace(name), MIMEType: meta.mimeType,
		SizeBytes: int64(len(data)), Width: meta.width, Height: meta.height,
		ThumbnailDataURI: "data:image/png;base64," + base64.StdEncoding.EncodeToString(thumbnail),
	}
	s.mu.Lock()
	s.drafts[id] = &chatAttachmentPayload{
		ChatAttachmentDraft: draft, draftPath: draftPath,
		draftThumbnailPath: thumbnailPath, extension: meta.extension,
	}
	s.mu.Unlock()
	return draft, nil
}

type chatImageMetadata struct {
	mimeType  string
	extension string
	width     int
	height    int
}

func validateChatImage(data []byte) (chatImageMetadata, image.Image, error) {
	if len(data) == 0 {
		return chatImageMetadata{}, nil, errors.New("图片内容为空")
	}
	if len(data) > maxChatAttachmentBytes {
		return chatImageMetadata{}, nil, errors.New("单张图片不能超过 20 MB")
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

func detectChatImageType(data []byte) (string, string) {
	switch {
	case len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}):
		return "image/png", ".png"
	case len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff:
		return "image/jpeg", ".jpg"
	case len(data) >= 6 && (string(data[:6]) == "GIF87a" || string(data[:6]) == "GIF89a"):
		return "image/gif", ".gif"
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "image/webp", ".webp"
	default:
		return "", ""
	}
}

func makeChatThumbnail(src image.Image, width, height int) ([]byte, error) {
	scale := float64(chatThumbnailMaxSize) / float64(max(width, height))
	if scale > 1 {
		scale = 1
	}
	targetWidth := max(1, int(float64(width)*scale))
	targetHeight := max(1, int(float64(height)*scale))
	target := image.NewNRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	draw.CatmullRom.Scale(target, target.Bounds(), src, src.Bounds(), draw.Over, nil)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, target); err != nil {
		return nil, err
	}
	return encoded.Bytes(), nil
}

func readLimitedFile(path string, limit int) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("打开图片失败: %w", err)
	}
	defer file.Close() //nolint:errcheck
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > int64(limit) {
		return nil, errors.New("单张图片不能超过 20 MB")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (s *ChatAttachmentService) validateDrafts(ids []string) ([]*chatAttachmentPayload, error) {
	if len(ids) > maxChatAttachmentCount {
		return nil, fmt.Errorf("一次最多发送 %d 张图片", maxChatAttachmentCount)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := make(map[string]struct{}, len(ids))
	items := make([]*chatAttachmentPayload, 0, len(ids))
	var total int64
	for _, id := range ids {
		if _, duplicate := seen[id]; duplicate {
			return nil, errors.New("图片附件重复")
		}
		seen[id] = struct{}{}
		draft, ok := s.drafts[id]
		if !ok || draft.claimed {
			return nil, errors.New("图片草稿已失效，请重新选择")
		}
		total += draft.SizeBytes
		items = append(items, draft)
	}
	if total > maxChatAttachmentTotal {
		return nil, errors.New("图片总大小不能超过 50 MB")
	}
	return items, nil
}

// attachDrafts moves validated drafts and inserts their rows in the caller's transaction.
func (s *ChatAttachmentService) attachDrafts(tx *sql.Tx, messageID int64, ids []string) (func(bool), error) {
	if len(ids) == 0 {
		return func(bool) {}, nil
	}
	s.mu.Lock()
	items := make([]*chatAttachmentPayload, 0, len(ids))
	for _, id := range ids {
		draft, ok := s.drafts[id]
		if !ok || draft.claimed {
			s.mu.Unlock()
			return nil, errors.New("图片草稿已失效，请重新选择")
		}
		draft.claimed = true
		items = append(items, draft)
	}
	s.mu.Unlock()
	if err := os.MkdirAll(s.fileDir(), 0o700); err != nil {
		s.releaseClaims(items)
		return nil, err
	}
	if err := os.MkdirAll(s.thumbnailDir(), 0o700); err != nil {
		s.releaseClaims(items)
		return nil, err
	}

	type move struct{ from, to string }
	moves := make([]move, 0, len(items)*2)
	rollbackMoves := func() {
		for i := len(moves) - 1; i >= 0; i-- {
			_ = os.Rename(moves[i].to, moves[i].from)
		}
	}
	for position, draft := range items {
		finalPath := filepath.Join(s.fileDir(), draft.ID+draft.extension)
		finalThumbnailPath := filepath.Join(s.thumbnailDir(), draft.ID+".png")
		if err := os.Rename(draft.draftPath, finalPath); err != nil {
			rollbackMoves()
			s.releaseClaims(items)
			return nil, fmt.Errorf("保存聊天图片失败: %w", err)
		}
		moves = append(moves, move{draft.draftPath, finalPath})
		if err := os.Rename(draft.draftThumbnailPath, finalThumbnailPath); err != nil {
			rollbackMoves()
			s.releaseClaims(items)
			return nil, fmt.Errorf("保存聊天图片缩略图失败: %w", err)
		}
		moves = append(moves, move{draft.draftThumbnailPath, finalThumbnailPath})
		if _, err := tx.Exec(`
			INSERT INTO message_attachments (
				id, message_id, kind, file_path, thumbnail_path, mime_type,
				original_name, width, height, size_bytes, position, created_at
			) VALUES (?, ?, 'image', ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			draft.ID, messageID, finalPath, finalThumbnailPath, draft.MIMEType,
			draft.Name, draft.Width, draft.Height, draft.SizeBytes, position, now()); err != nil {
			rollbackMoves()
			s.releaseClaims(items)
			return nil, err
		}
	}
	return func(committed bool) {
		if !committed {
			rollbackMoves()
			s.releaseClaims(items)
			return
		}
		s.mu.Lock()
		for _, draft := range items {
			delete(s.drafts, draft.ID)
		}
		s.mu.Unlock()
	}, nil
}

func (s *ChatAttachmentService) releaseClaims(items []*chatAttachmentPayload) {
	s.mu.Lock()
	for _, item := range items {
		item.claimed = false
	}
	s.mu.Unlock()
}

func (s *ChatAttachmentService) cleanupOwnedFiles() error {
	if err := os.RemoveAll(s.draftDir()); err != nil {
		return err
	}
	if s.db == nil {
		return nil
	}
	rows, err := s.db.Query("SELECT file_path, thumbnail_path FROM message_attachments")
	if err != nil {
		return err
	}
	used := make(map[string]struct{})
	for rows.Next() {
		var filePath, thumbnailPath string
		if err := rows.Scan(&filePath, &thumbnailPath); err != nil {
			rows.Close()
			return err
		}
		used[strings.ToLower(filepath.Clean(filePath))] = struct{}{}
		used[strings.ToLower(filepath.Clean(thumbnailPath))] = struct{}{}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, dir := range []string{s.fileDir(), s.thumbnailDir()} {
		_ = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil || entry.IsDir() {
				return nil
			}
			if _, ok := used[strings.ToLower(filepath.Clean(path))]; !ok {
				_ = os.Remove(path)
			}
			return nil
		})
	}
	return nil
}

func (s *ChatAttachmentService) ServiceShutdown() error {
	s.mu.Lock()
	drafts := s.drafts
	s.drafts = make(map[string]*chatAttachmentPayload)
	s.mu.Unlock()
	for _, draft := range drafts {
		_ = os.Remove(draft.draftPath)
		_ = os.Remove(draft.draftThumbnailPath)
	}
	return os.RemoveAll(s.draftDir())
}
