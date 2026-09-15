package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

type skillRunRequest struct {
	Skill   string   `json:"skill" jsonschema:"description=Loaded skill name,required"`
	Command string   `json:"command" jsonschema:"description=Cobra subcommand declared by the loaded skill,required"`
	Args    []string `json:"args" jsonschema:"description=Command flags and values as separate arguments"`
}

var skillDependencySvc *SkillDependencyService

type skillRunResponse struct {
	Stdout      string        `json:"stdout"`
	Stderr      string        `json:"stderr"`
	ExitCode    int           `json:"exitCode"`
	OutputPaths []string      `json:"outputPaths,omitempty"`
	Artifacts   []ArtifactRef `json:"artifacts,omitempty"`
}

type skillCommandEnvelope struct {
	OK    bool `json:"ok"`
	Data  any  `json:"data,omitempty"`
	Error any  `json:"error,omitempty"`
}

type skillCommandError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type skillUsageError struct{ err error }

func (e skillUsageError) Error() string { return e.err.Error() }
func (e skillUsageError) Unwrap() error { return e.err }

func usageErrorf(format string, args ...any) error {
	return skillUsageError{err: fmt.Errorf(format, args...)}
}

func newSkillRunTool(t *TodoService, source *todoToolSource, webSearch ...*WebSearchService) tool.Tool {
	var search *WebSearchService
	if len(webSearch) > 0 {
		search = webSearch[0]
	}
	return function.NewFunctionTool(
		func(ctx context.Context, req skillRunRequest) (skillRunResponse, error) {
			return executeSkillCommandWithWebSearch(ctx, t, source, search, req)
		},
		function.WithName("skill_run"),
		function.WithDescription("Execute one command from a loaded Miel skill. Built-in skills use declared Cobra commands; external skills use only their confirmed bundled Node/Python entry. Pass arguments as separate items; shell syntax is not supported."),
	)
}

func executeSkillCommand(ctx context.Context, t *TodoService, source *todoToolSource, req skillRunRequest) (skillRunResponse, error) {
	return executeSkillCommandWithWebSearch(ctx, t, source, nil, req)
}

func executeSkillCommandWithWebSearch(ctx context.Context, t *TodoService, source *todoToolSource, webSearch *WebSearchService, req skillRunRequest) (skillRunResponse, error) {
	skillName := strings.ToLower(strings.TrimSpace(req.Skill))
	commandName := strings.ToLower(strings.TrimSpace(req.Command))
	root, allowed := newBuiltinSkillCommand(skillName, t, source, webSearch)
	if root == nil {
		if skillDependencySvc == nil {
			return failedSkillRun(2, "invalid_skill", "Skill 依赖服务未初始化"), nil
		}
		if commandName != "run" {
			return failedSkillRun(2, "invalid_command", "外部 Skill 仅支持 run 命令"), nil
		}
		// command already carries the external Skill action. Some models still
		// repeat it as the first argv item; never pass that control value through
		// to the declared Python/Node entry.
		args := trimRepeatedExternalSkillCommand(req.Args, commandName)
		output, runErr := skillDependencySvc.ExecuteSkillContext(ctx, skillName, args)
		if runErr != nil {
			return skillRunResponse{}, fmt.Errorf("Skill %s 初始化或执行失败: %w", skillName, runErr)
		}
		skillDir, directoryErr := skillDependencySvc.skillDirectory(skillName)
		if directoryErr != nil {
			return skillRunResponse{}, fmt.Errorf("解析 Skill 输出目录: %w", directoryErr)
		}
		outputs := declaredSkillOutputPaths(output, skillDir)
		if skillName == "watermark" && len(outputs) == 0 {
			return skillRunResponse{}, errors.New("水印 Skill 未产生可验证的输出文件，已中断交付")
		}
		for _, outputPath := range outputs {
			logInfo(ctx, "skill.run.output", "skill", skillName, "output_path", outputPath)
		}
		return skillRunResponse{Stdout: string(output), ExitCode: 0, OutputPaths: outputs}, nil
	}
	if !allowed[commandName] {
		return failedSkillRun(2, "invalid_command", fmt.Sprintf("skill %s 不支持 command %q", skillName, req.Command)), nil
	}
	if skillName == "reminder" && commandName == "settings" {
		if len(req.Args) == 0 || (req.Args[0] != "get" && req.Args[0] != "set") {
			return failedSkillRun(2, "invalid_command", "reminder settings 需要 get 或 set 子命令"), nil
		}
	}
	for _, arg := range req.Args {
		if arg == "--help" || arg == "-h" {
			return failedSkillRun(2, "invalid_arguments", "skill_run 不支持帮助参数；请按已加载的 Skill 文档调用"), nil
		}
	}

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{commandName}, req.Args...))
	root.SilenceErrors = true
	root.SilenceUsage = true
	setSkillFlagErrors(root)
	if err := root.ExecuteContext(ctx); err != nil {
		var usageErr skillUsageError
		if errors.As(err, &usageErr) {
			writeSkillError(&stderr, "invalid_arguments", err.Error())
			return skillRunResponse{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: 2}, nil
		}
		if isSkillBusinessError(err) {
			writeSkillError(&stderr, "command_failed", err.Error())
			return skillRunResponse{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: 1}, nil
		}
		return skillRunResponse{}, fmt.Errorf("execute %s %s: %w", skillName, commandName, err)
	}
	return skillRunResponse{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: 0}, nil
}

