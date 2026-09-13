package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPermissionModeDefaultsToAskAndPersists(t *testing.T) {
	settings := NewSettingsService(newSettingsTestDB(t))
	service := NewPermissionService(settings)
	state, err := service.GetPermissionState()
	if err != nil || state.Mode != PermissionAsk {
		t.Fatalf("default state = %#v, err=%v", state, err)
	}
	if err := service.SetPermissionMode(PermissionFull); err != nil {
		t.Fatal(err)
	}
	state, err = service.GetPermissionState()
	if err != nil || state.Mode != PermissionFull {
		t.Fatalf("persisted state = %#v, err=%v", state, err)
	}
}

func TestPermissionApprovalSessionGrantIsScoped(t *testing.T) {
	settings := NewSettingsService(newSettingsTestDB(t))
	service := NewPermissionService(settings)
	root := filepath.Clean(t.TempDir())
	ctx := context.Background()
	result := make(chan ApprovalDecision, 1)
	go func() {
		decision, _ := service.authorize(ctx, ApprovalRequest{
			SessionID: "session-1", Tool: "read_file", Operation: "read",
			Target: filepath.Join(root, "one.txt"), ScopeRoot: root,
			OutsideWorkspace: true,
		})
		result <- decision
	}()

	var pending PermissionState
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		pending, _ = service.GetPermissionState()
		if len(pending.Pending) == 1 {
			break
		}
		time.Sleep(time.Millisecond * 5)
	}
	if len(pending.Pending) != 1 {
		t.Fatalf("pending = %#v", pending.Pending)
	}
	if err := service.ResolveApproval(pending.Pending[0].ID, ApprovalSession); err != nil {
		t.Fatal(err)
	}
	if got := <-result; got != ApprovalSession {
		t.Fatalf("decision = %q", got)
	}
	if got, err := service.authorize(ctx, ApprovalRequest{
		SessionID: "session-1", Tool: "read_file", Target: filepath.Join(root, "two.txt"), ScopeRoot: root,
		OutsideWorkspace: true,
	}); err != nil || got != ApprovalSession {
		t.Fatalf("same-root grant = %q, err=%v", got, err)
	}
	shortCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if got, err := service.authorize(shortCtx, ApprovalRequest{
		SessionID: "session-1", Tool: "write_file", Target: filepath.Join(root, "two.txt"), ScopeRoot: root,
		OutsideWorkspace: true,
	}); err == nil || got == ApprovalSession {
		t.Fatalf("different tool should not reuse grant: %q, err=%v", got, err)
	}
}

func TestWithinPathDoesNotAllowSibling(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	if !withinPath(filepath.Join(root, "nested", "file.txt"), root) {
		t.Fatal("nested path should be within root")
	}
	if withinPath(filepath.Join(root+"-other", "file.txt"), root) {
		t.Fatal("sibling path must not be within root")
	}
}

func TestPermissionNetworkSessionGrantIsReused(t *testing.T) {
	settings := NewSettingsService(newSettingsTestDB(t))
	service := NewPermissionService(settings)
	result := make(chan ApprovalDecision, 1)
	go func() {
		decision, _ := service.authorize(context.Background(), ApprovalRequest{
			SessionID: "network-session", Tool: "fetch_url", Operation: "fetch",
			Target: "https://example.com/one", ScopeRoot: "network",
		})
		result <- decision
	}()

	var pending PermissionState
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		pending, _ = service.GetPermissionState()
		if len(pending.Pending) == 1 {
			break
		}
		time.Sleep(time.Millisecond * 5)
	}
	if len(pending.Pending) != 1 {
		t.Fatalf("pending = %#v", pending.Pending)
	}
	if err := service.ResolveApproval(pending.Pending[0].ID, ApprovalSession); err != nil {
		t.Fatal(err)
	}
	if got := <-result; got != ApprovalSession {
		t.Fatalf("decision = %q", got)
	}
	if got, err := service.authorize(context.Background(), ApprovalRequest{
		SessionID: "network-session", Tool: "fetch_url", Operation: "fetch",
		Target: "https://example.org/two", ScopeRoot: "network",
	}); err != nil || got != ApprovalSession {
		t.Fatalf("network session grant = %q, err=%v", got, err)
	}
}

func TestPermissionWriteFileReplacesExistingFile(t *testing.T) {
	settings := NewSettingsService(newSettingsTestDB(t))
	service := NewPermissionService(settings)
	if err := service.SetPermissionMode(PermissionFull); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	path := filepath.Join(root, "existing.txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := (permissionToolEnv{service: service, workspace: root, sessionID: "write-session"}).writeFile(
		context.Background(), writeFileInput{Path: path, Content: "new"},
	)
	if err != nil || !result.OK {
		t.Fatalf("write result = %#v, err=%v", result, err)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "new" {
		t.Fatalf("content = %q, err=%v", content, err)
	}
}
