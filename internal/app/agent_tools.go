package app

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// todoToolSource keeps all todos created in one agent turn linked to the same
// immutable conversation source.
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

// buildReminderList summarizes overdue and soon-due todos using current data.
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
	cut := nowTs + leadHours*3600
	var overdue, upcoming []string
	for _, it := range items {
		if it.Status == TodoStatusDone || it.Deadline <= 0 {
			continue
		}
		if it.Deadline < nowTs {
			overdue = append(overdue, fmt.Sprintf("#%d %s(逾期 %s)", it.ID, it.Title,
				time.Unix(it.Deadline, 0).Format("2006-01-02 15:04")))
		} else if it.Deadline <= cut {
			left := (it.Deadline - nowTs) / 60
			upcoming = append(upcoming, fmt.Sprintf("#%d %s(%d 分钟后到期)", it.ID, it.Title, left))
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "逾期 %d 条,未来 %d 小时内到期 %d 条。", len(overdue), leadHours, len(upcoming))
	if len(overdue) > 0 {
		b.WriteString("\n逾期:\n- " + strings.Join(overdue, "\n- "))
	}
	if len(upcoming) > 0 {
		b.WriteString("\n即将到期:\n- " + strings.Join(upcoming, "\n- "))
	}
	return b.String(), nil
}
