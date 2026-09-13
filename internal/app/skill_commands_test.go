package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/tool"
)

func setupSkillCommandTest(t *testing.T) *TodoService {
	t.Helper()
	db := newTodoTestDB(t)
	oldStore, oldTodo, oldSettings, oldDirectories := store, todoSvc, settingsSvc, appDirectories
	store = db
	todoSvc = NewTodoService(db)
	settingsSvc = NewSettingsService(db)
	appDirectories = NewDirectoryManager(t.TempDir())
	t.Cleanup(func() {
		store, todoSvc, settingsSvc, appDirectories = oldStore, oldTodo, oldSettings, oldDirectories
	})
	return todoSvc
}

func skillData[T any](t *testing.T, result skillRunResponse) T {
	t.Helper()
	if result.ExitCode != 0 {
		t.Fatalf("skill command failed: %#v", result)
	}
	var envelope struct {
		OK   bool            `json:"ok"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &envelope); err != nil {
		t.Fatalf("decode stdout %q: %v", result.Stdout, err)
	}
	if !envelope.OK {
		t.Fatalf("command result is not ok: %s", result.Stdout)
	}
	var data T
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatalf("decode data %s: %v", envelope.Data, err)
	}
	return data
}

func runSkill(t *testing.T, service *TodoService, source *todoToolSource, request skillRunRequest) skillRunResponse {
	t.Helper()
	result, err := executeSkillCommand(context.Background(), service, source, request)
	if err != nil {
		t.Fatalf("skill infrastructure error: %v", err)
	}
	return result
}

func TestTodoSkillCommandsCoverLifecycleAndConversationSource(t *testing.T) {
	service := setupSkillCommandTest(t)
	var notifications []string
	service.Notify = func(name string, _ any) {
		notifications = append(notifications, name)
	}
	if _, err := store.Exec("INSERT INTO conversations (id, title, created_at, updated_at) VALUES (7, '计划讨论', 1, 1)"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Exec("INSERT INTO messages (id, conversation_id, role, content, created_at) VALUES (9, 7, 'user', '记得提交报告', 1)"); err != nil {
		t.Fatal(err)
	}
	source := &todoToolSource{input: todoSourceInput{
		Kind: "conversation", TextContent: "记得提交报告", ConversationID: 7, MessageID: 9,
	}}

	created := skillData[Todo](t, runSkill(t, service, source, skillRunRequest{
		Skill: "todo", Command: "add", Args: []string{"--title", "提交报告", "--description", "检查附件", "--deadline", "1789401600", "--milestone"},
	}))
	if created.Title != "提交报告" || !created.IsMilestone || created.Deadline != 1789401600 || created.SourceID == 0 {
		t.Fatalf("unexpected created todo: %#v", created)
	}
	detail, err := service.GetTodoSource(created.SourceID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.ConversationID != 7 || detail.MessageID != 9 || detail.TextContent != "记得提交报告" {
		t.Fatalf("unexpected source: %#v", detail)
	}

	got := skillData[Todo](t, runSkill(t, service, source, skillRunRequest{
		Skill: "todo", Command: "get", Args: []string{"--id", strconvID(created.ID)},
	}))
	if got.ID != created.ID {
		t.Fatalf("get returned todo %d, want %d", got.ID, created.ID)
	}
	updated := skillData[Todo](t, runSkill(t, service, source, skillRunRequest{
		Skill: "todo", Command: "status", Args: []string{"--id", strconvID(created.ID), "--status", "done"},
	}))
	if updated.Status != TodoStatusDone {
		t.Fatalf("status = %q, want done", updated.Status)
	}
	items := skillData[[]Todo](t, runSkill(t, service, source, skillRunRequest{
		Skill: "todo", Command: "list", Args: []string{"--status", "done"},
	}))
	if len(items) != 1 || items[0].ID != created.ID {
		t.Fatalf("filtered todos = %#v", items)
	}
	stats := skillData[TodoStats](t, runSkill(t, service, source, skillRunRequest{Skill: "todo", Command: "stats"}))
	if stats.Total != 1 || stats.Done != 1 {
		t.Fatalf("stats = %#v", stats)
	}
	events := skillData[[]Event](t, runSkill(t, service, source, skillRunRequest{Skill: "todo", Command: "events"}))
	if len(events) < 2 {
		t.Fatalf("events = %#v", events)
	}
	skillData[map[string]any](t, runSkill(t, service, source, skillRunRequest{
		Skill: "todo", Command: "delete", Args: []string{"--id", strconvID(created.ID)},
	}))
	if _, err := service.GetTodo(created.ID); err == nil {
		t.Fatal("todo still exists after delete")
	}
	for _, expected := range []string{"todos.changed", "todo.source.changed"} {
		if !containsString(notifications, expected) {
			t.Errorf("notification %q is absent: %#v", expected, notifications)
		}
	}
}

func TestReminderSkillCommandsReadAndUpdateSettings(t *testing.T) {
	service := setupSkillCommandTest(t)
	result := runSkill(t, service, nil, skillRunRequest{
		Skill: "reminder", Command: "settings", Args: []string{"set", "--enabled", "false", "--lead-hours", "12"},
	})
	settings := skillData[map[string]any](t, result)
	if settings["enabled"] != false || settings["leadHours"] != float64(12) {
		t.Fatalf("settings = %#v", settings)
	}
	read := skillData[map[string]any](t, runSkill(t, service, nil, skillRunRequest{
		Skill: "reminder", Command: "settings", Args: []string{"get"},
	}))
	if read["enabled"] != false || read["leadHours"] != float64(12) {
		t.Fatalf("read settings = %#v", read)
	}
	summary := skillData[map[string]any](t, runSkill(t, service, nil, skillRunRequest{
		Skill: "reminder", Command: "upcoming", Args: []string{"--lead-hours", "24"},
	}))
	if _, ok := summary["summary"].(string); !ok {
		t.Fatalf("upcoming result = %#v", summary)
	}
}

func TestOfficeSkillCommandsCreateArtifacts(t *testing.T) {
	service := setupSkillCommandTest(t)
	commands := []struct {
		request     skillRunRequest
		wantContent string
	}{
		{request: skillRunRequest{Skill: "office", Command: "document", Args: []string{"--title", "会议纪要", "--content", "# 会议纪要\n\n正文"}}, wantContent: "# 会议纪要"},
		{request: skillRunRequest{Skill: "office", Command: "table", Args: []string{"--title", "排期", "--csv", "name,date\n方案,2026-09-14"}}, wantContent: "方案,2026-09-14"},
		{request: skillRunRequest{Skill: "office", Command: "export-todos", Args: []string{"--format", "md"}}, wantContent: "# 待办清单"},
		{request: skillRunRequest{Skill: "office", Command: "weekly", Args: []string{"--since-days", "7"}}, wantContent: "# 工作周报"},
	}
	for _, test := range commands {
		data := skillData[map[string]any](t, runSkill(t, service, nil, test.request))
		path, ok := data["path"].(string)
		if !ok || path == "" {
			t.Fatalf("%s result = %#v", test.request.Command, data)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s artifact %q: %v", test.request.Command, path, err)
		}
		if !strings.Contains(string(content), test.wantContent) {
			t.Fatalf("%s artifact %q does not contain %q: %s", test.request.Command, path, test.wantContent, content)
		}
	}
}

func TestSkillRunRejectsUnknownOrInvalidCommandsWithoutGoError(t *testing.T) {
	service := setupSkillCommandTest(t)
	tests := []struct {
		name string
		req  skillRunRequest
	}{
		{name: "unknown skill", req: skillRunRequest{Skill: "shell", Command: "exec", Args: []string{"cmd.exe"}}},
		{name: "shell command", req: skillRunRequest{Skill: "todo", Command: "list;whoami"}},
		{name: "shell positional args", req: skillRunRequest{Skill: "todo", Command: "stats", Args: []string{";", "whoami"}}},
		{name: "unknown command", req: skillRunRequest{Skill: "office", Command: "powershell"}},
		{name: "mismatched command", req: skillRunRequest{Skill: "todo", Command: "weekly"}},
		{name: "unknown nested command", req: skillRunRequest{Skill: "reminder", Command: "settings", Args: []string{"exec"}}},
		{name: "missing flag", req: skillRunRequest{Skill: "todo", Command: "add"}},
		{name: "invalid enum", req: skillRunRequest{Skill: "todo", Command: "list", Args: []string{"--status", "blocked"}}},
		{name: "unknown flag", req: skillRunRequest{Skill: "todo", Command: "stats", Args: []string{"--exec", "whoami"}}},
		{name: "cobra help flag", req: skillRunRequest{Skill: "todo", Command: "list", Args: []string{"--help"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := runSkill(t, service, nil, tt.req)
			if result.ExitCode != 2 || result.Stdout != "" || !strings.Contains(result.Stderr, `"ok":false`) {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func TestSkillRunToolHasCompactRestrictedSchemaAndReturnsCommandErrors(t *testing.T) {
	service := setupSkillCommandTest(t)
	agentTool := newSkillRunTool(service, nil)
	declaration := agentTool.Declaration()
	if declaration.Name != "skill_run" {
		t.Fatalf("tool name = %q", declaration.Name)
	}
	if len(declaration.InputSchema.Properties) != 3 {
		t.Fatalf("input properties = %#v", declaration.InputSchema.Properties)
	}
	for _, name := range []string{"skill", "command", "args"} {
		if declaration.InputSchema.Properties[name] == nil {
			t.Errorf("input property %s is absent", name)
		}
	}
	callable, ok := agentTool.(tool.CallableTool)
	if !ok {
		t.Fatal("skill_run is not callable")
	}
	result, err := callable.Call(context.Background(), []byte(`{"skill":"todo","command":"add","args":[]}`))
	if err != nil {
		t.Fatalf("correctable command error escaped as Go error: %v", err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"exitCode":2`) {
		t.Fatalf("tool result = %s", encoded)
	}

	infrastructureTool := newSkillRunTool(nil, nil)
	infrastructureCallable, ok := infrastructureTool.(tool.CallableTool)
	if !ok {
		t.Fatal("skill_run infrastructure test tool is not callable")
	}
	_, err = infrastructureCallable.Call(context.Background(), []byte(`{"skill":"todo","command":"list","args":[]}`))
	if err == nil || !strings.Contains(err.Error(), "待办服务未初始化") {
		t.Fatalf("tool infrastructure error = %v", err)
	}
}

