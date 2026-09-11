package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// todoAgentTools 把待办/统计/日志能力包装为 function-calling 工具,供 Agent 调用。

type todoToolSource struct {
	mu       sync.Mutex
	input    todoSourceInput
	sourceID int64
}

func (s *todoToolSource) create(t *TodoService, input TodoInput) (Todo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sourceID > 0 {
		input.SourceID = s.sourceID
		return t.CreateTodo(input)
	}
	created, err := t.createTodo(input, &s.input)
	if err == nil {
		s.sourceID = created.SourceID
	}
	return created, err
}

func todoAgentTools(t *TodoService, sourceContext ...*todoToolSource) []tool.Tool {
	var source *todoToolSource
	if len(sourceContext) > 0 {
		source = sourceContext[0]
	}
	return []tool.Tool{
		function.NewFunctionTool(
			func(ctx context.Context, req toolCreateTodoReq) (Todo, error) {
				input := TodoInput{
					Title:       req.Title,
					Description: req.Description,
					Deadline:    req.Deadline,
					IsMilestone: req.Milestone,
					Status:      TodoStatusPending,
					Source:      "chat",
				}
				if source != nil {
					return source.create(t, input)
				}
				return t.CreateTodo(input)
			},
			function.WithName("create_todo"),
			function.WithDescription("Create a todo or milestone for the user. Ask for a deadline date if the user mentions one. Returns the saved todo."),
		),
		function.NewFunctionTool(
			func(ctx context.Context, req toolListTodosReq) (string, error) {
				items, err := t.ListTodos()
				if err != nil {
					return "", err
				}
				if len(items) == 0 {
					return "当前没有任何待办。", nil
				}
				var b strings.Builder
				fmt.Fprintf(&b, "共 %d 条待办(格式: id | 状态 | 标题 | 截止 | 里程碑):\n", len(items))
				for _, it := range items {
					deadline := "-"
					if it.Deadline > 0 {
						deadline = time.Unix(it.Deadline, 0).Format("2006-01-02 15:04")
					}
					ms := ""
					if it.IsMilestone {
						ms = " [里程碑]"
					}
					fmt.Fprintf(&b, "- %d | %s | %s | %s%s\n", it.ID, it.Status, it.Title, deadline, ms)
				}
				return b.String(), nil
			},
			function.WithName("list_todos"),
			function.WithDescription("List all todos and milestones with their id, status, title and deadline."),
		),
		function.NewFunctionTool(
			func(ctx context.Context, req toolTodoStatusReq) (Todo, error) {
				return t.SetTodoStatus(req.ID, req.Status)
			},
			function.WithName("set_todo_status"),
			function.WithDescription("Change a todo status. status is one of pending, doing, done."),
		),
		function.NewFunctionTool(
			func(ctx context.Context, req toolDeleteTodoReq) (string, error) {
				if err := t.DeleteTodo(req.ID); err != nil {
					return "", err
				}
				return "已删除待办 #" + fmt.Sprint(req.ID), nil
			},
			function.WithName("delete_todo"),
			function.WithDescription("Delete a todo by id."),
		),
		function.NewFunctionTool(
			func(ctx context.Context, _ toolNoArgs) (TodoStats, error) {
				return t.TodoStats()
			},
			function.WithName("todo_stats"),
			function.WithDescription("Get todo statistics: total, pending, done, milestones, overdue and due-soon counts."),
		),
		function.NewFunctionTool(
			func(ctx context.Context, req toolEventsReq) (string, error) {
				since := req.SinceDays
				if since <= 0 {
					since = 7
				}
				cut := time.Now().Add(-time.Duration(since) * 24 * time.Hour).Unix()
				items, err := t.ListEvents(cut)
				if err != nil {
					return "", err
				}
				if len(items) == 0 {
					return fmt.Sprintf("最近 %d 天没有任何操作记录。", since), nil
				}
				var b strings.Builder
				fmt.Fprintf(&b, "最近 %d 天操作记录(用于周报):\n", since)
				for _, e := range items {
					ts := time.Unix(e.TS, 0).Format("2006-01-02 15:04")
					fmt.Fprintf(&b, "- %s [%s] %s\n", ts, e.Type, e.Summary)
				}
				return b.String(), nil
			},
			function.WithName("list_events"),
			function.WithDescription("List recent operation events such as created or completed todos. Useful to generate a weekly report. sinceDays defaults to 7."),
		),
	}
}

