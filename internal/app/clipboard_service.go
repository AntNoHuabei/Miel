package app

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

type ClipboardTodoDraft struct {
	DraftID   string          `json:"draftId"`
	Kind      string          `json:"kind"`
	Text      string          `json:"text"`
	DataURI   string          `json:"dataUri"`
	CreatedAt int64           `json:"createdAt"`
	Items     []ExtractedTodo `json:"items"`
}

type ConfirmClipboardTodosReq struct {
	DraftID string          `json:"draftId"`
	Items   []ExtractedTodo `json:"items"`
}

type clipboardDraftPayload struct {
	kind      string
	text      string
	draftPath string
	createdAt int64
}

// ClipboardService reads Windows clipboard text/images and owns extraction drafts.
type ClipboardService struct {
	todo      *TodoService
	sourceDir string
	mu        sync.Mutex
	drafts    map[string]clipboardDraftPayload
}

func NewClipboardService(todo *TodoService) *ClipboardService {
	service := &ClipboardService{
		todo:      todo,
		sourceDir: filepath.Join(dataDir(), "sources", "clipboard"),
		drafts:    make(map[string]clipboardDraftPayload),
	}
	_ = os.RemoveAll(filepath.Join(service.sourceDirectory(), ".draft"))
	return service
}

func (s *ClipboardService) sourceDirectory() string {
	if s.sourceDir != "" {
		return s.sourceDir
	}
	return filepath.Join(dataDir(), "sources", "clipboard")
}

func (s *ClipboardService) ExtractTodos() (ClipboardTodoDraft, error) {
	payload, err := readWindowsClipboard()
	if err != nil {
		return ClipboardTodoDraft{}, err
	}
	draftID := uuid.NewString()
	createdAt := now()
	draft := clipboardDraftPayload{kind: payload.kind, text: payload.text, createdAt: createdAt}
	var items []ExtractedTodo
	var dataURI string
	if payload.kind == "clipboard_image" {
		dir := filepath.Join(s.sourceDirectory(), ".draft")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return ClipboardTodoDraft{}, fmt.Errorf("创建粘贴板临时目录失败: %w", err)
		}
		draft.draftPath = filepath.Join(dir, draftID+".png")
		if err := os.WriteFile(draft.draftPath, payload.imagePNG, 0o600); err != nil {
			return ClipboardTodoDraft{}, fmt.Errorf("保存粘贴板临时图片失败: %w", err)
		}
		provider, err := defaultVisionProvider()
		if err != nil {
			_ = os.Remove(draft.draftPath)
			return ClipboardTodoDraft{}, err
		}
		out, err := visionOnce(provider, todoExtractionPrompt("图片"), draft.draftPath)
		if err != nil {
			_ = os.Remove(draft.draftPath)
			return ClipboardTodoDraft{}, err
		}
		items, err = parseTodoJSON(out)
		if err != nil {
			_ = os.Remove(draft.draftPath)
			return ClipboardTodoDraft{}, err
		}
		dataURI = "data:image/png;base64," + base64.StdEncoding.EncodeToString(payload.imagePNG)
	} else {
		provider, err := settingsSvc.DefaultProvider()
		if err != nil {
			return ClipboardTodoDraft{}, errors.New("尚未配置模型服务商,请先在设置中配置")
		}
		out, err := textOnce(provider, todoExtractionPrompt("文本")+"\n\n原始文本:\n"+payload.text)
		if err != nil {
			return ClipboardTodoDraft{}, err
		}
		items, err = parseTodoJSON(out)
		if err != nil {
			return ClipboardTodoDraft{}, err
		}
	}
	s.mu.Lock()
	s.drafts[draftID] = draft
	s.mu.Unlock()
	return ClipboardTodoDraft{
		DraftID: draftID, Kind: payload.kind, Text: payload.text,
		DataURI: dataURI, CreatedAt: createdAt, Items: items,
	}, nil
}