func trimRepeatedExternalSkillCommand(args []string, command string) []string {
	if len(args) == 0 || !strings.EqualFold(strings.TrimSpace(args[0]), command) {
		return args
	}
	return append([]string(nil), args[1:]...)
}

// declaredSkillOutputPaths accepts only the documented, line-oriented output
// contract. A successful process alone is insufficient for file-producing
// Skills: the application must be able to verify the exact file it delivers.
// Relative paths are resolved from the declared Skill working directory, never
// the application's process directory.
func declaredSkillOutputPaths(output []byte, skillDir string) []string {
	seen := make(map[string]struct{})
	paths := make([]string, 0)
	for _, line := range strings.Split(string(output), "\n") {
		raw := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "wrote "))
		if raw == "" || !strings.HasPrefix(strings.TrimSpace(line), "wrote ") {
			continue
		}
		path := raw
		wasRelative := !filepath.IsAbs(path)
		if wasRelative {
			path = filepath.Join(skillDir, path)
		}
		path, err := filepath.Abs(path)
		if err != nil {
			continue
		}
		path = filepath.Clean(path)
		if wasRelative && !isWithin(skillDir, path) {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	return paths
}

func isSkillBusinessError(err error) bool {
	return errors.Is(err, sql.ErrNoRows) || errors.Is(err, ErrNotFound)
}

func failedSkillRun(exitCode int, code, message string) skillRunResponse {
	var stderr bytes.Buffer
	writeSkillError(&stderr, code, message)
	return skillRunResponse{Stderr: stderr.String(), ExitCode: exitCode}
}

func writeSkillData(cmd *cobra.Command, data any) error {
	return json.NewEncoder(cmd.OutOrStdout()).Encode(skillCommandEnvelope{OK: true, Data: data})
}

func writeSkillError(out *bytes.Buffer, code, message string) {
	_ = json.NewEncoder(out).Encode(skillCommandEnvelope{
		OK:    false,
		Error: skillCommandError{Code: code, Message: message},
	})
}

func noCommandArgs(cmd *cobra.Command, args []string) error {
	if len(args) != 0 {
		return usageErrorf("%s 不接受位置参数", cmd.CommandPath())
	}
	return nil
}

func setSkillFlagErrors(cmd *cobra.Command) {
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return skillUsageError{err: err}
	})
	for _, child := range cmd.Commands() {
		setSkillFlagErrors(child)
	}
}

func newBuiltinSkillCommand(skillName string, t *TodoService, source *todoToolSource, webSearch ...*WebSearchService) (*cobra.Command, map[string]bool) {
	var search *WebSearchService
	if len(webSearch) > 0 {
		search = webSearch[0]
	}
	switch skillName {
	case "todo":
		return newTodoCommand(t, source), commandSet("add", "list", "get", "status", "delete", "stats", "events")
	case "reminder":
		return newReminderCommand(), commandSet("upcoming", "settings")
	case "office":
		return newOfficeCommand(t), commandSet("weekly", "document", "table", "export-todos")
	case "websearch":
		return newWebSearchCommand(search), commandSet("search", "fetch")
	default:
		return nil, nil
	}
}

