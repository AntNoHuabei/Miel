package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

const (
	maxPermissionFileBytes = 2 << 20
	maxPermissionOutput    = 512 << 10
	maxPermissionResponse  = 2 << 20
)

type permissionToolEnv struct {
	service   *PermissionService
	workspace string
	sessionID string
}

type filePathInput struct {
	Path string `json:"path"`
}

type writeFileInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type commandInput struct {
	Command   string `json:"command"`
	Shell     string `json:"shell,omitempty"`
	Workdir   string `json:"workdir,omitempty"`
	TimeoutMS int    `json:"timeoutMs,omitempty"`
}

type fetchURLInput struct {
	URL string `json:"url"`
}

type permissionToolResult struct {
	OK      bool   `json:"ok"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
}

func controlledWorkspaceTools(service *PermissionService, workspace, sessionID string) []tool.Tool {
	env := permissionToolEnv{service: service, workspace: strings.TrimSpace(workspace), sessionID: strings.TrimSpace(sessionID)}
	return []tool.Tool{
		function.NewFunctionTool(func(ctx context.Context, in filePathInput) (permissionToolResult, error) {
			return env.listDirectory(ctx, in)
		}, function.WithName("list_directory"), function.WithDescription("列出目录内容。需要用户权限批准。")),
		function.NewFunctionTool(func(ctx context.Context, in filePathInput) (permissionToolResult, error) {
			return env.readFile(ctx, in)
		}, function.WithName("read_file"), function.WithDescription("读取文本文件。需要用户权限批准。")),
		function.NewFunctionTool(func(ctx context.Context, in writeFileInput) (permissionToolResult, error) {
			return env.writeFile(ctx, in)
		}, function.WithName("write_file"), function.WithDescription("写入文本文件。需要用户权限批准。")),
		function.NewFunctionTool(func(ctx context.Context, in commandInput) (permissionToolResult, error) {
			return env.executeCommand(ctx, in)
		}, function.WithName("execute_command"), function.WithDescription("在受控 cmd.exe 中执行命令；PowerShell 必须显式指定。")),
		function.NewFunctionTool(func(ctx context.Context, in fetchURLInput) (permissionToolResult, error) {
			return env.fetchURL(ctx, in)
		}, function.WithName("fetch_url"), function.WithDescription("获取 HTTP 或 HTTPS URL。需要用户权限批准。")),
	}
}

func (e permissionToolEnv) authorize(ctx context.Context, toolName, operation, target, scopeRoot string, outside bool) (permissionToolResult, bool) {
	if e.service == nil {
		return permissionToolResult{OK: false, Code: "permission_unavailable", Message: "权限服务未初始化"}, false
	}
	decision, err := e.service.authorize(ctx, ApprovalRequest{
		SessionID: e.sessionID, Tool: toolName, Operation: operation, WorkspacePath: e.workspace,
		Target: target, ScopeRoot: scopeRoot, RiskLevel: riskForTool(toolName), OutsideWorkspace: outside,
	})
	if err != nil {
		return permissionToolResult{OK: false, Code: "permission_cancelled", Message: err.Error()}, false
	}
	if decision == ApprovalDeny {
		return permissionToolResult{OK: false, Code: "permission_denied", Message: "用户拒绝了该操作", Data: map[string]any{"retryable": true}}, false
	}
	return permissionToolResult{}, true
}

func (e permissionToolEnv) resolvePath(path string) (string, bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", false, errors.New("path 不能为空")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false, err
	}
	abs = canonicalPath(abs)
	if e.workspace == "" {
		return abs, true, nil
	}
	workspace, err := filepath.Abs(e.workspace)
	if err != nil {
		return "", false, err
	}
	workspace = canonicalPath(workspace)
	return abs, !withinPath(abs, workspace), nil
}

// canonicalPath resolves existing symlinks and the nearest existing parent for
// a new file, so a symlink cannot turn an approved workspace path into another root.
func canonicalPath(path string) string {
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	parent := filepath.Dir(path)
	base := filepath.Base(path)
	for {
		if resolved, err := filepath.EvalSymlinks(parent); err == nil {
			return filepath.Join(filepath.Clean(resolved), base)
		}
		next := filepath.Dir(parent)
		if next == parent {
			return path
		}
		base = filepath.Join(filepath.Base(parent), base)
		parent = next
	}
}

func (e permissionToolEnv) listDirectory(ctx context.Context, in filePathInput) (permissionToolResult, error) {
	path, outside, err := e.resolvePath(in.Path)
	if err != nil {
		return permissionToolResult{OK: false, Code: "invalid_path", Message: err.Error()}, nil
	}
	if result, ok := e.authorize(ctx, "list_directory", "list", path, path, outside); !ok {
		return result, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return permissionToolResult{OK: false, Code: "read_failed", Message: err.Error()}, nil
	}
	data := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		info, _ := entry.Info()
		item := map[string]any{"name": entry.Name(), "directory": entry.IsDir()}
		if info != nil {
			item["size"] = info.Size()
		}
		data = append(data, item)
	}
	return permissionToolResult{OK: true, Data: data}, nil
}

func (e permissionToolEnv) readFile(ctx context.Context, in filePathInput) (permissionToolResult, error) {
	path, outside, err := e.resolvePath(in.Path)
	if err != nil {
		return permissionToolResult{OK: false, Code: "invalid_path", Message: err.Error()}, nil
	}
	if result, ok := e.authorize(ctx, "read_file", "read", path, filepath.Dir(path), outside); !ok {
		return result, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return permissionToolResult{OK: false, Code: "read_failed", Message: err.Error()}, nil
	}
	if info.Size() > maxPermissionFileBytes {
		return permissionToolResult{OK: false, Code: "file_too_large", Message: "文件超过 2 MiB 限制"}, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return permissionToolResult{OK: false, Code: "read_failed", Message: err.Error()}, nil
	}
	return permissionToolResult{OK: true, Data: map[string]any{"path": path, "content": string(content)}}, nil
}

func (e permissionToolEnv) writeFile(ctx context.Context, in writeFileInput) (permissionToolResult, error) {
	path, outside, err := e.resolvePath(in.Path)
	if err != nil {
		return permissionToolResult{OK: false, Code: "invalid_path", Message: err.Error()}, nil
	}
	if len(in.Content) > maxPermissionFileBytes {
		return permissionToolResult{OK: false, Code: "file_too_large", Message: "写入内容超过 2 MiB 限制"}, nil
	}
	if result, ok := e.authorize(ctx, "write_file", "write", path, filepath.Dir(path), outside); !ok {
		return result, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return permissionToolResult{OK: false, Code: "write_failed", Message: err.Error()}, nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".blankmind-write-*")
	if err != nil {
		return permissionToolResult{OK: false, Code: "write_failed", Message: err.Error()}, nil
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err = tmp.WriteString(in.Content); err == nil {
		err = tmp.Close()
	}
	if err == nil {
		err = replaceFile(tmpName, path)
	}
	if err != nil {
		return permissionToolResult{OK: false, Code: "write_failed", Message: err.Error()}, nil
	}
	return permissionToolResult{OK: true, Data: map[string]any{"path": path, "bytes": len(in.Content)}}, nil
}

func (e permissionToolEnv) executeCommand(ctx context.Context, in commandInput) (permissionToolResult, error) {
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
		workdir = e.workspace
	}
	if workdir == "" {
		return permissionToolResult{OK: false, Code: "workdir_required", Message: "没有工作区时必须显式指定 workdir"}, nil
	}
	workdir, outside, err := e.resolvePath(workdir)
	if err != nil {
		return permissionToolResult{OK: false, Code: "invalid_workdir", Message: err.Error()}, nil
	}
	if result, ok := e.authorize(ctx, "execute_command", "execute", command, workdir, outside); !ok {
		return result, nil
	}
	timeout := time.Duration(in.TimeoutMS) * time.Millisecond
	if timeout <= 0 || timeout > 2*time.Minute {
		timeout = 60 * time.Second
	}
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var cmd *exec.Cmd
	if shell == "powershell" {
		cmd = exec.CommandContext(cmdCtx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", command)
	} else {
		cmd = exec.CommandContext(cmdCtx, "cmd.exe", "/D", "/S", "/C", command)
	}
	cmd.Dir = workdir
	cmd.Env = minimalCommandEnv()
	out, err := cmd.CombinedOutput()
	if len(out) > maxPermissionOutput {
		out = out[:maxPermissionOutput]
	}
	if cmdCtx.Err() != nil {
		return permissionToolResult{OK: false, Code: "command_timeout", Message: "命令执行超时", Data: map[string]any{"output": string(out)}}, nil
	}
	if err != nil {
		return permissionToolResult{OK: false, Code: "command_failed", Message: err.Error(), Data: map[string]any{"output": string(out)}}, nil
	}
	return permissionToolResult{OK: true, Data: map[string]any{"output": string(out)}}, nil
}

func (e permissionToolEnv) fetchURL(ctx context.Context, in fetchURLInput) (permissionToolResult, error) {
	url := strings.TrimSpace(in.URL)
	u, err := parseSafeURL(url)
	if err != nil {
		return permissionToolResult{OK: false, Code: "invalid_url", Message: err.Error()}, nil
	}
	if result, ok := e.authorize(ctx, "fetch_url", "fetch", u.String(), "network", false); !ok {
		return result, nil
	}
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return errors.New("不允许自动重定向")
	}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	resp, err := client.Do(req)
	if err != nil {
		return permissionToolResult{OK: false, Code: "network_failed", Message: err.Error()}, nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPermissionResponse+1))
	if err != nil {
		return permissionToolResult{OK: false, Code: "network_failed", Message: err.Error()}, nil
	}
	if len(body) > maxPermissionResponse {
		return permissionToolResult{OK: false, Code: "response_too_large", Message: "响应超过 2 MiB 限制"}, nil
	}
	return permissionToolResult{OK: resp.StatusCode >= 200 && resp.StatusCode < 300, Code: http.StatusText(resp.StatusCode), Data: map[string]any{"status": resp.StatusCode, "contentType": resp.Header.Get("Content-Type"), "body": string(body)}}, nil
}

func minimalCommandEnv() []string {
	return []string{"SystemRoot=" + os.Getenv("SystemRoot"), "TEMP=" + os.Getenv("TEMP"), "TMP=" + os.Getenv("TMP"), "PATH=" + os.Getenv("PATH")}
}

func parseSafeURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, errors.New("URL 只能使用 http 或 https")
	}
	host := u.Hostname()
	lowerHost := strings.ToLower(strings.TrimSuffix(host, "."))
	if lowerHost == "localhost" || strings.HasSuffix(lowerHost, ".localhost") || strings.HasSuffix(lowerHost, ".local") {
		return nil, errors.New("禁止访问本机或本地域名")
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()) {
		return nil, errors.New("禁止访问本机或内网地址")
	}
	return u, nil
}

func riskForTool(name string) string {
	switch name {
	case "write_file", "execute_command":
		return "high"
	case "fetch_url":
		return "medium"
	default:
		return "low"
	}
}

func toolResultJSON(result permissionToolResult) string {
	b, _ := json.Marshal(result)
	return string(b)
}
