package app

import (
	"bufio"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

const (
	maxPlanSearchFiles   = 5000
	maxPlanSearchMatches = 200
)

type planSearchInput struct {
	Query string `json:"query"`
	Path  string `json:"path,omitempty"`
}

type gitInspectInput struct {
	Action string `json:"action"`
	Ref    string `json:"ref,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

func planAgentTools(service *PermissionService, workspace, sessionID string) []tool.Tool {
	if service == nil || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	env := permissionToolEnv{service: service, workspace: strings.TrimSpace(workspace), sessionID: strings.TrimSpace(sessionID)}
	all := controlledWorkspaceTools(service, workspace, sessionID)
	tools := make([]tool.Tool, 0, 5)
	for _, candidate := range all {
		name := candidate.Declaration().Name
		if name == "list_directory" || name == "read_file" || name == "fetch_url" {
			tools = append(tools, candidate)
		}
	}
	tools = append(tools,
		function.NewFunctionTool(func(ctx context.Context, input planSearchInput) (permissionToolResult, error) {
			return env.searchWorkspace(ctx, input)
		}, function.WithName("search_workspace"), function.WithDescription("在工作区文本文件中只读搜索字符串，返回匹配文件、行号和片段。")),
		function.NewFunctionTool(func(ctx context.Context, input gitInspectInput) (permissionToolResult, error) {
			return env.inspectGit(ctx, input)
		}, function.WithName("git_inspect"), function.WithDescription("只读查看工作区 Git status、diff、log 或 show；不接受任意命令。")),
	)
	return tools
}

func (e permissionToolEnv) searchWorkspace(ctx context.Context, input planSearchInput) (permissionToolResult, error) {
	query := strings.TrimSpace(input.Query)
	if query == "" {
		return permissionToolResult{OK: false, Code: "invalid_query", Message: "query 不能为空"}, nil
	}
	root := strings.TrimSpace(input.Path)
	if root == "" {
		root = e.workspace
	}
	if root == "" {
		return permissionToolResult{OK: false, Code: "workspace_required", Message: "搜索需要工作区"}, nil
	}
	root, outside, err := e.resolvePath(root)
	if err != nil {
		return permissionToolResult{OK: false, Code: pathErrorCode(err, "invalid_path"), Message: err.Error()}, nil
	}
	if result, ok := e.authorize(ctx, "search_workspace", "search", root, root, outside); !ok {
		return result, nil
	}
	lowerQuery := strings.ToLower(query)
	matches := make([]map[string]any, 0)
	files := 0
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			name := strings.ToLower(entry.Name())
			if path != root && (name == ".git" || name == "node_modules" || name == "vendor" || name == "dist" || name == "bin") {
				return filepath.SkipDir
			}
			return nil
		}
		files++
		if files > maxPlanSearchFiles || len(matches) >= maxPlanSearchMatches {
			return fs.SkipAll
		}
		info, err := entry.Info()
		if err != nil || info.Size() > maxPermissionFileBytes {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer file.Close() //nolint:errcheck
		scanner := bufio.NewScanner(file)
		buffer := make([]byte, 64*1024)
		scanner.Buffer(buffer, maxPermissionFileBytes)
		line := 0
		for scanner.Scan() {
			line++
			text := scanner.Text()
			if strings.Contains(strings.ToLower(text), lowerQuery) {
				relative, _ := filepath.Rel(root, path)
				matches = append(matches, map[string]any{"path": relative, "line": line, "text": strings.TrimSpace(text)})
				if len(matches) >= maxPlanSearchMatches {
					break
				}
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.SkipAll) {
		return permissionToolResult{OK: false, Code: "search_failed", Message: err.Error()}, nil
	}
	return permissionToolResult{OK: true, Data: map[string]any{"matches": matches, "truncated": files > maxPlanSearchFiles || len(matches) >= maxPlanSearchMatches}}, nil
}

func (e permissionToolEnv) inspectGit(ctx context.Context, input gitInspectInput) (permissionToolResult, error) {
	if strings.TrimSpace(e.workspace) == "" {
		return permissionToolResult{OK: false, Code: "workspace_required", Message: "Git 检查需要工作区"}, nil
	}
	workdir, outside, err := e.resolvePath(e.workspace)
	if err != nil {
		return permissionToolResult{OK: false, Code: "invalid_workdir", Message: err.Error()}, nil
	}
	action := strings.ToLower(strings.TrimSpace(input.Action))
	var args []string
	switch action {
	case "status":
		args = []string{"status", "--short", "--branch"}
	case "diff":
		args = []string{"diff", "--no-ext-diff", "--"}
	case "log":
		limit := input.Limit
		if limit <= 0 || limit > 50 {
			limit = 10
		}
		args = []string{"log", "--oneline", "--decorate", "-n", strconv.Itoa(limit)}
	case "show":
		ref := strings.TrimSpace(input.Ref)
		if ref == "" {
			ref = "HEAD"
		}
		if strings.HasPrefix(ref, "-") || strings.ContainsAny(ref, "\r\n\x00") {
			return permissionToolResult{OK: false, Code: "invalid_ref", Message: "Git ref 无效"}, nil
		}
		args = []string{"show", "--no-ext-diff", "--stat", "--oneline", ref}
	default:
		return permissionToolResult{OK: false, Code: "invalid_action", Message: "action 只能是 status、diff、log 或 show"}, nil
	}
	if result, ok := e.authorize(ctx, "git_inspect", action, workdir, workdir, outside); !ok {
		return result, nil
	}
	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, "git", args...)
	hideProcessWindow(cmd)
	cmd.Dir = workdir
	cmd.Env = minimalCommandEnv()
	output, err := cmd.CombinedOutput()
	if len(output) > maxPermissionOutput {
		output = output[:maxPermissionOutput]
	}
	if cmdCtx.Err() != nil {
		return permissionToolResult{OK: false, Code: "git_timeout", Message: "Git 检查超时"}, nil
	}
	if err != nil {
		return permissionToolResult{OK: false, Code: "git_failed", Message: err.Error(), Data: map[string]any{"output": string(output)}}, nil
	}
	return permissionToolResult{OK: true, Data: map[string]any{"output": string(output)}}, nil
}
