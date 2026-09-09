package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// officeTools 把办公生成能力(周报/文档/表格/导出待办)包装为 function-calling
// 工具,供 Agent 调用;产物统一落到应用数据目录 outputs/ 下,前端可在设置里打开。
func officeTools(t *TodoService) []tool.Tool {
	return []tool.Tool{
		function.NewFunctionTool(
			func(ctx context.Context, req toolReportReq) (string, error) {
				if t == nil {
					return "", fmt.Errorf("待办服务未初始化")
				}
				days := req.SinceDays
				if days <= 0 {
					days = 7
				}
				md, path, err := generateWeeklyReport(t, days)
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("已生成周报并保存到:\n%s\n\n%s", path, md), nil
			},
			function.WithName("generate_weekly_report"),
			function.WithDescription("Aggregate todos milestones and activity logs of the last N days into a weekly work report. Saves a markdown file and returns it. Call when the user asks for a weekly report or summary. sinceDays defaults to 7."),
		),
		function.NewFunctionTool(
			func(ctx context.Context, req toolDocReq) (string, error) {
				title := strings.TrimSpace(req.Title)
				if title == "" {
					return "", fmt.Errorf("文档标题不能为空")
				}
				name := fmt.Sprintf("%s_%s.md", stamp(), slugify(title))
				path, err := writeOutput("documents", name, []byte(req.Content))
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("文档已保存:\n%s\n(可在设置中点击“打开数据目录”查看)", path), nil
			},
			function.WithName("create_document"),
			function.WithDescription("Save a document as a markdown file. title is the file name and content is the markdown body. Use for meeting notes summaries drafts and reports. Returns the saved file path."),
		),
		function.NewFunctionTool(
			func(ctx context.Context, req toolTableReq) (string, error) {
				title := strings.TrimSpace(req.Title)
				if title == "" {
					return "", fmt.Errorf("表格标题不能为空")
				}
				csv := strings.TrimSpace(req.CSV)
				if csv == "" {
					return "", fmt.Errorf("表格内容不能为空(请提供 CSV,首行为表头)")
				}
				name := fmt.Sprintf("%s_%s.csv", stamp(), slugify(title))
				path, err := writeOutput("tables", name, []byte(csv+"\n"))
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("表格已保存:\n%s\n(可在设置中点击“打开数据目录”查看)", path), nil
			},
			function.WithName("create_table"),
			function.WithDescription("Save tabular data as a CSV file. Provide title and csv where the first line is the header. Use for schedules plans budgets and structured lists. Returns the saved file path."),
		),
		function.NewFunctionTool(
			func(ctx context.Context, req toolExportReq) (string, error) {
				if t == nil {
					return "", fmt.Errorf("待办服务未初始化")
				}
				format := strings.ToLower(strings.TrimSpace(req.Format))
				if format != "csv" && format != "md" {
					format = "md"
				}
				items, err := t.ListTodos()
				if err != nil {
					return "", err
				}
				var content string
				var ext string
				if format == "csv" {
					ext = "csv"
					content = exportTodosCSV(items)
				} else {
					ext = "md"
					content = exportTodosMD(items)
				}
				name := fmt.Sprintf("%s_todos.%s", stamp(), ext)
				path, err := writeOutput("tables", name, []byte(content))
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("待办已导出(%d 条):\n%s", len(items), path), nil
			},
			function.WithName("export_todos"),
			function.WithDescription("Export all todos to a file. format is md or csv, defaults to md. Returns the saved file path."),
		),
	}
}

// 工具入参结构(jsonschema tag 生成给模型的参数描述)。
type toolReportReq struct {
	SinceDays int64 `json:"sinceDays" jsonschema:"description=Look back window in days,default 7"`
}

type toolDocReq struct {
	Title   string `json:"title" jsonschema:"description=Document title used as file name,required"`
	Content string `json:"content" jsonschema:"description=Markdown body of the document,required"`
}

type toolTableReq struct {
	Title string `json:"title" jsonschema:"description=Table title used as file name,required"`
	CSV   string `json:"csv" jsonschema:"description=CSV text,first line is header,required"`
}

type toolExportReq struct {
	Format string `json:"format" jsonschema:"description=Export format md or csv,default md"`
}

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
	fmt.Fprintf(&b, "> 由 BlankMind 依据本地待办与操作日志自动汇总(统计窗口最近 %d 天)。\n\n", a.Days)

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
	b.WriteString("\n---\n*由 BlankMind 自动生成*\n")
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
