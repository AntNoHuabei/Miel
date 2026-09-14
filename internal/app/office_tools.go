package app

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// weeklyAgg 周报聚合结果。
type weeklyAgg struct {
	Days    int64
	From    string
	To      string
	Done    []Todo
	Added   []Todo
	Overdue []Todo
	Open    []Todo
	Events  []Event
}

// aggregateWeekly 从待办与事件日志聚合一周数据。
func aggregateWeekly(t *TodoService, days int64) (*weeklyAgg, error) {
	if days <= 0 {
		days = 7
	}
	cut := time.Now().Add(-time.Duration(days) * 24 * time.Hour).Unix()
	items, err := t.ListTodos()
	if err != nil {
		return nil, err
	}
	evs, err := t.ListEvents(cut)
	if err != nil {
		return nil, err
	}
	agg := &weeklyAgg{
		Days: days,
		From: time.Unix(cut, 0).Format("2006-01-02"),
		To:   time.Now().Format("2006-01-02"),
	}
	now := time.Now().Unix()
	for _, it := range items {
		switch {
		case it.Status == TodoStatusDone && it.DoneAt >= cut:
			agg.Done = append(agg.Done, it)
		case it.CreatedAt >= cut:
			agg.Added = append(agg.Added, it)
		}
		if it.Status != TodoStatusDone {
			if it.Deadline > 0 && it.Deadline < now {
				agg.Overdue = append(agg.Overdue, it)
			} else {
				agg.Open = append(agg.Open, it)
			}
		}
	}
	agg.Events = evs
	sortTodosBy(agg.Done, func(a, b Todo) bool { return a.DoneAt > b.DoneAt })
	sortTodosBy(agg.Added, func(a, b Todo) bool { return a.CreatedAt > b.CreatedAt })
	return agg, nil
}

func sortTodosBy(s []Todo, less func(a, b Todo) bool) {
	sort.SliceStable(s, func(i, j int) bool { return less(s[i], s[j]) })
}

// generateWeeklyReport 聚合并渲染周报(markdown),同时落盘 outputs/reports/。
func generateWeeklyReport(t *TodoService, days int64) (string, string, error) {
	agg, err := aggregateWeekly(t, days)
	if err != nil {
		return "", "", err
	}
	md := renderWeeklyReport(agg)
	name := fmt.Sprintf("weekly_report_%s_%s.md", agg.From, agg.To)
	path, err := writeOutput("reports", name, []byte(md))
	if err != nil {
		return "", "", err
	}
	if _, err := insertEvent(store, EventReportGenerated,
		fmt.Sprintf("生成周报 %s~%s", agg.From, agg.To), 0); err != nil {
		return "", "", err
	}
	return md, path, nil
}

