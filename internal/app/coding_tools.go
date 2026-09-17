package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

type codeReadInput struct {
	Path      string `json:"path"`
	StartLine int    `json:"startLine,omitempty"`
	EndLine   int    `json:"endLine,omitempty"`
}

type codeSearchInput struct {
	Query         string   `json:"query"`
	Path          string   `json:"path,omitempty"`
	Regex         bool     `json:"regex,omitempty"`
	CaseSensitive bool     `json:"caseSensitive,omitempty"`
	Include       []string `json:"include,omitempty"`
	Exclude       []string `json:"exclude,omitempty"`
	MaxResults    int      `json:"maxResults,omitempty"`
}

type applyPatchInput struct {
	Path           string `json:"path"`
	Patch          string `json:"patch"`
	ExpectedSHA256 string `json:"expectedSha256,omitempty"`
}

type commandStartInput struct {
	Command   string `json:"command"`
	Shell     string `json:"shell,omitempty"`
	Workdir   string `json:"workdir,omitempty"`
	TimeoutMS int    `json:"timeoutMs,omitempty"`
}

type commandSessionInput struct {
	SessionID string `json:"sessionId"`
	Offset    int    `json:"offset,omitempty"`
	Input     string `json:"input,omitempty"`
}

func codingReadOnlyTools(service *PermissionService, workspace, permissionSessionID string) []tool.Tool {
	env := permissionToolEnv{service: service, workspace: strings.TrimSpace(workspace), sessionID: strings.TrimSpace(permissionSessionID)}
	tools := make([]tool.Tool, 0, 6)
	for _, candidate := range controlledWorkspaceTools(service, workspace, permissionSessionID) {
		name := candidate.Declaration().Name
		if name == "list_directory" || name == "fetch_url" {
			tools = append(tools, candidate)
		}
	}
	return append(tools,
		function.NewFunctionTool(func(ctx context.Context, in codeReadInput) (permissionToolResult, error) {
			return env.readCodeFile(ctx, in)
		}, function.WithName("read_code_file"), function.WithDescription("按行读取文本文件并返回 SHA-256；相对路径以工作区为基准。")),
		function.NewFunctionTool(func(ctx context.Context, in codeSearchInput) (permissionToolResult, error) {
			return env.searchCode(ctx, in)
		}, function.WithName("search_code"), function.WithDescription("使用文本或正则表达式搜索工作区，支持 glob 过滤。")),
		function.NewFunctionTool(func(ctx context.Context, in gitInspectInput) (permissionToolResult, error) {
			return env.inspectGit(ctx, in)
		}, function.WithName("git_inspect"), function.WithDescription("结构化查看 Git status、diff、staged_diff、log 或 show。")),
		function.NewFunctionTool(func(ctx context.Context, _ struct{}) (permissionToolResult, error) { return env.inspectChanges(ctx) }, function.WithName("inspect_changes"), function.WithDescription("汇总当前工作区的状态、未暂存和已暂存差异。")),
	)
}

func codingAgentTools(service *PermissionService, workspace, permissionSessionID string, conversationID int64, manager *codingCommandManager) []tool.Tool {
	env := permissionToolEnv{service: service, workspace: strings.TrimSpace(workspace), sessionID: strings.TrimSpace(permissionSessionID)}
	tools := codingReadOnlyTools(service, workspace, permissionSessionID)
	return append(tools,
		function.NewFunctionTool(func(ctx context.Context, in applyPatchInput) (permissionToolResult, error) {
			return env.applyCodePatch(ctx, in)
		}, function.WithName("apply_patch"), function.WithDescription("应用 unified diff；可用读取时返回的 SHA-256 防止覆盖并发修改。")),
		function.NewFunctionTool(func(ctx context.Context, in commandStartInput) (permissionToolResult, error) {
			return manager.start(ctx, env, conversationID, in)
		}, function.WithName("start_command"), function.WithDescription("启动可轮询、可取消的 cmd 或 PowerShell 命令会话。")),
		function.NewFunctionTool(func(_ context.Context, in commandSessionInput) (permissionToolResult, error) {
			return manager.read(conversationID, in)
		}, function.WithName("read_command"), function.WithDescription("从指定偏移读取命令会话输出和运行状态。")),
		function.NewFunctionTool(func(ctx context.Context, in commandSessionInput) (permissionToolResult, error) {
			return manager.write(ctx, env, conversationID, in)
		}, function.WithName("write_command"), function.WithDescription("向仍在运行的命令会话写入 stdin。")),
		function.NewFunctionTool(func(_ context.Context, in commandSessionInput) (permissionToolResult, error) {
			return manager.stop(conversationID, in.SessionID)
		}, function.WithName("stop_command"), function.WithDescription("停止命令会话。")),
	)
}