func newWebSearchCommand(service *WebSearchService) *cobra.Command {
	root := &cobra.Command{Use: "websearch"}
	var query string
	var limit int
	search := &cobra.Command{
		Use:  "search",
		Args: noCommandArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if service == nil {
				return errors.New("联网搜索服务未初始化")
			}
			if strings.TrimSpace(query) == "" {
				return usageErrorf("--query 不能为空")
			}
			if limit < 1 || limit > 10 {
				return usageErrorf("--limit 必须在 1 到 10 之间")
			}
			response, err := service.Search(cmd.Context(), query, limit)
			if err != nil {
				return err
			}
			return writeSkillData(cmd, response)
		},
	}
	search.Flags().StringVar(&query, "query", "", "搜索关键词")
	search.Flags().IntVar(&limit, "limit", 5, "返回结果数量")
	var rawURL string
	fetch := &cobra.Command{
		Use:  "fetch",
		Args: noCommandArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if service == nil {
				return errors.New("联网搜索服务未初始化")
			}
			if strings.TrimSpace(rawURL) == "" {
				return usageErrorf("--url 不能为空")
			}
			document, err := service.Fetch(cmd.Context(), rawURL)
			if err != nil {
				return err
			}
			return writeSkillData(cmd, document)
		},
	}
	fetch.Flags().StringVar(&rawURL, "url", "", "公开 HTTP(S) 网页地址")
	root.AddCommand(search, fetch)
	return root
}

func commandSet(names ...string) map[string]bool {
	result := make(map[string]bool, len(names))
	for _, name := range names {
		result[name] = true
	}
	return result
}

func newTodoCommand(t *TodoService, source *todoToolSource) *cobra.Command {
	root := &cobra.Command{Use: "todo"}

	var title, description string
	var deadline int64
	var milestone bool
	add := &cobra.Command{
		Use:  "add",
		Args: noCommandArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if t == nil {
				return errors.New("待办服务未初始化")
			}
			if strings.TrimSpace(title) == "" {
				return usageErrorf("--title 不能为空")
			}
			if deadline < 0 {
				return usageErrorf("--deadline 不能小于 0")
			}
			input := TodoInput{Title: title, Description: description, Deadline: deadline, IsMilestone: milestone, Status: TodoStatusPending, Source: "chat"}
			var created Todo
			var err error
			if source != nil {
				created, err = source.create(t, input)
			} else {
				created, err = t.CreateTodo(input)
			}
			if err != nil {
				return err
			}
			return writeSkillData(cmd, created)
		},
	}
	add.Flags().StringVar(&title, "title", "", "todo title")
	add.Flags().StringVar(&description, "description", "", "optional detail")
	add.Flags().Int64Var(&deadline, "deadline", 0, "deadline as unix seconds")
	add.Flags().BoolVar(&milestone, "milestone", false, "mark as milestone")

	var listStatus string
	list := &cobra.Command{
		Use:  "list",
		Args: noCommandArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if t == nil {
				return errors.New("待办服务未初始化")
			}
			if err := validateTodoStatus(listStatus, true); err != nil {
				return err
			}
			items, err := t.ListTodos()
			if err != nil {
				return err
			}
			if listStatus != "" {
				filtered := make([]Todo, 0, len(items))
				for _, item := range items {
					if item.Status == listStatus {
						filtered = append(filtered, item)
					}
				}
				items = filtered
			}
			return writeSkillData(cmd, items)
		},
	}
	list.Flags().StringVar(&listStatus, "status", "", "pending, doing, or done")

	var getID int64
	get := &cobra.Command{
		Use:  "get",
		Args: noCommandArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if t == nil {
				return errors.New("待办服务未初始化")
			}
			if getID <= 0 {
				return usageErrorf("--id 必须大于 0")
			}
			item, err := t.GetTodo(getID)
			if err != nil {
				return err
			}
			return writeSkillData(cmd, item)
		},
	}
	get.Flags().Int64Var(&getID, "id", 0, "todo id")

	var statusID int64
	var statusValue string
	status := &cobra.Command{
		Use:  "status",
		Args: noCommandArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if t == nil {
				return errors.New("待办服务未初始化")
			}
			if statusID <= 0 {
				return usageErrorf("--id 必须大于 0")
			}
			if err := validateTodoStatus(statusValue, false); err != nil {
				return err
			}
			item, err := t.SetTodoStatus(statusID, statusValue)
			if err != nil {
				return err
			}
			return writeSkillData(cmd, item)
		},
	}
	status.Flags().Int64Var(&statusID, "id", 0, "todo id")
	status.Flags().StringVar(&statusValue, "status", "", "pending, doing, or done")

	var deleteID int64
	deleteCommand := &cobra.Command{
		Use:  "delete",
		Args: noCommandArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if t == nil {
				return errors.New("待办服务未初始化")
			}
			if deleteID <= 0 {
				return usageErrorf("--id 必须大于 0")
			}
			if err := t.DeleteTodo(deleteID); err != nil {
				return err
			}
			return writeSkillData(cmd, map[string]any{"deleted": true, "id": deleteID})
		},
	}
	deleteCommand.Flags().Int64Var(&deleteID, "id", 0, "todo id")

	stats := &cobra.Command{
		Use:  "stats",
		Args: noCommandArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if t == nil {
				return errors.New("待办服务未初始化")
			}
			result, err := t.TodoStats()
			if err != nil {
				return err
			}
			return writeSkillData(cmd, result)
		},
	}

	var sinceDays int64
	events := &cobra.Command{
		Use:  "events",
		Args: noCommandArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if t == nil {
				return errors.New("待办服务未初始化")
			}
			if sinceDays <= 0 {
				return usageErrorf("--since-days 必须大于 0")
			}
			items, err := t.ListEvents(now() - sinceDays*24*60*60)
			if err != nil {
				return err
			}
			return writeSkillData(cmd, items)
		},
	}
	events.Flags().Int64Var(&sinceDays, "since-days", 7, "look-back window in days")

	root.AddCommand(add, list, get, status, deleteCommand, stats, events)
	return root
}

