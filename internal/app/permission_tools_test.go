package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPermissionToolResolvePathUsesWorkspaceAsRelativeBase(t *testing.T) {
	workspace := t.TempDir()
	inside := filepath.Join(workspace, "src", "file.txt")
	absOutside := filepath.Join(t.TempDir(), "outside.txt")
	env := permissionToolEnv{workspace: workspace}

	tests := []struct {
		name        string
		input       string
		want        string
		wantOutside bool
	}{
		{name: "workspace root", input: ".", want: workspace},
		{name: "workspace child", input: filepath.Join("src", "file.txt"), want: inside},
		{name: "relative outside", input: filepath.Join("..", filepath.Base(absOutside)), want: filepath.Join(workspace, "..", filepath.Base(absOutside)), wantOutside: true},
		{name: "absolute inside", input: inside, want: inside},
		{name: "absolute outside", input: absOutside, want: absOutside, wantOutside: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, outside, err := env.resolvePath(tt.input)
			if err != nil {
				t.Fatal(err)
			}
			if got != canonicalPath(tt.want) || outside != tt.wantOutside {
				t.Fatalf("resolvePath(%q) = (%q, %v), want (%q, %v)", tt.input, got, outside, canonicalPath(tt.want), tt.wantOutside)
			}
		})
	}
}

func TestPermissionToolResolvePathRequiresWorkspaceForRelativePath(t *testing.T) {
	env := permissionToolEnv{}
	if _, _, err := env.resolvePath("."); !errors.Is(err, errWorkspaceRequired) {
		t.Fatalf("relative path error = %v, want errWorkspaceRequired", err)
	}

	abs := t.TempDir()
	got, outside, err := env.resolvePath(abs)
	if err != nil || got != canonicalPath(abs) || !outside {
		t.Fatalf("absolute path without workspace = (%q, %v, %v)", got, outside, err)
	}

	result, err := env.listDirectory(context.Background(), filePathInput{Path: "."})
	if err != nil || result.Code != "workspace_required" {
		t.Fatalf("list_directory result = %#v, err=%v", result, err)
	}
	result, err = env.executeCommand(context.Background(), commandInput{Command: "cd", Workdir: "."})
	if err != nil || result.Code != "workspace_required" {
		t.Fatalf("execute_command result = %#v, err=%v", result, err)
	}
}

func TestPermissionToolResolvePathRejectsWindowsDriveRelativePath(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows drive-relative paths are platform-specific")
	}
	_, _, err := (permissionToolEnv{workspace: t.TempDir()}).resolvePath(`C:temp`)
	if err == nil || !strings.Contains(err.Error(), "驱动器相对路径") {
		t.Fatalf("drive-relative path error = %v", err)
	}
}

func TestPermissionToolResolvePathCanonicalizesSymlinkBoundary(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(workspace, "external")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	got, isOutside, err := (permissionToolEnv{workspace: workspace}).resolvePath(filepath.Join("external", "new.txt"))
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(outside, "new.txt")
	if got != canonicalPath(want) || !isOutside {
		t.Fatalf("symlink path = (%q, %v), want (%q, true)", got, isOutside, canonicalPath(want))
	}
}

func TestPermissionFileToolsOperateRelativeToWorkspace(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "root.txt"), []byte("root"), 0o644); err != nil {
		t.Fatal(err)
	}
	service := NewPermissionService(NewSettingsService(newSettingsTestDB(t)))
	if err := service.SetPermissionMode(PermissionAuto); err != nil {
		t.Fatal(err)
	}
	env := permissionToolEnv{service: service, workspace: workspace, sessionID: "relative-files"}

	listed, err := env.listDirectory(context.Background(), filePathInput{Path: "."})
	if err != nil || !listed.OK {
		t.Fatalf("list result = %#v, err=%v", listed, err)
	}
	entries, ok := listed.Data.([]map[string]any)
	if !ok {
		t.Fatalf("list data type = %T", listed.Data)
	}
	found := false
	for _, entry := range entries {
		if entry["name"] == "root.txt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("workspace file missing from list: %#v", entries)
	}

	relativeFile := filepath.Join("nested", "note.txt")
	written, err := env.writeFile(context.Background(), writeFileInput{Path: relativeFile, Content: "workspace content"})
	if err != nil || !written.OK {
		t.Fatalf("write result = %#v, err=%v", written, err)
	}
	content, err := os.ReadFile(filepath.Join(workspace, relativeFile))
	if err != nil || string(content) != "workspace content" {
		t.Fatalf("written content = %q, err=%v", content, err)
	}
	read, err := env.readFile(context.Background(), filePathInput{Path: relativeFile})
	if err != nil || !read.OK || read.Data.(map[string]any)["content"] != "workspace content" {
		t.Fatalf("read result = %#v, err=%v", read, err)
	}
	state, err := service.GetPermissionState()
	if err != nil || len(state.Pending) != 0 {
		t.Fatalf("workspace-relative auto operations created approvals: %#v, err=%v", state.Pending, err)
	}
}

