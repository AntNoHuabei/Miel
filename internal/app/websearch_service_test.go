package app

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestWebSearchRejectsPrivateURLsAndPrivateDNS(t *testing.T) {
	service := NewWebSearchService()
	for _, raw := range []string{"file:///tmp/a", "http://127.0.0.1", "http://[::1]", "http://localhost"} {
		if _, err := service.safePublicURL(context.Background(), raw); err == nil {
			t.Errorf("%s was accepted", raw)
		}
	}
	service.lookup = func(context.Context, string, string) ([]net.IP, error) { return []net.IP{net.ParseIP("10.0.0.8")}, nil }
	if _, err := service.safePublicURL(context.Background(), "https://example.com"); err == nil || !strings.Contains(err.Error(), "内网") {
		t.Fatalf("private DNS error = %v", err)
	}
}

func TestWebSearchUsesExistingPermissionApproval(t *testing.T) {
	permissions := NewPermissionService(nil)
	ctx := WithPermissionContext(context.Background(), permissions, "websearch-session", "")
	done := make(chan error, 1)
	go func() { done <- authorizeWebSearch(ctx, "search", "search:abc") }()
	var pending ApprovalRequest
	deadline := time.After(time.Second)
	for pending.ID == "" {
		state, err := permissions.GetPermissionState()
		if err != nil {
			t.Fatal(err)
		}
		if len(state.Pending) > 0 {
			pending = state.Pending[0]
			break
		}
		select {
		case <-deadline:
			t.Fatal("websearch did not request approval")
		case <-time.After(time.Millisecond):
		}
	}
	if pending.Tool != "websearch" || pending.Operation != "search" || pending.RiskLevel != "medium" {
		t.Fatalf("approval = %#v", pending)
	}
	if err := permissions.ResolveApproval(pending.ID, ApprovalSession); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := authorizeWebSearch(ctx, "fetch", "https://example.com"); err != nil {
		t.Fatalf("session grant was not reused: %v", err)
	}
}

func TestWebSearchBuiltinContract(t *testing.T) {
	root, allowed := newBuiltinSkillCommand("websearch", nil, nil)
	if root == nil || !allowed["search"] || !allowed["fetch"] {
		t.Fatalf("builtin contract = %#v %#v", root, allowed)
	}
	if result, err := executeSkillCommand(context.Background(), nil, nil, skillRunRequest{Skill: "websearch", Command: "unknown"}); err != nil || result.ExitCode != 2 {
		t.Fatalf("unknown command = %#v err=%v", result, err)
	}
	if result, err := executeSkillCommand(context.Background(), nil, nil, skillRunRequest{Skill: "websearch", Command: "search", Args: []string{"--query", "test"}}); err == nil || !strings.Contains(err.Error(), "服务未初始化") || result.ExitCode != 0 {
		t.Fatalf("uninitialized service = %#v err=%v", result, err)
	}
}
