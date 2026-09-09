package app

import (
	"database/sql"
	"errors"
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
		SELECT id, title, description, deadline, is_milestone, status, source, created_at, done_at
		FROM todos ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Todo{}
	for rows.Next() {
		var t Todo
		var ms int
		if err := rows.Scan(&t.ID, &t.Title, &t.Description, &t.Deadline, &ms,
			&t.Status, &t.Source, &t.CreatedAt, &t.DoneAt); err != nil {
			return nil, err
		}
		t.IsMilestone = ms != 0
		items = append(items, t)
	}
	return items, rows.Err()
}

// GetTodo 按 ID 读取单个待办。
func (s *TodoService) GetTodo(id int64) (Todo, error) {
	var t Todo
	var ms int
	err := s.db.QueryRow(`
		SELECT id, title, description, deadline, is_milestone, status, source, created_at, done_at
		FROM todos WHERE id = ?`, id).
		Scan(&t.ID, &t.Title, &t.Description, &t.Deadline, &ms,
			&t.Status, &t.Source, &t.CreatedAt, &t.DoneAt)
	if err != nil {
		return t, err
	}
	t.IsMilestone = ms != 0
	return t, nil
}

// CreateTodo 新增待办并写操作日志。
func (s *TodoService) CreateTodo(in TodoInput) (Todo, error) {
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
	res, err := s.db.Exec(`
		INSERT INTO todos (title, description, deadline, is_milestone, status, source, created_at, done_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		title, in.Description, in.Deadline, boolToInt(in.IsMilestone),
		status, source, ts, doneAt)
	if err != nil {
		return Todo{}, err
	}
	id, _ := res.LastInsertId()
	if _, err := insertEvent(s.db, EventTodoCreated, "新增待办:"+title, id); err != nil {
		return Todo{}, err
	}
	t, err := s.GetTodo(id)
	if err == nil {
		s.notify("todos.changed", "created")
	}
	return t, err
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
	if _, err := s.db.Exec("DELETE FROM todos WHERE id = ?", id); err != nil {
		return err
	}
	insertEvent(s.db, EventTodoDeleted, "删除待办:"+old.Title, id) //nolint:errcheck
	s.notify("todos.changed", "deleted")
	return nil
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