func validateTodoStatus(status string, allowEmpty bool) error {
	if allowEmpty && status == "" {
		return nil
	}
	if status != TodoStatusPending && status != TodoStatusDoing && status != TodoStatusDone {
		return usageErrorf("--status 必须是 pending、doing 或 done")
	}
	return nil
}

func newReminderCommand() *cobra.Command {
	root := &cobra.Command{Use: "reminder"}
	var leadHours int64
	upcoming := &cobra.Command{
		Use:  "upcoming",
		Args: noCommandArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if leadHours < 0 {
				return usageErrorf("--lead-hours 不能小于 0")
			}
			result, err := buildReminderList(leadHours)
			if err != nil {
				return err
			}
			return writeSkillData(cmd, map[string]any{"summary": result})
		},
	}
	upcoming.Flags().Int64Var(&leadHours, "lead-hours", 0, "override the configured look-ahead window")

	settingsCommand := &cobra.Command{
		Use:  "settings",
		Args: noCommandArgs,
		RunE: func(*cobra.Command, []string) error {
			return usageErrorf("settings 需要 get 或 set 子命令")
		},
	}
	get := &cobra.Command{
		Use:  "get",
		Args: noCommandArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			settings, err := currentReminderSettings()
			if err != nil {
				return err
			}
			return writeSkillData(cmd, settings)
		},
	}
	var enabledValue string
	var configuredLeadHours int64
	set := &cobra.Command{
		Use:  "set",
		Args: noCommandArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if settingsSvc == nil {
				return errors.New("设置服务未初始化")
			}
			setEnabled := cmd.Flags().Changed("enabled")
			setLeadHours := cmd.Flags().Changed("lead-hours")
			if !setEnabled && !setLeadHours {
				return usageErrorf("至少提供 --enabled 或 --lead-hours")
			}
			if setLeadHours && configuredLeadHours <= 0 {
				return usageErrorf("--lead-hours 必须大于 0")
			}
			if setEnabled {
				enabled, err := strconv.ParseBool(enabledValue)
				if err != nil {
					return usageErrorf("--enabled 必须是 true 或 false")
				}
				value := "0"
				if enabled {
					value = "1"
				}
				if err := settingsSvc.SetSetting(SettingRemindEnabled, value); err != nil {
					return err
				}
			}
			if setLeadHours {
				if err := settingsSvc.SetSetting(SettingRemindLeadHours, strconv.FormatInt(configuredLeadHours, 10)); err != nil {
					return err
				}
			}
			settings, err := currentReminderSettings()
			if err != nil {
				return err
			}
			return writeSkillData(cmd, settings)
		},
	}
	set.Flags().StringVar(&enabledValue, "enabled", "", "true or false")
	set.Flags().Int64Var(&configuredLeadHours, "lead-hours", 0, "remind this many hours before deadline")
	settingsCommand.AddCommand(get, set)
	root.AddCommand(upcoming, settingsCommand)
	return root
}