// renderWeeklyReport 渲染周报 markdown。
func renderWeeklyReport(a *weeklyAgg) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# 工作周报(%s ~ %s)\n\n", a.From, a.To)
	fmt.Fprintf(&b, "> 由 Miel 依据本地待办与操作日志自动汇总(统计窗口最近 %d 天)。\n\n", a.Days)

	fmt.Fprintf(&b, "## 一、本周完成(%d)\n", len(a.Done))
	if len(a.Done) == 0 {
		b.WriteString("(无)\n")
	}
	for _, it := range a.Done {
		fmt.Fprintf(&b, "- [x] %s(完成于 %s)\n", it.Title, dayFmt(it.DoneAt))
	}

	fmt.Fprintf(&b, "\n## 二、本周新增(%d)\n", len(a.Added))
	if len(a.Added) == 0 {
		b.WriteString("(无)\n")
	}
	for _, it := range a.Added {
		fmt.Fprintf(&b, "- %s%s(来源:%s%s)\n", it.Title, msMark(it),
			sourceLabel(it.Source), deadlineSuffix(it))
	}

	openMs := 0
	for _, it := range a.Open {
		if it.IsMilestone {
			openMs++
		}
	}
	fmt.Fprintf(&b, "\n## 三、里程碑进度\n")
	if openMs == 0 {
		b.WriteString("(当前无进行中的里程碑)\n")
	}
	for _, it := range a.Open {
		if it.IsMilestone {
			fmt.Fprintf(&b, "- 🚩 %s%s\n", it.Title, deadlineSuffix(it))
		}
	}

	fmt.Fprintf(&b, "\n## 四、进行中待跟进(%d)\n", len(a.Open))
	if len(a.Open) == 0 {
		b.WriteString("(无)\n")
	}
	for _, it := range a.Open {
		if !it.IsMilestone {
			fmt.Fprintf(&b, "- %s%s\n", it.Title, deadlineSuffix(it))
		}
	}

	fmt.Fprintf(&b, "\n## 五、逾期未完成(%d)\n", len(a.Overdue))
	if len(a.Overdue) == 0 {
		b.WriteString("(无)\n")
	}
	for _, it := range a.Overdue {
		fmt.Fprintf(&b, "- ⚠️ %s(截止 %s,已逾期)\n", it.Title, dayFmt(it.Deadline))
	}

	fmt.Fprintf(&b, "\n## 六、操作流水(%d 条)\n", len(a.Events))
	if len(a.Events) == 0 {
		b.WriteString("(无)\n")
	}
	for _, e := range a.Events {
		fmt.Fprintf(&b, "- %s [%s] %s\n", timeFmt(e.TS), e.Type, e.Summary)
	}
	b.WriteString("\n---\n*由 Miel 自动生成*\n")
	return b.String()
}

func msMark(it Todo) string {
	if it.IsMilestone {
		return " 🚩"
	}
	return ""
}

func sourceLabel(s string) string {
	switch s {
	case "chat":
		return "对话"
	case "screenshot":
		return "截图"
	default:
		return "手动"
	}
}

func deadlineSuffix(it Todo) string {
	if it.Deadline <= 0 {
		return ""
	}
	return fmt.Sprintf(",截止 %s", dayFmt(it.Deadline))
}

func dayFmt(ts int64) string {
	if ts <= 0 {
		return "-"
	}
	return time.Unix(ts, 0).Format("01-02")
}

func timeFmt(ts int64) string {
	if ts <= 0 {
		return "-"
	}
	return time.Unix(ts, 0).Format("01-02 15:04")
}

// exportTodosMD 待办导出为 markdown。
func exportTodosMD(items []Todo) string {
	var b strings.Builder
	b.WriteString("# 待办清单\n\n")
	if len(items) == 0 {
		b.WriteString("(当前没有待办)\n")
		return b.String()
	}
	fmt.Fprintf(&b, "共 %d 条\n\n", len(items))
	for _, it := range items {
		status := map[string]string{
			TodoStatusPending: "待办",
			TodoStatusDoing:   "进行中",
			TodoStatusDone:    "已完成",
		}[it.Status]
		fmt.Fprintf(&b, "- [%s] %s%s(状态:%s,来源:%s%s)\n",
			statusMark(it.Status), it.Title, msMark(it), status,
			sourceLabel(it.Source), deadlineSuffix(it))
	}
	return b.String()
}

func statusMark(s string) string {
	if s == TodoStatusDone {
		return "x"
	}
	return " "
}

// exportTodosCSV 待办导出为 CSV(带 BOM,便于 Excel 打开中文)。
func exportTodosCSV(items []Todo) string {
	var b strings.Builder
	b.WriteString("\uFEFFid,title,description,status,milestone,deadline,source,created_at,done_at\n")
	cell := func(s string) string {
		if strings.ContainsAny(s, ",\"\n") {
			return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
		}
		return s
	}
	for _, it := range items {
		fmt.Fprintf(&b, "%d,%s,%s,%s,%t,%d,%s,%d,%d\n",
			it.ID, cell(it.Title), cell(it.Description), it.Status,
			it.IsMilestone, it.Deadline, cell(it.Source), it.CreatedAt, it.DoneAt)
	}
	return b.String()
}