func (e permissionToolEnv) readCodeFile(ctx context.Context, in codeReadInput) (permissionToolResult, error) {
	path, outside, err := e.resolvePath(in.Path)
	if err != nil {
		return permissionToolResult{OK: false, Code: pathErrorCode(err, "invalid_path"), Message: err.Error()}, nil
	}
	if result, ok := e.authorize(ctx, "read_code_file", "read", path, filepath.Dir(path), outside); !ok {
		return result, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return permissionToolResult{OK: false, Code: "read_failed", Message: err.Error()}, nil
	}
	if len(content) > maxPermissionFileBytes {
		return permissionToolResult{OK: false, Code: "file_too_large", Message: "文件超过 2 MiB 限制"}, nil
	}
	lines := strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
	start := in.StartLine
	if start <= 0 {
		start = 1
	}
	end := in.EndLine
	if end <= 0 || end > len(lines) {
		end = len(lines)
	}
	if start > end || start > len(lines) {
		return permissionToolResult{OK: false, Code: "invalid_range", Message: "行范围无效"}, nil
	}
	digest := sha256.Sum256(content)
	return permissionToolResult{OK: true, Data: map[string]any{"path": path, "startLine": start, "endLine": end, "totalLines": len(lines), "sha256": hex.EncodeToString(digest[:]), "content": strings.Join(lines[start-1:end], "\n")}}, nil
}

func matchesAny(path string, patterns []string) bool {
	path = filepath.ToSlash(path)
	for _, pattern := range patterns {
		pattern = filepath.ToSlash(strings.TrimSpace(pattern))
		if pattern == "" {
			continue
		}
		if ok, _ := filepath.Match(pattern, path); ok {
			return true
		}
		if ok, _ := filepath.Match(pattern, filepath.Base(path)); ok {
			return true
		}
	}
	return false
}

func (e permissionToolEnv) searchCode(ctx context.Context, in codeSearchInput) (permissionToolResult, error) {
	if strings.TrimSpace(in.Query) == "" {
		return permissionToolResult{OK: false, Code: "invalid_query", Message: "query 不能为空"}, nil
	}
	root := strings.TrimSpace(in.Path)
	if root == "" {
		root = e.workspace
	}
	root, outside, err := e.resolvePath(root)
	if err != nil {
		return permissionToolResult{OK: false, Code: pathErrorCode(err, "invalid_path"), Message: err.Error()}, nil
	}
	if result, ok := e.authorize(ctx, "search_code", "search", root, root, outside); !ok {
		return result, nil
	}
	query := in.Query
	if !in.Regex {
		query = regexp.QuoteMeta(query)
	}
	if !in.CaseSensitive {
		query = "(?i)" + query
	}
	re, err := regexp.Compile(query)
	if err != nil {
		return permissionToolResult{OK: false, Code: "invalid_regex", Message: err.Error()}, nil
	}
	limit := in.MaxResults
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	matches := make([]map[string]any, 0)
	skipped := map[string]bool{".git": true, "node_modules": true, "vendor": true, "dist": true, "build": true, "bin": true}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			if path != root && skipped[strings.ToLower(entry.Name())] {
				return filepath.SkipDir
			}
			return nil
		}
		relative, _ := filepath.Rel(root, path)
		if len(in.Include) > 0 && !matchesAny(relative, in.Include) {
			return nil
		}
		if matchesAny(relative, in.Exclude) {
			return nil
		}
		info, err := entry.Info()
		if err != nil || info.Size() > maxPermissionFileBytes {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil || bytes.IndexByte(content, 0) >= 0 {
			return nil
		}
		for index, line := range strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n") {
			if re.MatchString(line) {
				matches = append(matches, map[string]any{"path": filepath.ToSlash(relative), "line": index + 1, "text": strings.TrimSpace(line)})
				if len(matches) >= limit {
					return fs.SkipAll
				}
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.SkipAll) {
		return permissionToolResult{OK: false, Code: "search_failed", Message: err.Error()}, nil
	}
	return permissionToolResult{OK: true, Data: map[string]any{"matches": matches, "truncated": len(matches) >= limit}}, nil
}

