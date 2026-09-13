package app

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/adrg/xdg"
)

// DirectoryKind identifies a managed application directory.
type DirectoryKind string

const (
	DirectoryRoot            DirectoryKind = "root"
	DirectoryLogs            DirectoryKind = "logs"
	DirectoryScreenshots     DirectoryKind = "screenshots"
	DirectorySources         DirectoryKind = "sources"
	DirectoryClipboardSource DirectoryKind = "sources.clipboard"
	DirectorySkills          DirectoryKind = "skills"
	DirectoryOutputs         DirectoryKind = "outputs"
	DirectoryOutputReports   DirectoryKind = "outputs.reports"
	DirectoryOutputDocuments DirectoryKind = "outputs.documents"
	DirectoryOutputTables    DirectoryKind = "outputs.tables"
	DirectoryOutputMemories  DirectoryKind = "outputs.memories"
	DirectoryAttachments     DirectoryKind = "attachments"
	DirectoryChatAttachments DirectoryKind = "attachments.chat"
	DirectoryChatDrafts      DirectoryKind = "attachments.chat.drafts"
	DirectoryChatFiles       DirectoryKind = "attachments.chat.files"
	DirectoryChatThumbnails  DirectoryKind = "attachments.chat.thumbnails"
)

// DirectoryPaths is the stable, inspectable path contract for the application.
// Database files intentionally remain at the root for compatibility with prior releases.
type DirectoryPaths struct {
	Root             string `json:"root"`
	Database         string `json:"database"`
	AGUIDatabase     string `json:"aguiDatabase"`
	MemoryDatabase   string `json:"memoryDatabase"`
	Logs             string `json:"logs"`
	LogFile          string `json:"logFile"`
	Screenshots      string `json:"screenshots"`
	Sources          string `json:"sources"`
	ClipboardSources string `json:"clipboardSources"`
	Skills           string `json:"skills"`
	Outputs          string `json:"outputs"`
	Reports          string `json:"reports"`
	Documents        string `json:"documents"`
	Tables           string `json:"tables"`
	Memories         string `json:"memories"`
	Attachments      string `json:"attachments"`
	ChatAttachments  string `json:"chatAttachments"`
	ChatDrafts       string `json:"chatDrafts"`
	ChatFiles        string `json:"chatFiles"`
	ChatThumbnails   string `json:"chatThumbnails"`
}

// DirectoryManager owns all persistent application paths and their creation.
type DirectoryManager struct {
	root string
}

// NewDirectoryManager creates a manager rooted at root. An empty root uses the platform data home.
func NewDirectoryManager(root string) *DirectoryManager {
	if strings.TrimSpace(root) == "" {
		root = filepath.Join(xdg.DataHome, "BlankMind")
	}
	return &DirectoryManager{root: filepath.Clean(root)}
}

var appDirectories = NewDirectoryManager("")

// DefaultDirectoryManager returns the process-wide manager used by application services.
func DefaultDirectoryManager() *DirectoryManager { return appDirectories }

// SetDirectoryManager replaces the process-wide manager. It is primarily useful for tests.
func SetDirectoryManager(manager *DirectoryManager) {
	if manager != nil {
		appDirectories = manager
	}
}

// Root returns the application data root.
func (d *DirectoryManager) Root() string { return d.root }

// DatabasePath returns a database file path while keeping database files at the legacy root.
func (d *DirectoryManager) DatabasePath(name string) string {
	return filepath.Join(d.root, filepath.Base(strings.TrimSpace(name)))
}

// LogFilePath returns the append-only application log path.
func (d *DirectoryManager) LogFilePath() string {
	return filepath.Join(d.Path(DirectoryLogs), "blankmind.log")
}

// OutputPath returns a path under a validated output category.
func (d *DirectoryManager) OutputPath(category, filename string) (string, error) {
	if strings.TrimSpace(filename) == "" {
		return "", errors.New("产物文件名不能为空")
	}
	cleanName := filepath.Base(filename)
	if cleanName == "." || cleanName == string(filepath.Separator) || cleanName != filename {
		return "", errors.New("产物文件名不可包含目录")
	}
	dir, err := d.OutputDirectory(category)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, cleanName), nil
}

// OutputDirectory returns and creates a validated output category directory.
func (d *DirectoryManager) OutputDirectory(category string) (string, error) {
	category = strings.TrimSpace(category)
	if category == "" {
		return "", errors.New("产物目录不能为空")
	}
	dir := filepath.Join(d.Path(DirectoryOutputs), filepath.Clean(category))
	if !isWithin(d.Path(DirectoryOutputs), dir) {
		return "", errors.New("产物目录无效")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建产物目录失败: %w", err)
	}
	return dir, nil
}