func TestSkillRunSeparatesBusinessAndInfrastructureErrors(t *testing.T) {
	service := setupSkillCommandTest(t)
	result, err := executeSkillCommand(context.Background(), service, nil, skillRunRequest{
		Skill: "todo", Command: "get", Args: []string{"--id", "999"},
	})
	if err != nil {
		t.Fatalf("business error escaped as Go error: %v", err)
	}
	if result.ExitCode != 1 || result.Stdout != "" || !strings.Contains(result.Stderr, `"code":"command_failed"`) {
		t.Fatalf("business error result = %#v", result)
	}

	_, err = executeSkillCommand(context.Background(), nil, nil, skillRunRequest{
		Skill: "todo", Command: "list",
	})
	if err == nil || !strings.Contains(err.Error(), "待办服务未初始化") {
		t.Fatalf("infrastructure error = %v", err)
	}
}

func TestBuiltinSkillsOverwriteStaleInstalledCopies(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"todo", "reminder", "office"} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("stale custom content"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ensureBuiltinSkills(root)
	for _, name := range []string{"todo", "reminder", "office"} {
		content, err := os.ReadFile(filepath.Join(root, name, "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(content), "skill_run") || strings.Contains(string(content), "stale custom content") {
			t.Fatalf("%s was not upgraded: %s", name, content)
		}
	}
}

func strconvID(id int64) string {
	return strconv.FormatInt(id, 10)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