func TestPermissionListDirectoryUsesEachSessionWorkspace(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	if err := os.WriteFile(filepath.Join(first, "first-only.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(second, "second-only.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	service := NewPermissionService(NewSettingsService(newSettingsTestDB(t)))
	if err := service.SetPermissionMode(PermissionAuto); err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		name      string
		workspace string
		wantFile  string
	}{
		{name: "first session", workspace: first, wantFile: "first-only.txt"},
		{name: "second session", workspace: second, wantFile: "second-only.txt"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			env := permissionToolEnv{service: service, workspace: tt.workspace, sessionID: tt.name}
			result, err := env.listDirectory(context.Background(), filePathInput{Path: "."})
			if err != nil || !result.OK {
				t.Fatalf("list result = %#v, err=%v", result, err)
			}
			entries := result.Data.([]map[string]any)
			if len(entries) != 1 || entries[0]["name"] != tt.wantFile {
				t.Fatalf("listed entries = %#v, want %q", entries, tt.wantFile)
			}
		})
	}
}

func TestPermissionCommandUsesWorkspaceForDefaultAndRelativeWorkdir(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("execute_command currently targets cmd.exe and powershell.exe")
	}
	workspace := t.TempDir()
	subdir := filepath.Join(workspace, "subdir")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	service := NewPermissionService(NewSettingsService(newSettingsTestDB(t)))
	if err := service.SetPermissionMode(PermissionAuto); err != nil {
		t.Fatal(err)
	}
	env := permissionToolEnv{service: service, workspace: workspace, sessionID: "relative-command"}

	tests := []struct {
		name    string
		workdir string
		want    string
	}{
		{name: "default", want: workspace},
		{name: "relative", workdir: "subdir", want: subdir},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := env.executeCommand(context.Background(), commandInput{Command: "cd", Workdir: tt.workdir})
			if err != nil || !result.OK {
				t.Fatalf("command result = %#v, err=%v", result, err)
			}
			output := strings.TrimSpace(result.Data.(map[string]any)["output"].(string))
			if !strings.EqualFold(filepath.Clean(output), filepath.Clean(tt.want)) {
				t.Fatalf("command workdir = %q, want %q", output, tt.want)
			}
		})
	}
}

func TestPermissionAutoStillRequestsApprovalForAbsoluteOutsidePath(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	service := NewPermissionService(NewSettingsService(newSettingsTestDB(t)))
	if err := service.SetPermissionMode(PermissionAuto); err != nil {
		t.Fatal(err)
	}
	env := permissionToolEnv{service: service, workspace: workspace, sessionID: "outside-list"}
	type outcome struct {
		result permissionToolResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := env.listDirectory(context.Background(), filePathInput{Path: outside})
		done <- outcome{result: result, err: err}
	}()

	var pending PermissionState
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		pending, _ = service.GetPermissionState()
		if len(pending.Pending) == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(pending.Pending) != 1 {
		t.Fatalf("pending approvals = %#v", pending.Pending)
	}
	request := pending.Pending[0]
	if !request.OutsideWorkspace || request.Target != canonicalPath(outside) {
		t.Fatalf("outside request = %#v", request)
	}
	if err := service.ResolveApproval(request.ID, ApprovalDeny); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if got.err != nil || got.result.Code != "permission_denied" {
			t.Fatalf("denied result = %#v, err=%v", got.result, got.err)
		}
	case <-time.After(time.Second):
		t.Fatal("tool call did not finish after approval denial")
	}
}
