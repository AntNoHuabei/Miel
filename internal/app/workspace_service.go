package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const settingCurrentWorkspace = "workspace.current"
const workspaceNone = "__none__"

// workspaceDirectoryPicker 由桌面层注入系统目录选择器。
type workspaceDirectoryPicker func() (string, error)

var defaultWorkspaceDirectoryPicker workspaceDirectoryPicker

// SetWorkspaceDirectoryPicker 注入 Wails 原生目录选择器。
//
//wails:ignore
func (s *SettingsService) SetWorkspaceDirectoryPicker(picker workspaceDirectoryPicker) {
	defaultWorkspaceDirectoryPicker = picker
}

// BindWorkspaceDirectoryPicker adapts the desktop picker without exposing it as a Wails method.
func BindWorkspaceDirectoryPicker(service *SettingsService, picker interface{ PickWorkspaceDirectory() (string, error) }) {
	if service == nil || picker == nil {
		return
	}
	service.SetWorkspaceDirectoryPicker(picker.PickWorkspaceDirectory)
}

// ListWorkspaces 返回已添加的工作目录,当前项排最前。
func (s *SettingsService) ListWorkspaces() ([]Workspace, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("存储未初始化")
	}
	current, err := s.GetSetting(settingCurrentWorkspace)
	if err != nil {
		return nil, err
	}
	if current == workspaceNone {
		current = ""
	} else if current == "" {
		current = filepath.Clean(dataDir())
		if err := s.addWorkspacePath(current); err != nil {
			return nil, err
		}
		if err := s.SetSetting(settingCurrentWorkspace, current); err != nil {
			return nil, err
		}
	}

	rows, err := s.db.Query("SELECT path, name FROM workspaces ORDER BY CASE WHEN path = ? THEN 0 ELSE 1 END, lower(name), path", current)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Workspace{}
	for rows.Next() {
		var item Workspace
		if err := rows.Scan(&item.Path, &item.Name); err != nil {
			return nil, err
		}
		item.IsCurrent = filepath.Clean(item.Path) == filepath.Clean(current)
		items = append(items, item)
	}
	return items, rows.Err()
}

// AddWorkspace 添加一个真实存在的目录并切换到它。
func (s *SettingsService) AddWorkspace(path string) (Workspace, error) {
	clean, err := normalizeWorkspacePath(path)
	if err != nil {
		return Workspace{}, err
	}
	if err := s.addWorkspacePath(clean); err != nil {
		return Workspace{}, err
	}
	if err := s.SetSetting(settingCurrentWorkspace, clean); err != nil {
		return Workspace{}, err
	}
	return Workspace{Name: filepath.Base(clean), Path: clean, IsCurrent: true}, nil
}

// PickWorkspace 打开系统目录选择器,选择后自动添加并切换。
func (s *SettingsService) PickWorkspace() (Workspace, error) {
	if defaultWorkspaceDirectoryPicker == nil {
		return Workspace{}, errors.New("目录选择器未初始化")
	}
	path, err := defaultWorkspaceDirectoryPicker()
	if err != nil {
		return Workspace{}, err
	}
	if strings.TrimSpace(path) == "" {
		return Workspace{}, nil
	}
	return s.AddWorkspace(path)
}

// SetWorkspace 切换到已添加的工作目录;传入空串表示退出工作区。
func (s *SettingsService) SetWorkspace(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return s.SetSetting(settingCurrentWorkspace, workspaceNone)
	}
	clean, err := normalizeWorkspacePath(path)
	if err != nil {
		return err
	}
	var exists int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM workspaces WHERE path = ?", clean).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return errors.New("工作区尚未添加")
	}
	return s.SetSetting(settingCurrentWorkspace, clean)
}

// agentWorkspacePath validates the workspace supplied with a chat request
// against the user's persisted workspace list. An empty result means no
// workspace is active, so file operations remain individually gated.
func (s *SettingsService) agentWorkspacePath(path string) string {
	if s == nil || s.db == nil {
		return ""
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	clean, err := cleanWorkspacePath(path)
	if err != nil {
		return ""
	}
	var exists int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM workspaces WHERE path = ?", clean).Scan(&exists); err != nil || exists == 0 {
		return ""
	}
	return clean
}

// RemoveWorkspace 移除工作目录;不会删除磁盘上的文件。
func (s *SettingsService) RemoveWorkspace(path string) error {
	clean, err := cleanWorkspacePath(path)
	if err != nil {
		return err
	}
	if _, err := s.db.Exec("DELETE FROM workspaces WHERE path = ?", clean); err != nil {
		return err
	}
	current, err := s.GetSetting(settingCurrentWorkspace)
	if err != nil {
		return err
	}
	if filepath.Clean(current) == filepath.Clean(clean) {
		return s.SetSetting(settingCurrentWorkspace, workspaceNone)
	}
	return nil
}

func (s *SettingsService) addWorkspacePath(path string) error {
	_, err := s.db.Exec("INSERT OR IGNORE INTO workspaces (path, name, created_at) VALUES (?, ?, ?)", path, filepath.Base(path), now())
	return err
}

func normalizeWorkspacePath(path string) (string, error) {
	clean, err := cleanWorkspacePath(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(clean)
	if err != nil {
		return "", fmt.Errorf("工作区目录不可用: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("工作区必须是目录")
	}
	abs, err := filepath.Abs(clean)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func cleanWorkspacePath(path string) (string, error) {
	clean := filepath.Clean(strings.TrimSpace(path))
	if clean == "." || clean == "" {
		return "", errors.New("工作区路径不能为空")
	}
	abs, err := filepath.Abs(clean)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}
