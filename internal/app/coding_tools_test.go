package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCodingToolProfilesAreIsolated(t *testing.T) {
	readonly := codingReadOnlyTools(&PermissionService{}, t.TempDir(), "session")
	readNames := map[string]bool{}
	for _, candidate := range readonly {
		readNames[candidate.Declaration().Name] = true
	}
	for _, want := range []string{"list_directory", "fetch_url", "read_code_file", "search_code", "git_inspect", "inspect_changes"} {
		if !readNames[want] {
			t.Errorf("coding plan missing %q", want)
		}
	}
	for _, forbidden := range []string{"apply_patch", "start_command", "write_command", "skill_run", "memory_search"} {
		if readNames[forbidden] {
			t.Errorf("coding plan exposes %q", forbidden)
		}
	}

	manager := newCodingCommandManager()
	writeTools := codingAgentTools(&PermissionService{}, t.TempDir(), "session", 42, manager)
	writeNames := map[string]bool{}
	for _, candidate := range writeTools {
		writeNames[candidate.Declaration().Name] = true
	}
	for _, want := range []string{"apply_patch", "start_command", "read_command", "write_command", "stop_command"} {
		if !writeNames[want] {
			t.Errorf("coding agent missing %q", want)
		}
	}
	for _, forbidden := range []string{"skill_run", "memory_search", "memory_add"} {
		if writeNames[forbidden] {
			t.Errorf("coding agent exposes office capability %q", forbidden)
		}
	}
}

func TestCodingReadSearchAndPatchHashConflict(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "sample.txt")
	if err := os.WriteFile(path, []byte("alpha\nbeta\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	service := NewPermissionService(NewSettingsService(newSettingsTestDB(t)))
	if err := service.SetPermissionMode(PermissionFull); err != nil {
		t.Fatal(err)
	}
	env := permissionToolEnv{service: service, workspace: workspace, sessionID: "coding"}

	read, err := env.readCodeFile(context.Background(), codeReadInput{Path: "sample.txt", StartLine: 2, EndLine: 2})
	if err != nil || !read.OK {
		t.Fatalf("read = %#v, err=%v", read, err)
	}
	data := read.Data.(map[string]any)
	if data["content"] != "beta" || data["totalLines"] != 3 {
		t.Fatalf("read data = %#v", data)
	}

	searched, err := env.searchCode(context.Background(), codeSearchInput{Query: "^be", Regex: true, Include: []string{"*.txt"}})
	if err != nil || !searched.OK || len(searched.Data.(map[string]any)["matches"].([]map[string]any)) != 1 {
		t.Fatalf("search = %#v, err=%v", searched, err)
	}

	if err := os.WriteFile(path, []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conflict, err := env.applyCodePatch(context.Background(), applyPatchInput{Path: "sample.txt", ExpectedSHA256: data["sha256"].(string), Patch: "diff --git a/sample.txt b/sample.txt\n--- a/sample.txt\n+++ b/sample.txt\n@@ -1 +1 @@\n-changed\n+done\n"})
	if err != nil || conflict.Code != "content_conflict" {
		t.Fatalf("conflict = %#v, err=%v", conflict, err)
	}

	digest := sha256.Sum256([]byte("changed\n"))
	applied, err := env.applyCodePatch(context.Background(), applyPatchInput{Path: "sample.txt", ExpectedSHA256: hex.EncodeToString(digest[:]), Patch: "diff --git a/sample.txt b/sample.txt\n--- a/sample.txt\n+++ b/sample.txt\n@@ -1 +1 @@\n-changed\n+done\n"})
	if err != nil || !applied.OK {
		t.Fatalf("apply = %#v, err=%v", applied, err)
	}
	content, _ := os.ReadFile(path)
	if strings.ReplaceAll(string(content), "\r\n", "\n") != "done\n" {
		t.Fatalf("content = %q", content)
	}
}

func TestCodingCommandSessionCanBeReadAndStopped(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("command shell contract is Windows-specific")
	}
	workspace := t.TempDir()
	service := NewPermissionService(NewSettingsService(newSettingsTestDB(t)))
	if err := service.SetPermissionMode(PermissionFull); err != nil {
		t.Fatal(err)
	}
	env := permissionToolEnv{service: service, workspace: workspace, sessionID: "coding-command"}
	manager := newCodingCommandManager()
	started, err := manager.start(context.Background(), env, 7, commandStartInput{Command: "echo ready & ping -n 20 127.0.0.1 >nul"})
	if err != nil || !started.OK {
		t.Fatalf("start = %#v, err=%v", started, err)
	}
	id := started.Data.(map[string]any)["sessionId"].(string)
	deadline := time.Now().Add(3 * time.Second)
	for {
		result, _ := manager.read(7, commandSessionInput{SessionID: id})
		if strings.Contains(result.Data.(map[string]any)["output"].(string), "ready") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("command output was not available")
		}
		time.Sleep(20 * time.Millisecond)
	}
	stopped, err := manager.stop(7, id)
	if err != nil || !stopped.OK {
		t.Fatalf("stop = %#v, err=%v", stopped, err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if item, ok := manager.get(7, id); ok && item.command.ProcessState != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
}