func currentReminderSettings() (map[string]any, error) {
	if settingsSvc == nil {
		return nil, errors.New("设置服务未初始化")
	}
	enabledValue, err := settingsSvc.GetSetting(SettingRemindEnabled)
	if err != nil {
		return nil, err
	}
	leadValue, err := settingsSvc.GetSetting(SettingRemindLeadHours)
	if err != nil {
		return nil, err
	}
	leadHours := int64(24)
	if leadValue != "" {
		if parsed, parseErr := strconv.ParseInt(leadValue, 10, 64); parseErr == nil && parsed > 0 {
			leadHours = parsed
		}
	}
	return map[string]any{"enabled": enabledValue != "0", "leadHours": leadHours}, nil
}

func newOfficeCommand(t *TodoService) *cobra.Command {
	root := &cobra.Command{Use: "office"}
	var sinceDays int64
	weekly := &cobra.Command{
		Use:  "weekly",
		Args: noCommandArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if t == nil {
				return errors.New("待办服务未初始化")
			}
			if sinceDays <= 0 {
				return usageErrorf("--since-days 必须大于 0")
			}
			markdown, path, artifacts, err := generateWeeklyReport(cmd.Context(), t, sinceDays)
			if err != nil {
				return err
			}
			return writeSkillData(cmd, map[string]any{"path": path, "markdown": markdown, "artifacts": artifacts})
		},
	}
	weekly.Flags().Int64Var(&sinceDays, "since-days", 7, "look-back window in days")

	var documentTitle, documentContent string
	document := &cobra.Command{
		Use:  "document",
		Args: noCommandArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(documentTitle) == "" {
				return usageErrorf("--title 不能为空")
			}
			if strings.TrimSpace(documentContent) == "" {
				return usageErrorf("--content 不能为空")
			}
			name := fmt.Sprintf("%s_%s.md", stamp(), slugify(documentTitle))
			output, err := publishOfficeOutput(cmd.Context(), "documents", name, []byte(documentContent))
			if err != nil {
				return err
			}
			return writeSkillData(cmd, output)
		},
	}
	document.Flags().StringVar(&documentTitle, "title", "", "document title")
	document.Flags().StringVar(&documentContent, "content", "", "markdown body")

	var tableTitle, tableCSV string
	table := &cobra.Command{
		Use:  "table",
		Args: noCommandArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(tableTitle) == "" {
				return usageErrorf("--title 不能为空")
			}
			csvContent := strings.TrimSpace(tableCSV)
			if csvContent == "" {
				return usageErrorf("--csv 不能为空")
			}
			name := fmt.Sprintf("%s_%s.csv", stamp(), slugify(tableTitle))
			output, err := publishOfficeOutput(cmd.Context(), "tables", name, []byte(csvContent+"\n"))
			if err != nil {
				return err
			}
			return writeSkillData(cmd, output)
		},
	}
	table.Flags().StringVar(&tableTitle, "title", "", "table title")
	table.Flags().StringVar(&tableCSV, "csv", "", "CSV content including header")

	var exportFormat string
	export := &cobra.Command{
		Use:  "export-todos",
		Args: noCommandArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if t == nil {
				return errors.New("待办服务未初始化")
			}
			exportFormat = strings.ToLower(strings.TrimSpace(exportFormat))
			if exportFormat != "md" && exportFormat != "csv" {
				return usageErrorf("--format 必须是 md 或 csv")
			}
			items, err := t.ListTodos()
			if err != nil {
				return err
			}
			content := exportTodosMD(items)
			if exportFormat == "csv" {
				content = exportTodosCSV(items)
			}
			name := fmt.Sprintf("%s_todos.%s", stamp(), exportFormat)
			output, err := publishOfficeOutput(cmd.Context(), "tables", name, []byte(content))
			if err != nil {
				return err
			}
			return writeSkillData(cmd, map[string]any{"path": output.Path, "count": len(items), "format": exportFormat, "artifacts": output.Artifacts})
		},
	}
	export.Flags().StringVar(&exportFormat, "format", "md", "md or csv")

	root.AddCommand(weekly, document, table, export)
	return root
}