func fileSHA256(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:]), nil
}

func patchTargets(patch string) ([]string, error) {
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.ReplaceAll(patch, "\r\n", "\n"), "\n") {
		if !strings.HasPrefix(line, "--- ") && !strings.HasPrefix(line, "+++ ") {
			continue
		}
		value := strings.Fields(strings.TrimSpace(line[4:]))
		if len(value) == 0 || value[0] == "/dev/null" {
			continue
		}
		path := filepath.ToSlash(value[0])
		if strings.HasPrefix(path, "a/") || strings.HasPrefix(path, "b/") {
			path = path[2:]
		}
		if path == "" || filepath.IsAbs(path) || strings.HasPrefix(path, "../") || strings.Contains(path, "/../") {
			return nil, errors.New("补丁包含无效路径")
		}
		seen[path] = true
	}
	if len(seen) == 0 {
		return nil, errors.New("补丁没有文件目标")
	}
	targets := make([]string, 0, len(seen))
	for target := range seen {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	return targets, nil
}

func (e permissionToolEnv) applyCodePatch(ctx context.Context, in applyPatchInput) (permissionToolResult, error) {
	if strings.TrimSpace(e.workspace) == "" {
		return permissionToolResult{OK: false, Code: "workspace_required", Message: "补丁需要工作区"}, nil
	}
	path, outside, err := e.resolvePath(in.Path)
	if err != nil {
		return permissionToolResult{OK: false, Code: pathErrorCode(err, "invalid_path"), Message: err.Error()}, nil
	}
	if strings.TrimSpace(in.Patch) == "" {
		return permissionToolResult{OK: false, Code: "invalid_patch", Message: "patch 不能为空"}, nil
	}
	targets, targetErr := patchTargets(in.Patch)
	if targetErr != nil {
		return permissionToolResult{OK: false, Code: "invalid_patch", Message: targetErr.Error()}, nil
	}
	if len(targets) != 1 {
		return permissionToolResult{OK: false, Code: "invalid_patch", Message: "一次补丁只能修改一个文件"}, nil
	}
	targetPath, _, targetErr := e.resolvePath(targets[0])
	if targetErr != nil || !strings.EqualFold(canonicalPath(targetPath), canonicalPath(path)) {
		return permissionToolResult{OK: false, Code: "invalid_patch", Message: "补丁目标与 path 不一致"}, nil
	}
	if expected := strings.ToLower(strings.TrimSpace(in.ExpectedSHA256)); expected != "" {
		actual, hashErr := fileSHA256(path)
		if hashErr != nil || actual != expected {
			return permissionToolResult{OK: false, Code: "content_conflict", Message: "文件已变化，请重新读取后生成补丁", Data: map[string]any{"actualSha256": actual}}, nil
		}
	}
	if result, ok := e.authorize(ctx, "apply_patch", "patch", path, e.workspace, outside); !ok {
		return result, nil
	}
	for _, args := range [][]string{{"apply", "--check", "--whitespace=nowarn", "-"}, {"apply", "--whitespace=nowarn", "-"}} {
		cmd := exec.CommandContext(ctx, "git", args...)
		hideProcessWindow(cmd)
		cmd.Dir = e.workspace
		cmd.Env = minimalCommandEnv()
		cmd.Stdin = strings.NewReader(in.Patch)
		output, runErr := cmd.CombinedOutput()
		if runErr != nil {
			return permissionToolResult{OK: false, Code: "patch_failed", Message: runErr.Error(), Data: map[string]any{"output": string(output)}}, nil
		}
	}
	return permissionToolResult{OK: true, Data: map[string]any{"path": path}}, nil
}