// 工具入参结构(jsonschema tag 用于生成给模型的参数描述;注意描述内不使用逗号)。
type toolCreateTodoReq struct {
	Title       string `json:"title" jsonschema:"description=Todo title,required"`
	Description string `json:"description" jsonschema:"description=Optional extra detail"`
	Deadline    int64  `json:"deadline" jsonschema:"description=Deadline as unix seconds or 0 when none"`
	Milestone   bool   `json:"milestone" jsonschema:"description=Mark as a milestone"`
}

type toolListTodosReq struct {
	Status string `json:"status" jsonschema:"description=Optional filter: pending doing done or empty for all"`
}

type toolTodoStatusReq struct {
	ID     int64  `json:"id" jsonschema:"description=Todo id,required"`
	Status string `json:"status" jsonschema:"description=New status,enum=pending,enum=doing,enum=done,required"`
}

type toolDeleteTodoReq struct {
	ID int64 `json:"id" jsonschema:"description=Todo id,required"`
}

type toolEventsReq struct {
	SinceDays int64 `json:"sinceDays" jsonschema:"description=Look back window in days,default 7"`
}

type toolNoArgs struct{}

// ---- reminder subcommand 工具(reminder skill)----

// reminderAgentTools 提醒相关的 subcommand:
//   - reminder_upcoming:逾期 + 提前量内即将到期(真实数据)
//   - reminder_settings:提醒开关 / 提前小时数
func reminderAgentTools() []tool.Tool {
	return []tool.Tool{
		function.NewFunctionTool(
			func(ctx context.Context, _ toolNoArgs) (string, error) {
				return buildReminderList(0)
			},
			function.WithName("reminder_upcoming"),
			function.WithDescription("List overdue and soon-due todos based on real data. Subcommand of the reminder skill. Call when the user asks what is due or overdue."),
		),
		function.NewFunctionTool(
			func(ctx context.Context, req toolReminderSettingsReq) (string, error) {
				if req.Enabled != nil {
					v := "0"
					if *req.Enabled {
						v = "1"
					}
					if err := settingsSvc.SetSetting(SettingRemindEnabled, v); err != nil {
						return "", err
					}
				}
				if req.LeadHours > 0 {
					if err := settingsSvc.SetSetting(SettingRemindLeadHours, strconv.FormatInt(req.LeadHours, 10)); err != nil {
						return "", err
					}
				}
				enabled, _ := settingsSvc.GetSetting(SettingRemindEnabled)
				lead, _ := settingsSvc.GetSetting(SettingRemindLeadHours)
				if lead == "" {
					lead = "24"
				}
				onOff := "关闭"
				if enabled == "1" || enabled == "" {
					onOff = "开启"
				}
				return "提醒:" + onOff + " · 提前 " + lead + " 小时", nil
			},
			function.WithName("reminder_settings"),
			function.WithDescription("Read or update reminder settings. enabled on/off, leadHours in hours (default 24). Subcommand of the reminder skill."),
		),
	}
}

type toolReminderSettingsReq struct {
	Enabled   *bool `json:"enabled" jsonschema:"description=Turn reminders on or off"`
	LeadHours int64 `json:"leadHours" jsonschema:"description=Remind this many hours before the deadline"`
}

// buildReminderList 汇总逾期与即将到期(提前量内)的待办文本。
func buildReminderList(leadHours int64) (string, error) {
	if store == nil || todoSvc == nil {
		return "", errors.New("服务未初始化")
	}
	if leadHours <= 0 {
		if v, _ := settingsSvc.GetSetting(SettingRemindLeadHours); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				leadHours = n
			}
		}
		if leadHours <= 0 {
			leadHours = 24
		}
	}
	items, err := todoSvc.ListTodos()
	if err != nil {
		return "", err
	}
	nowTs := now()
	var b strings.Builder
	b.WriteString("待办到期提醒(提前 " + strconv.FormatInt(leadHours, 10) + " 小时):\n")
	cut := nowTs + leadHours*3600
	n := 0
	for _, it := range items {
		if it.Status == TodoStatusDone || it.Deadline <= 0 {
			continue
		}
		if it.Deadline < nowTs {
			b.WriteString(fmt.Sprintf("- ⚠️ %s(id=%d):已逾期\n", it.Title, it.ID))
			n++
		} else if it.Deadline <= cut {
			left := (it.Deadline - nowTs) / 60
			b.WriteString(fmt.Sprintf("- ⏰ %s(id=%d):%d 分钟后到期\n", it.Title, it.ID, left))
			n++
		}
	}
	if n == 0 {
		return "未来 " + strconv.FormatInt(leadHours, 10) + " 小时内没有到期待办,也没有逾期项。", nil
	}
	return b.String(), nil
}
