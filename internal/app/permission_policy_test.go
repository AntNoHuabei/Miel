package app

import "testing"

func TestApprovesAutomaticallyUsesWorkspaceBoundary(t *testing.T) {
	tests := []struct {
		name    string
		mode    PermissionMode
		request ApprovalRequest
		want    bool
	}{
		{name: "ask keeps workspace reads gated", mode: PermissionAsk, request: ApprovalRequest{Tool: "read_file"}},
		{name: "auto allows workspace directory listing", mode: PermissionAuto, request: ApprovalRequest{Tool: "list_directory"}, want: true},
		{name: "auto allows workspace writes", mode: PermissionAuto, request: ApprovalRequest{Tool: "write_file"}, want: true},
		{name: "auto allows workspace commands", mode: PermissionAuto, request: ApprovalRequest{Tool: "execute_command"}, want: true},
		{name: "auto gates outside directory listing", mode: PermissionAuto, request: ApprovalRequest{Tool: "list_directory", OutsideWorkspace: true}},
		{name: "auto gates outside commands", mode: PermissionAuto, request: ApprovalRequest{Tool: "execute_command", OutsideWorkspace: true}},
		{name: "auto gates network access", mode: PermissionAuto, request: ApprovalRequest{Tool: "fetch_url"}},
		{name: "auto gates unknown tools", mode: PermissionAuto, request: ApprovalRequest{Tool: "unknown_tool"}},
		{name: "full allows network access", mode: PermissionFull, request: ApprovalRequest{Tool: "fetch_url"}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := approvesAutomatically(tt.mode, tt.request); got != tt.want {
				t.Fatalf("approvesAutomatically(%q, %#v) = %v, want %v", tt.mode, tt.request, got, tt.want)
			}
		})
	}
}