func (e permissionToolEnv) inspectChanges(ctx context.Context) (permissionToolResult, error) {
	parts := map[string]string{}
	for _, action := range []string{"status", "diff", "staged_diff"} {
		result, _ := e.inspectGit(ctx, gitInspectInput{Action: action})
		if !result.OK {
			return result, nil
		}
		if data, ok := result.Data.(map[string]any); ok {
			parts[action], _ = data["output"].(string)
		}
	}
	return permissionToolResult{OK: true, Data: parts}, nil
}

type lockedOutput struct {
	mu   sync.Mutex
	data []byte
}

func (w *lockedOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.data = append(w.data, p...)
	if len(w.data) > maxPermissionOutput {
		w.data = w.data[len(w.data)-maxPermissionOutput:]
	}
	return len(p), nil
}
func (w *lockedOutput) read(offset int) (string, int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if offset < 0 || offset > len(w.data) {
		offset = 0
	}
	return string(w.data[offset:]), len(w.data)
}

type codingCommandSession struct {
	id             string
	conversationID int64
	command        *exec.Cmd
	stdin          io.WriteCloser
	output         *lockedOutput
	mu             sync.Mutex
	status         string
	exitCode       int
	err            string
}
type codingCommandManager struct {
	mu       sync.Mutex
	sessions map[string]*codingCommandSession
	next     atomic.Uint64
}

func killCommandProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if runtime.GOOS == "windows" {
		killer := exec.Command("taskkill.exe", "/PID", fmt.Sprint(cmd.Process.Pid), "/T", "/F")
		hideProcessWindow(killer)
		_ = killer.Run()
		return
	}
	_ = cmd.Process.Kill()
}

func newCodingCommandManager() *codingCommandManager {
	return &codingCommandManager{sessions: map[string]*codingCommandSession{}}
}
func (m *codingCommandManager) get(conversationID int64, id string) (*codingCommandSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.sessions[id]
	return item, ok && item.conversationID == conversationID
}

func (m *codingCommandManager) start(ctx context.Context, env permissionToolEnv, conversationID int64, in commandStartInput) (permissionToolResult, error) {
	command := strings.TrimSpace(in.Command)
	if command == "" {
		return permissionToolResult{OK: false, Code: "invalid_command", Message: "command 不能为空"}, nil
	}
	shell := strings.ToLower(strings.TrimSpace(in.Shell))
	if shell == "" {
		shell = "cmd"
	}
	if shell != "cmd" && shell != "powershell" {
		return permissionToolResult{OK: false, Code: "invalid_shell", Message: "shell 只能是 cmd 或 powershell"}, nil
	}
	workdir := strings.TrimSpace(in.Workdir)
	if workdir == "" {
		workdir = env.workspace
	}
	resolved, outside, err := env.resolvePath(workdir)
	if err != nil {
		return permissionToolResult{OK: false, Code: pathErrorCode(err, "invalid_workdir"), Message: err.Error()}, nil
	}
	if result, ok := env.authorize(ctx, "start_command", "execute", command, resolved, outside); !ok {
		return result, nil
	}
	var cmd *exec.Cmd
	if shell == "powershell" {
		cmd = exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", command)
	} else {
		cmd = exec.Command("cmd.exe", "/d", "/s", "/c", command)
	}
	hideProcessWindow(cmd)
	cmd.Dir = resolved
	cmd.Env = minimalCommandEnv()
	output := &lockedOutput{}
	cmd.Stdout = output
	cmd.Stderr = output
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return permissionToolResult{OK: false, Code: "command_failed", Message: err.Error()}, nil
	}
	if err := cmd.Start(); err != nil {
		return permissionToolResult{OK: false, Code: "command_failed", Message: err.Error()}, nil
	}
	id := fmt.Sprintf("cmd-%d-%d", conversationID, m.next.Add(1))
	item := &codingCommandSession{id: id, conversationID: conversationID, command: cmd, stdin: stdin, output: output, status: "running", exitCode: -1}
	m.mu.Lock()
	m.sessions[id] = item
	m.mu.Unlock()
	go func() {
		waitErr := cmd.Wait()
		item.mu.Lock()
		defer item.mu.Unlock()
		if item.status != "running" {
			return
		}
		item.status = "completed"
		if waitErr != nil {
			item.status = "failed"
			item.err = waitErr.Error()
		}
		if cmd.ProcessState != nil {
			item.exitCode = cmd.ProcessState.ExitCode()
		}
	}()
	if in.TimeoutMS > 0 {
		timeout := time.Duration(in.TimeoutMS) * time.Millisecond
		go func() {
			<-time.After(timeout)
			item.mu.Lock()
			running := item.status == "running"
			item.mu.Unlock()
			if running && cmd.Process != nil {
				killCommandProcess(cmd)
				item.mu.Lock()
				item.status = "timed_out"
				item.err = "命令执行超时"
				item.mu.Unlock()
			}
		}()
	}
	return permissionToolResult{OK: true, Data: map[string]any{"sessionId": id, "status": "running"}}, nil
}

