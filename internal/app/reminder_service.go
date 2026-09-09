package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"
)

// ReminderItem 提醒条目(前端提醒中心展示)。
type ReminderItem struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Kind      string `json:"kind"` // dueSoon | overdue
	Deadline  int64  `json:"deadline"`
	CreatedAt int64  `json:"createdAt"`
}

// ReminderService 每分钟检查未完成待办的 deadline:
//   - 逾期(deadline 已过)触发一次 overdue 提醒
//   - 距截止 <= remind.lead.hours 触发一次 dueSoon 提醒
//
// 每个待办同一种提醒只发一次(内存去重,重启后对已过提醒不再重复打扰)。
// 提醒同时以事件 reminders.changed(JSON 字符串)推给前端:
// 前端负责系统通知(Web Notification)与应用内提醒中心/角标 —— 双通道。
type ReminderService struct {
	db          *sql.DB
	notified    map[int64]map[string]bool // todoID -> kind -> sent
	leadHours   int64
	lastEmit    map[int64]string
	lastVersion int64
}

// NewReminderService 构造提醒服务。
func NewReminderService(db *sql.DB) *ReminderService {
	return &ReminderService{db: db, notified: map[int64]map[string]bool{}}
}

// ReminderItemCount 供前端拉取未读提醒数量(角标)。
func (s *ReminderService) ReminderItemCount() int {
	if store == nil {
		return 0
	}
	var n int
	_ = store.QueryRow(
		"SELECT COUNT(*) FROM todos WHERE status != ? AND deadline > 0 AND deadline <= ?",
		TodoStatusDone, now()+int64(s.leadHours)*3600).Scan(&n)
	return n
}

// remindEmit 发送提醒事件;payload 为 JSON 字符串(对应注册的 string 事件)。
func remindEmit(ev map[string]any) {
	b, _ := json.Marshal(ev)
	Emit("reminders.changed", string(b))
}

// Start 启动后台提醒循环(每分钟一次)。
func (s *ReminderService) Start(ctx context.Context) {
	s.reloadConfig()
	go func() {
		// 启动 30 秒后先做一次(避免与启动竞态),随后每分钟
		timer := time.NewTimer(30 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				s.tick()
				timer.Reset(time.Minute)
			}
		}
	}()
}

// reloadConfig 从 settings 读取提醒开关与提前量。
func (s *ReminderService) reloadConfig() {
	if settingsSvc == nil {
		return
	}
	enabled, err := settingsSvc.GetSetting(SettingRemindEnabled)
	if err == nil && enabled == "0" {
		return // 提醒关闭仍保留方法,不启动 tick 逻辑由调用方控制
	}
	lead, err := settingsSvc.GetSetting(SettingRemindLeadHours)
	if err != nil || lead == "" {
		lead = "24"
	}
	s.leadHours, _ = strconv.ParseInt(lead, 10, 64)
	if s.leadHours <= 0 {
		s.leadHours = 24
	}
}

// tick 扫描一次。
func (s *ReminderService) tick() {
	if store == nil {
		return
	}
	s.reloadConfig()
	ts := now()
	rows, err := store.Query(`
		SELECT id, title, deadline FROM todos
		WHERE status != ? AND deadline > 0`, TodoStatusDone)
	if err != nil {
		log.Println("reminder scan failed:", err)
		return
	}
	defer rows.Close()
	dueLead := int64(s.leadHours) * 3600
	for rows.Next() {
		var id int64
		var title string
		var deadline int64
		if err := rows.Scan(&id, &title, &deadline); err != nil {
			continue
		}
		if s.notified[id] == nil {
			s.notified[id] = map[string]bool{}
		}
		switch {
		case deadline <= ts && !s.notified[id]["overdue"]:
			s.notified[id]["overdue"] = true
			remindEmit(map[string]any{
				"type": "overdue", "id": id, "title": title,
				"deadline": deadline, "ts": ts,
				"text": fmt.Sprintf("待办已逾期:「%s」", title),
			})
		case deadline <= ts+dueLead && !s.notified[id]["dueSoon"]:
			s.notified[id]["dueSoon"] = true
			remindEmit(map[string]any{
				"type": "dueSoon", "id": id, "title": title,
				"deadline": deadline, "ts": ts,
				"text": fmt.Sprintf("待办即将到期:「%s」", title),
			})
		}
	}
}

// ResetTodoReminder 在待办状态变化(如完成/延期)后调用,允许再次提醒。
func (s *ReminderService) ResetTodoReminder(todoID int64) {
	delete(s.notified, todoID)
}