func (s *ClipboardService) ConfirmTodos(req ConfirmClipboardTodosReq) ([]Todo, error) {
	s.mu.Lock()
	draft, ok := s.drafts[req.DraftID]
	s.mu.Unlock()
	if !ok {
		return nil, errors.New("粘贴板待办草稿已失效,请重新提取")
	}
	inputs := make([]TodoInput, 0, len(req.Items))
	for _, item := range req.Items {
		title := strings.TrimSpace(item.Title)
		if title == "" {
			continue
		}
		inputs = append(inputs, TodoInput{
			Title: title, Description: strings.TrimSpace(item.Description),
			Deadline: parseDueDate(item.DueDate), IsMilestone: item.Milestone,
			Status: TodoStatusPending, Source: "clipboard",
		})
	}
	if len(inputs) == 0 {
		s.DiscardDraft(req.DraftID)
		return nil, errors.New("请至少选择一条有效待办")
	}
	source := todoSourceInput{Kind: draft.kind, TextContent: draft.text, MIMEType: "text/plain"}
	var finalPath string
	if draft.kind == "clipboard_image" {
		dir := s.sourceDirectory()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
		finalPath = filepath.Join(dir, fmt.Sprintf("clipboard_%d_%s.png", draft.createdAt, req.DraftID))
		if err := os.Rename(draft.draftPath, finalPath); err != nil {
			return nil, fmt.Errorf("保存粘贴板来源图片失败: %w", err)
		}
		source.FilePath = finalPath
		source.MIMEType = "image/png"
	}
	created, err := s.todo.createTodosWithSource(inputs, source)
	if err != nil {
		if finalPath != "" {
			if restoreErr := os.Rename(finalPath, draft.draftPath); restoreErr != nil {
				_ = os.Remove(finalPath)
				s.mu.Lock()
				delete(s.drafts, req.DraftID)
				s.mu.Unlock()
			}
		}
		return nil, err
	}
	s.mu.Lock()
	delete(s.drafts, req.DraftID)
	s.mu.Unlock()
	return created, nil
}

func (s *ClipboardService) DiscardDraft(draftID string) {
	s.mu.Lock()
	draft, ok := s.drafts[draftID]
	delete(s.drafts, draftID)
	s.mu.Unlock()
	if ok && draft.draftPath != "" {
		_ = os.Remove(draft.draftPath)
	}
}

func (s *ClipboardService) ServiceShutdown() error {
	s.mu.Lock()
	drafts := s.drafts
	s.drafts = make(map[string]clipboardDraftPayload)
	s.mu.Unlock()
	for _, draft := range drafts {
		if draft.draftPath != "" {
			_ = os.Remove(draft.draftPath)
		}
	}
	return nil
}

func textOnce(provider Provider, prompt string) (string, error) {
	llm, err := buildModel(provider)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	responses, err := llm.GenerateContent(ctx, model.NewRequest([]model.Message{model.NewUserMessage(prompt)}))
	if err != nil {
		return "", err
	}
	var output strings.Builder
	for response := range responses {
		if response.Error != nil {
			return "", fmt.Errorf("%v", response.Error)
		}
		for _, choice := range response.Choices {
			if choice.Delta.Content != "" {
				output.WriteString(choice.Delta.Content)
			} else if choice.Message.Content != "" {
				output.WriteString(choice.Message.Content)
			}
		}
	}
	if strings.TrimSpace(output.String()) == "" {
		return "", errors.New("模型未返回有效内容")
	}
	return strings.TrimSpace(output.String()), nil
}

func todoExtractionPrompt(inputKind string) string {
	return `你是办公助手。请从以下` + inputKind + `中提取所有明确的任务、安排、会议或里程碑。
要求:
1. 只输出 JSON 数组,不要输出解释或 Markdown 围栏。
2. 数组元素格式:{"title":"简短标题","description":"补充说明","milestone":false,"dueDate":""}。
3. 明确的日期转为 YYYY-MM-DD 或 YYYY-MM-DD HH:MM;没有截止日期时 dueDate 为空。
4. 不推测原内容没有表达的任务;没有任务时输出 []。
5. 重要节点或里程碑把 milestone 设为 true。`
}
