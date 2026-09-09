package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AntNoHuabei/blankmind/internal/capture"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// ScreenshotResult 一次截图的结果(前端弹菜单预览/后续处理)。
type ScreenshotResult struct {
	ID        int64  `json:"id"`
	Path      string `json:"path"`
	DataURI   string `json:"dataUri"` // JPEG 预览(data URI)
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	CreatedAt int64  `json:"createdAt"`
}

// SaveShotReq “仅保存”动作入参。
type SaveShotReq struct {
	ID   int64  `json:"id"`
	Note string `json:"note"`
}

// ExtractedTodo 视觉模型从截图提取的待办(供前端预览确认)。
type ExtractedTodo struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Milestone   bool   `json:"milestone"`
	DueDate     string `json:"dueDate"` // YYYY-MM-DD 或 YYYY-MM-DD HH:MM,可为空
}

// ConfirmExtractedReq 前端确认后批量入库的入参。
type ConfirmExtractedReq struct {
	ShotID int64           `json:"shotId"`
	Items  []ExtractedTodo `json:"items"`
}

// AskShotReq 截图问答入参。
type AskShotReq struct {
	ShotID   int64  `json:"shotId"`
	Question string `json:"question"`
}

// ScreenshotService 提供截屏、保存与视觉 AI 处理(转待办/问答)能力。
type ScreenshotService struct {
	db     *sql.DB
	notify func(name string, data any)
}

// NewScreenshotService 构造截图服务。
func NewScreenshotService(db *sql.DB) *ScreenshotService {
	return &ScreenshotService{db: db}
}

// SetNotify 注入事件广播回调。
func (s *ScreenshotService) SetNotify(fn func(name string, data any)) { s.notify = fn }

// EmitEvent 跨包(热键等)触发该服务事件,等价于服务内部 emit。
func (s *ScreenshotService) EmitEvent(name string, data any) { s.emit(name, data) }

func (s *ScreenshotService) emit(name string, data any) {
	if s.notify != nil {
		s.notify(name, data)
	}
}

// Capture 捕获主屏:JPEG 落盘 + 返回 data URI 预览,并记录截图条目。
// 是否“保存”由用户在菜单里决定(此时才写操作日志)。
func (s *ScreenshotService) Capture() (ScreenshotResult, error) {
	img, err := capture.Screen()
	if err != nil {
		return ScreenshotResult{}, fmt.Errorf("截屏失败: %w", err)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		return ScreenshotResult{}, fmt.Errorf("编码截图失败: %w", err)
	}
	data := buf.Bytes()

	dir := filepath.Join(dataDir(), "screenshots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ScreenshotResult{}, fmt.Errorf("创建截图目录失败: %w", err)
	}
	path := filepath.Join(dir, fmt.Sprintf("shot_%d.jpg", now()))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return ScreenshotResult{}, fmt.Errorf("保存截图失败: %w", err)
	}

	res, err := s.db.Exec(
		"INSERT INTO screenshots (path, note, created_at) VALUES (?, ?, ?)",
		path, "", now())
	if err != nil {
		return ScreenshotResult{}, err
	}
	id, _ := res.LastInsertId()
	b := img.Bounds()
	return ScreenshotResult{
		ID:        id,
		Path:      path,
		DataURI:   "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(data),
		Width:     b.Dx(),
		Height:    b.Dy(),
		CreatedAt: now(),
	}, nil
}

// getShot 读取截图记录(路径等)。
func (s *ScreenshotService) getShot(id int64) (Screenshot, error) {
	var sh Screenshot
	err := s.db.QueryRow(
		"SELECT id, path, note, created_at FROM screenshots WHERE id = ?", id).
		Scan(&sh.ID, &sh.Path, &sh.Note, &sh.CreatedAt)
	if err == sql.ErrNoRows {
		return sh, errors.New("截图不存在")
	}
	return sh, err
}

// SaveShot “仅保存”:补备注并写操作日志。
func (s *ScreenshotService) SaveShot(req SaveShotReq) error {
	sh, err := s.getShot(req.ID)
	if err != nil {
		return err
	}
	note := strings.TrimSpace(req.Note)
	if _, err := s.db.Exec("UPDATE screenshots SET note = ? WHERE id = ?", note, req.ID); err != nil {
		return err
	}
	if _, err := insertEvent(s.db, EventScreenshotAdded, "保存截图:"+filepath.Base(sh.Path), sh.ID); err != nil {
		return err
	}
	s.emit("screenshot.saved", map[string]any{"id": sh.ID})
	return nil
}

// defaultVisionProvider 返回支持图片输入的默认服务商;否则给出明确引导错误。
func defaultVisionProvider() (Provider, error) {
	p, err := settingsSvc.DefaultProvider()
	if err != nil {
		return Provider{}, errors.New("尚未配置模型服务商,请先在设置中配置")
	}
	if !providerSupportsVision(p) {
		return Provider{}, errors.New("当前默认模型不支持图片输入。请在设置中把支持视觉的模型(如 OpenAI gpt-4o)设为当前,或为自定义模型勾选“支持图片输入”")
	}
	return p, nil
}