func (m *codingCommandManager) read(conversationID int64, in commandSessionInput) (permissionToolResult, error) {
	item, ok := m.get(conversationID, in.SessionID)
	if !ok {
		return permissionToolResult{OK: false, Code: "session_not_found", Message: "命令会话不存在"}, nil
	}
	output, next := item.output.read(in.Offset)
	item.mu.Lock()
	data := map[string]any{"sessionId": item.id, "status": item.status, "output": output, "nextOffset": next, "exitCode": item.exitCode, "error": item.err}
	item.mu.Unlock()
	return permissionToolResult{OK: true, Data: data}, nil
}
func (m *codingCommandManager) write(ctx context.Context, env permissionToolEnv, conversationID int64, in commandSessionInput) (permissionToolResult, error) {
	item, ok := m.get(conversationID, in.SessionID)
	if !ok {
		return permissionToolResult{OK: false, Code: "session_not_found", Message: "命令会话不存在"}, nil
	}
	if result, allowed := env.authorize(ctx, "write_command", "stdin", in.SessionID, env.workspace, false); !allowed {
		return result, nil
	}
	item.mu.Lock()
	running := item.status == "running"
	item.mu.Unlock()
	if !running {
		return permissionToolResult{OK: false, Code: "session_finished", Message: "命令会话已结束"}, nil
	}
	_, err := io.WriteString(item.stdin, in.Input)
	if err != nil {
		return permissionToolResult{OK: false, Code: "write_failed", Message: err.Error()}, nil
	}
	return permissionToolResult{OK: true}, nil
}
func (m *codingCommandManager) stop(conversationID int64, id string) (permissionToolResult, error) {
	item, ok := m.get(conversationID, id)
	if !ok {
		return permissionToolResult{OK: false, Code: "session_not_found", Message: "命令会话不存在"}, nil
	}
	item.mu.Lock()
	running := item.status == "running"
	if running {
		item.status = "stopped"
	}
	item.mu.Unlock()
	if running {
		killCommandProcess(item.command)
	}
	return permissionToolResult{OK: true, Data: map[string]any{"sessionId": id, "status": "stopped"}}, nil
}
func (m *codingCommandManager) stopConversation(conversationID int64) {
	m.mu.Lock()
	items := make([]*codingCommandSession, 0)
	for id, item := range m.sessions {
		if conversationID == 0 || item.conversationID == conversationID {
			items = append(items, item)
			delete(m.sessions, id)
		}
	}
	m.mu.Unlock()
	sort.Slice(items, func(i, j int) bool { return items[i].id < items[j].id })
	for _, item := range items {
		killCommandProcess(item.command)
	}
}
