package app

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// TodoService 负责待办 / 里程碑的 CRUD 与操作日志,并向前端广播变更。
type TodoService struct {
	db *sql.DB
	// Notify 由 main 装配后注入,用于向应用事件总线广播。
	Notify func(name string, data any)
}

// NewTodoService 构造 TodoService。
func NewTodoService(db *sql.DB) *TodoService {
	return &TodoService{db: db}
}

func (s *TodoService) notify(name string, data any) {
	if s.Notify != nil {
		s.Notify(name, data)
	}
}

// ListTodos 返回全部待办(调用方自行过滤/排序,数据量小)。
func (s *TodoService) ListTodos() ([]Todo, error) {
	rows, err := s.db.Query(`
		SELECT id, title, description, deadline, is_milestone, status, source, source_id, created_at, done_at
		FROM todos ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Todo{}
	for rows.Next() {
		var t Todo
		var ms int
		var sourceID sql.NullInt64
		if err := rows.Scan(&t.ID, &t.Title, &t.Description, &t.Deadline, &ms,
			&t.Status, &t.Source, &sourceID, &t.CreatedAt, &t.DoneAt); err != nil {
			return nil, err
		}
		t.IsMilestone = ms != 0
		t.SourceID = sourceID.Int64
		items = append(items, t)
	}
	return items, rows.Err()
}

// GetTodo 按 ID 读取单个待办。
func (s *TodoService) GetTodo(id int64) (Todo, error) {
	var t Todo
	var ms int
	var sourceID sql.NullInt64
	err := s.db.QueryRow(`
		SELECT id, title, description, deadline, is_milestone, status, source, source_id, created_at, done_at
		FROM todos WHERE id = ?`, id).
		Scan(&t.ID, &t.Title, &t.Description, &t.Deadline, &ms,
			&t.Status, &t.Source, &sourceID, &t.CreatedAt, &t.DoneAt)
	if err != nil {
		return t, err
	}
	t.IsMilestone = ms != 0
	t.SourceID = sourceID.Int64
	return t, nil
}

// CreateTodo 新增待办并写操作日志。
func (s *TodoService) CreateTodo(in TodoInput) (Todo, error) {
	return s.createTodo(in, nil)
}

func (s *TodoService) createTodo(in TodoInput, sourceInput *todoSourceInput) (Todo, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return Todo{}, errors.New("标题不能为空")
	}
	status := in.Status
	if status == "" {
		status = TodoStatusPending
	}
	source := in.Source
	if source == "" {
		source = "manual"
	}
	ts := now()
	doneAt := int64(0)
	if status == TodoStatusDone {
		doneAt = ts
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Todo{}, err
	}
	defer tx.Rollback() //nolint:errcheck
	sourceID := in.SourceID
	if sourceInput != nil {
		sourceID, err = insertTodoSource(tx, *sourceInput)
		if err != nil {
			return Todo{}, err
		}
	}
	res, err := tx.Exec(`
		INSERT INTO todos (title, description, deadline, is_milestone, status, source, source_id, created_at, done_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		title, in.Description, in.Deadline, boolToInt(in.IsMilestone),
		status, source, nullableSourceID(sourceID), ts, doneAt)
	if err != nil {
		return Todo{}, err
	}
	id, _ := res.LastInsertId()
	if _, err := tx.Exec("INSERT INTO events (ts, type, summary, ref_id) VALUES (?, ?, ?, ?)",
		now(), EventTodoCreated, "新增待办:"+title, id); err != nil {
		return Todo{}, err
	}
	if err := tx.Commit(); err != nil {
		return Todo{}, err
	}
	t, err := s.GetTodo(id)
	if err == nil {
		s.notify("todos.changed", "created")
		if sourceID > 0 {
			s.notify("todo.source.changed", sourceID)
		}
	}
	return t, err
}

func (s *TodoService) createTodosWithSource(inputs []TodoInput, source todoSourceInput) ([]Todo, error) {
	if len(inputs) == 0 {
		return []Todo{}, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck
	sourceID, err := insertTodoSource(tx, source)
	if err != nil {
		return nil, err
	}
	created := make([]Todo, 0, len(inputs))
	for _, in := range inputs {
		title := strings.TrimSpace(in.Title)
		if title == "" {
			return nil, errors.New("标题不能为空")
		}
		status := in.Status
		if status == "" {
			status = TodoStatusPending
		}
		ts := now()
		doneAt := int64(0)
		if status == TodoStatusDone {
			doneAt = ts
		}
		res, err := tx.Exec(`
			INSERT INTO todos (
				title, description, deadline, is_milestone, status, source,
				source_id, created_at, done_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			title, in.Description, in.Deadline, boolToInt(in.IsMilestone), status,
			in.Source, sourceID, ts, doneAt)
		if err != nil {
			return nil, err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec("INSERT INTO events (ts, type, summary, ref_id) VALUES (?, ?, ?, ?)",
			ts, EventTodoCreated, "新增待办:"+title, id); err != nil {
			return nil, err
		}
		created = append(created, Todo{
			ID: id, Title: title, Description: in.Description, Deadline: in.Deadline,
			IsMilestone: in.IsMilestone, Status: status, Source: in.Source,
			SourceID: sourceID, CreatedAt: ts, DoneAt: doneAt,
		})
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	s.notify("todos.changed", "created")
	s.notify("todo.source.changed", sourceID)
	return created, nil
}

// UpdateTodo 更新待办的信息字段与状态;状态流转为 done 时写完成日志。
func (s *TodoService) UpdateTodo(in TodoInput) (Todo, error) {
	if in.ID <= 0 {
		return Todo{}, errors.New("缺少待办 ID")
	}
	old, err := s.GetTodo(in.ID)
	if err != nil {
		return Todo{}, err
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return Todo{}, errors.New("标题不能为空")
	}
	status := in.Status
	if status == "" {
		status = old.Status
	}
	doneAt := old.DoneAt
	if status == TodoStatusDone && old.Status != TodoStatusDone {
		doneAt = now()
		insertEvent(s.db, EventTodoDone, "完成待办:"+old.Title, old.ID) //nolint:errcheck
	}
	if _, err := s.db.Exec(`
		UPDATE todos SET title=?, description=?, deadline=?, is_milestone=?, status=?, done_at=?
		WHERE id=?`,
		title, in.Description, in.Deadline, boolToInt(in.IsMilestone),
		status, doneAt, in.ID); err != nil {
		return Todo{}, err
	}
	t, err := s.GetTodo(in.ID)
	if err == nil {
		s.notify("todos.changed", "updated")
	}
	return t, err
}

// SetTodoStatus 快捷改状态(勾选完成 / 重开)。
func (s *TodoService) SetTodoStatus(id int64, status string) (Todo, error) {
	old, err := s.GetTodo(id)
	if err != nil {
		return Todo{}, err
	}
	if status != TodoStatusPending && status != TodoStatusDoing && status != TodoStatusDone {
		return Todo{}, errors.New("非法状态:" + status)
	}
	doneAt := old.DoneAt
	if status == TodoStatusDone && old.Status != TodoStatusDone {
		doneAt = now()
		insertEvent(s.db, EventTodoDone, "完成待办:"+old.Title, old.ID) //nolint:errcheck
	} else if status != TodoStatusDone {
		doneAt = 0
	}
	if _, err := s.db.Exec("UPDATE todos SET status=?, done_at=? WHERE id=?", status, doneAt, id); err != nil {
		return Todo{}, err
	}
	t, err := s.GetTodo(id)
	if err == nil {
		s.notify("todos.changed", "status:"+status)
	}
	return t, err
}

// DeleteTodo 删除待办并写日志。
func (s *TodoService) DeleteTodo(id int64) error {
	old, err := s.GetTodo(id)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec("DELETE FROM todos WHERE id = ?", id); err != nil {
		return err
	}
	if _, err := tx.Exec("INSERT INTO events (ts, type, summary, ref_id) VALUES (?, ?, ?, ?)",
		now(), EventTodoDeleted, "删除待办:"+old.Title, id); err != nil {
		return err
	}
	var ownedPath string
	if old.SourceID > 0 {
		var refs int
		if err := tx.QueryRow("SELECT COUNT(*) FROM todos WHERE source_id = ?", old.SourceID).Scan(&refs); err != nil {
			return err
		}
		if refs == 0 {
			var kind string
			if err := tx.QueryRow("SELECT kind, file_path FROM todo_sources WHERE id = ?", old.SourceID).
				Scan(&kind, &ownedPath); err != nil && err != sql.ErrNoRows {
				return err
			}
			if kind != "clipboard_image" {
				ownedPath = ""
			}
			if _, err := tx.Exec("DELETE FROM todo_sources WHERE id = ?", old.SourceID); err != nil {
				return err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if ownedPath != "" {
		_ = os.Remove(ownedPath)
	}
	s.notify("todos.changed", "deleted")
	if old.SourceID > 0 {
		s.notify("todo.source.changed", old.SourceID)
	}
	return nil
}

func insertTodoSource(tx *sql.Tx, source todoSourceInput) (int64, error) {
	res, err := tx.Exec(`
		INSERT INTO todo_sources (
			kind, text_content, file_path, mime_type, conversation_id,
			message_id, screenshot_id, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		source.Kind, source.TextContent, source.FilePath, source.MIMEType,
		source.ConversationID, source.MessageID, source.ScreenshotID, now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func nullableSourceID(sourceID int64) any {
	if sourceID <= 0 {
		return nil
	}
	return sourceID
}

// GetTodoSource returns the immutable source payload used to create a todo.
func (s *TodoService) GetTodoSource(sourceID int64) (TodoSource, error) {
	if sourceID <= 0 {
		return TodoSource{}, errors.New("该待办没有可追溯来源")
	}
	var source TodoSource
	err := s.db.QueryRow(`
		SELECT id, kind, text_content, file_path, mime_type, conversation_id,
			message_id, screenshot_id, created_at
		FROM todo_sources WHERE id = ?`, sourceID).
		Scan(&source.ID, &source.Kind, &source.TextContent, &source.FilePath,
			&source.MIMEType, &source.ConversationID, &source.MessageID,
			&source.ScreenshotID, &source.CreatedAt)
	if err == sql.ErrNoRows {
		return TodoSource{}, errors.New("待办来源不存在")
	}
	if err != nil {
		return TodoSource{}, err
	}
	if source.ConversationID > 0 {
		if err := s.db.QueryRow("SELECT title FROM conversations WHERE id = ?", source.ConversationID).
			Scan(&source.ConversationTitle); err == nil {
			source.ConversationAvailable = true
		}
	}
	if source.ScreenshotID > 0 {
		var shotPath string
		_ = s.db.QueryRow("SELECT path, note FROM screenshots WHERE id = ?", source.ScreenshotID).
			Scan(&shotPath, &source.ScreenshotNote)
		if source.FilePath == "" {
			source.FilePath = shotPath
		}
	}
	source.Available = source.TextContent != ""
	if source.Kind == "clipboard_image" || source.Kind == "screenshot" {
		data, readErr := os.ReadFile(source.FilePath)
		if readErr != nil {
			source.Available = false
			source.Error = "来源图片不可用: " + readErr.Error()
			return source, nil
		}
		mimeType := source.MIMEType
		if mimeType == "" {
			mimeType = mimeFromExtension(filepath.Ext(source.FilePath))
		}
		source.DataURI = "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data)
		source.Available = true
	}
	if !source.Available && source.Error == "" {
		source.Error = "来源内容不可用"
	}
	return source, nil
}

func mimeFromExtension(ext string) string {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return "image/png"
	}
}

// TodoStats 计算快捷视图汇总数据(角标与里程碑提醒)。
func (s *TodoService) TodoStats() (TodoStats, error) {
	var st TodoStats
	ts := now()
	// 总/待办/完成/里程碑
	if err := s.db.QueryRow("SELECT COUNT(*) FROM todos").Scan(&st.Total); err != nil {
		return st, err
	}
	if err := s.db.QueryRow(
		"SELECT COUNT(*) FROM todos WHERE status != ?", TodoStatusDone).Scan(&st.Pending); err != nil {
		return st, err
	}
	if err := s.db.QueryRow(
		"SELECT COUNT(*) FROM todos WHERE status = ?", TodoStatusDone).Scan(&st.Done); err != nil {
		return st, err
	}
	if err := s.db.QueryRow(
		"SELECT COUNT(*) FROM todos WHERE is_milestone = 1").Scan(&st.Milestones); err != nil {
		return st, err
	}
	// 逾期:有 deadline 且未完成且已过期
	if err := s.db.QueryRow(`
		SELECT COUNT(*) FROM todos
		WHERE deadline > 0 AND deadline < ? AND status != ?`, ts, TodoStatusDone).
		Scan(&st.Overdue); err != nil {
		return st, err
	}
	// 24 小时内到期(不含已逾期)
	if err := s.db.QueryRow(`
		SELECT COUNT(*) FROM todos
		WHERE deadline > 0 AND deadline >= ? AND deadline <= ? AND status != ?`,
		ts, ts+86400, TodoStatusDone).Scan(&st.DueSoon); err != nil {
		return st, err
	}
	return st, nil
}

// ListEvents 返回操作日志;since > 0 时只返回该时间点之后(周报等按周聚合)。
func (s *TodoService) ListEvents(since int64) ([]Event, error) {
	rows, err := s.db.Query("SELECT id, ts, type, summary, ref_id FROM events WHERE ts >= ? ORDER BY ts DESC", since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.TS, &e.Type, &e.Summary, &e.RefID); err != nil {
			return nil, err
		}
		items = append(items, e)
	}
	return items, rows.Err()
}