// visionOnce 单轮视觉问答:文本 + 本地图片 → 文本回复。
func visionOnce(p Provider, prompt, imgPath string) (string, error) {
	m, err := buildModel(p)
	if err != nil {
		return "", err
	}
	msg := model.NewUserMessage(prompt)
	if err := msg.AddImageFilePath(imgPath, "auto"); err != nil {
		return "", fmt.Errorf("读取截图失败: %w", err)
	}
	req := model.NewRequest([]model.Message{msg})
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	ch, err := m.GenerateContent(ctx, req)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	for rsp := range ch {
		if rsp.Error != nil {
			return "", fmt.Errorf("%v", rsp.Error)
		}
		for _, c := range rsp.Choices {
			if c.Delta.Content != "" {
				out.WriteString(c.Delta.Content)
			}
			if c.Message.Content != "" && c.Delta.Content == "" {
				out.WriteString(c.Message.Content)
			}
		}
	}
	if strings.TrimSpace(out.String()) == "" {
		return "", errors.New("模型未返回有效内容")
	}
	return strings.TrimSpace(out.String()), nil
}

// ExtractTodos 用视觉模型把截图里的任务/安排提取为结构化待办(供预览)。
func (s *ScreenshotService) ExtractTodos(id int64) ([]ExtractedTodo, error) {
	sh, err := s.getShot(id)
	if err != nil {
		return nil, err
	}
	p, err := defaultVisionProvider()
	if err != nil {
		return nil, err
	}
	prompt := `你是办公助手。请仔细阅读这张截图,把其中出现的所有任务、待办、安排、会议或里程碑逐条提取。
要求:
1. 只输出一个 JSON 数组,不要输出任何解释或 Markdown 代码块标记。
2. 数组元素格式:{"title": "简短标题", "description": "补充说明(无则空字符串)", "milestone": false, "dueDate": ""}
3. title 用原文语言概括;若截图里有明确截止时间(如"3月15日""2026-03-15 18:00"),把 dueDate 写成 YYYY-MM-DD 或 YYYY-MM-DD HH:MM;没有则为空字符串。
4. 若截图中没有任何任务类内容,输出 []。
5. 里程碑/重要节点把 milestone 置为 true。`
	out, err := visionOnce(p, prompt, sh.Path)
	if err != nil {
		return nil, err
	}
	return parseTodoJSON(out)
}

// parseTodoJSON 从模型回复中稳健地解析 JSON 数组(容忍被 Markdown 围栏包裹等)。
func parseTodoJSON(raw string) ([]ExtractedTodo, error) {
	text := strings.TrimSpace(raw)
	if idx := strings.Index(text, "["); idx >= 0 {
		text = text[idx:]
	}
	if j := strings.LastIndex(text, "]"); j >= 0 {
		text = text[:j+1]
	}
	var items []ExtractedTodo
	if err := json.Unmarshal([]byte(text), &items); err != nil {
		// 兜底:尝试剥离 ```json 围栏
		fenced := strings.TrimSuffix(strings.TrimPrefix(raw, "```json"), "```")
		fenced = strings.TrimSuffix(strings.TrimPrefix(fenced, "```"), "```")
		if err2 := json.Unmarshal([]byte(strings.TrimSpace(fenced)), &items); err2 != nil {
			return nil, errors.New("无法解析模型返回的待办列表,请重试或改用“问答”手动处理")
		}
	}
	return items, nil
}

// parseDueDate 把模型给出的日期串转 unix 秒;无法识别返回 0(表示无截止)。
func parseDueDate(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	layouts := []string{
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
		time.RFC3339,
	}
	for _, l := range layouts {
		if t, err := time.ParseInLocation(l, s, time.Local); err == nil {
			return t.Unix()
		}
	}
	return 0
}

// ConfirmExtracted 把前端确认后的截图待办批量入库(来源 screenshot)。
func (s *ScreenshotService) ConfirmExtracted(req ConfirmExtractedReq) ([]Todo, error) {
	if todoSvc == nil {
		return nil, errors.New("待办服务未初始化")
	}
	created := make([]Todo, 0, len(req.Items))
	for _, it := range req.Items {
		title := strings.TrimSpace(it.Title)
		if title == "" {
			continue
		}
		t, err := todoSvc.CreateTodo(TodoInput{
			Title:       title,
			Description: strings.TrimSpace(it.Description),
			Deadline:    parseDueDate(it.DueDate),
			IsMilestone: it.Milestone,
			Status:      TodoStatusPending,
			Source:      "screenshot",
		})
		if err != nil {
			return created, err
		}
		created = append(created, t)
	}
	if len(created) > 0 {
		if _, err := insertEvent(s.db, EventScreenshotAdded,
			fmt.Sprintf("截图转待办 %d 条", len(created)), req.ShotID); err != nil {
			return created, err
		}
		s.emit("screenshot.processed", map[string]any{"shotId": req.ShotID, "count": len(created)})
	}
	return created, nil
}

// AskAboutShot 对截图进行单轮问答(解释/翻译/描述/追问等)。
func (s *ScreenshotService) AskAboutShot(req AskShotReq) (string, error) {
	q := strings.TrimSpace(req.Question)
	if q == "" {
		return "", errors.New("问题不能为空")
	}
	sh, err := s.getShot(req.ShotID)
	if err != nil {
		return "", err
	}
	p, err := defaultVisionProvider()
	if err != nil {
		return "", err
	}
	return visionOnce(p, "请结合这张截图回答用户的问题。问题:"+q, sh.Path)
}

// ScreenshotService 绑定方法注释:
//   - Capture / SaveShot / ExtractTodos / ConfirmExtracted / AskAboutShot
//     是前端(含截图菜单)调用的入口。
