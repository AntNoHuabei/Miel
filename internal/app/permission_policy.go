package app

func approvesAutomatically(mode PermissionMode, request ApprovalRequest) bool {
	if mode == PermissionFull {
		return true
	}
	if mode != PermissionAuto || request.OutsideWorkspace {
		return false
	}
	switch request.Tool {
	case "list_directory", "read_file", "write_file", "execute_command":
		return true
	default:
		return false
	}
}