// Path resolves a managed directory without creating it.
func (d *DirectoryManager) Path(kind DirectoryKind) string {
	switch kind {
	case DirectoryRoot:
		return d.root
	case DirectoryLogs:
		return filepath.Join(d.root, "logs")
	case DirectoryScreenshots:
		return filepath.Join(d.root, "screenshots")
	case DirectorySources:
		return filepath.Join(d.root, "sources")
	case DirectoryClipboardSource:
		return filepath.Join(d.root, "sources", "clipboard")
	case DirectorySkills:
		return filepath.Join(d.root, "skills")
	case DirectoryOutputs:
		return filepath.Join(d.root, "outputs")
	case DirectoryOutputReports:
		return filepath.Join(d.root, "outputs", "reports")
	case DirectoryOutputDocuments:
		return filepath.Join(d.root, "outputs", "documents")
	case DirectoryOutputTables:
		return filepath.Join(d.root, "outputs", "tables")
	case DirectoryOutputMemories:
		return filepath.Join(d.root, "outputs", "memories")
	case DirectoryAttachments:
		return filepath.Join(d.root, "attachments")
	case DirectoryChatAttachments:
		return filepath.Join(d.root, "attachments", "chat")
	case DirectoryChatDrafts:
		return filepath.Join(d.root, "attachments", "chat", ".draft")
	case DirectoryChatFiles:
		return filepath.Join(d.root, "attachments", "chat", "files")
	case DirectoryChatThumbnails:
		return filepath.Join(d.root, "attachments", "chat", "thumbnails")
	default:
		return d.root
	}
}

// Ensure creates one managed directory and returns its path.
func (d *DirectoryManager) Ensure(kind DirectoryKind) (string, error) {
	path := d.Path(kind)
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", fmt.Errorf("创建目录 %s 失败: %w", path, err)
	}
	return path, nil
}

// EnsureAll creates the stable directory layout used by runtime services.
func (d *DirectoryManager) EnsureAll() error {
	for _, kind := range []DirectoryKind{
		DirectoryRoot, DirectoryLogs, DirectoryScreenshots, DirectorySources,
		DirectoryClipboardSource, DirectorySkills, DirectoryOutputs,
		DirectoryOutputReports, DirectoryOutputDocuments, DirectoryOutputTables,
		DirectoryOutputMemories, DirectoryAttachments, DirectoryChatAttachments,
		DirectoryChatDrafts, DirectoryChatFiles, DirectoryChatThumbnails,
	} {
		if _, err := d.Ensure(kind); err != nil {
			return err
		}
	}
	return nil
}

// Paths returns all important paths for diagnostics and UI integration.
func (d *DirectoryManager) Paths() DirectoryPaths {
	return DirectoryPaths{
		Root: d.Root(), Database: d.DatabasePath("blankmind.db"), AGUIDatabase: d.DatabasePath("agui.db"), MemoryDatabase: d.DatabasePath("memory.db"),
		Logs: d.Path(DirectoryLogs), LogFile: d.LogFilePath(), Screenshots: d.Path(DirectoryScreenshots), Sources: d.Path(DirectorySources), ClipboardSources: d.Path(DirectoryClipboardSource), Skills: d.Path(DirectorySkills),
		Outputs: d.Path(DirectoryOutputs), Reports: d.Path(DirectoryOutputReports), Documents: d.Path(DirectoryOutputDocuments), Tables: d.Path(DirectoryOutputTables), Memories: d.Path(DirectoryOutputMemories),
		Attachments: d.Path(DirectoryAttachments), ChatAttachments: d.Path(DirectoryChatAttachments), ChatDrafts: d.Path(DirectoryChatDrafts), ChatFiles: d.Path(DirectoryChatFiles), ChatThumbnails: d.Path(DirectoryChatThumbnails),
	}
}

// ConfigureLogging sends the standard logger to both stderr and the managed log file.
// The returned file must be closed when the process exits.
func (d *DirectoryManager) ConfigureLogging() (io.Closer, error) {
	if _, err := d.Ensure(DirectoryLogs); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(d.LogFilePath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("打开日志文件失败: %w", err)
	}
	log.SetOutput(io.MultiWriter(os.Stderr, file))
	return file, nil
}

// OpenRoot opens the application data directory in the native file manager.
func (d *DirectoryManager) OpenRoot() error {
	if _, err := d.Ensure(DirectoryRoot); err != nil {
		return err
	}
	if err := exec.Command("explorer", d.Root()).Start(); err != nil {
		return fmt.Errorf("打开数据目录失败: %w", err)
	}
	return nil
}

func isWithin(root, candidate string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
