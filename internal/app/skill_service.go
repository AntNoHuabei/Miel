package app

import (
	"archive/zip"
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	agentskill "trpc.group/trpc-go/trpc-agent-go/skill"
)

const maxSkillPackageBytes = 20 << 20

// Skill is the user-visible metadata for one installed skill.
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"` // builtin, local, or skillhub
	Enabled     bool   `json:"enabled"`
	Path        string `json:"path"`
	UpdatedAt   int64  `json:"updatedAt"`
}

// SkillHubSkill is a public catalog entry returned by SkillHub.
type SkillHubSkill struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description_zh"`
	Fallback    string `json:"description"`
	Category    string `json:"category"`
	Downloads   int64  `json:"downloads"`
	Stars       int64  `json:"stars"`
	Version     string `json:"version"`
	IconURL     string `json:"iconUrl"`
	Verified    bool   `json:"verified"`
}

type SkillHubPage struct {
	Skills []SkillHubSkill `json:"skills"`
	Total  int             `json:"total"`
	Page   int             `json:"page"`
	Pages  int             `json:"pages"`
}

type skillInstallMeta struct {
	Source    string `json:"source"`
	Imported  int64  `json:"importedAt"`
	SourceURL string `json:"sourceUrl,omitempty"`
}

type skillState struct {
	Enabled map[string]bool `json:"enabled"`
}

// SkillService manages documents under the application skills directory.
type SkillService struct {
	manager      *DirectoryManager
	client       *http.Client
	picker       func() (string, error)
	dependencies *SkillDependencyService
}

func NewSkillService(manager *DirectoryManager) *SkillService {
	if manager == nil {
		manager = DefaultDirectoryManager()
	}
	return &SkillService{manager: manager, client: &http.Client{Timeout: 30 * time.Second}}
}

// BindSkillPicker connects the native file dialog without exposing platform details to Wails.
func BindSkillPicker(service *SkillService, picker interface{ PickSkillPath() (string, error) }) {
	if service != nil && picker != nil {
		service.picker = picker.PickSkillPath
	}
}

// PickLocal opens the native picker and imports the selected directory or ZIP.
func (s *SkillService) PickLocal() (Skill, error) {
	if s == nil || s.picker == nil {
		return Skill{}, errors.New("本地技能选择器未初始化")
	}
	path, err := s.picker()
	if err != nil || strings.TrimSpace(path) == "" {
		return Skill{}, err
	}
	return s.ImportLocal(path)
}

func (s *SkillService) skillsRoot() (string, error) {
	if s == nil || s.manager == nil {
		return "", errors.New("技能服务未初始化")
	}
	return s.manager.Ensure(DirectorySkills)
}

// ReconcileDependencyState keeps unconfirmed executable skills out of the Agent repository.
func (s *SkillService) ReconcileDependencyState() error {
	if s == nil || s.dependencies == nil {
		return nil
	}
	root, err := s.skillsRoot()
	if err != nil {
		return err
	}
	state, err := s.loadState(root)
	if err != nil {
		return err
	}
	changed := false
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || isBuiltinSkill(entry.Name()) {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		if _, err := os.Stat(filepath.Join(dir, skillManifestFile)); err == nil {
			continue
		}
		plan := detectSkillDependencies(dir, normalizeSkillName(entry.Name()))
		enabled, recorded := state.Enabled[plan.Skill]
		automatic := plan.Confidence == "high" && plan.EntryCommand != "" && len(plan.EntryArgs) > 0
		if plan.Runtime != "" && !automatic && (!recorded || enabled) {
			state.Enabled[plan.Skill] = false
			changed = true
		}
	}
	if changed {
		return s.saveState(root, state)
	}
	return nil
}

func (s *SkillService) ListSkills() ([]Skill, error) {
	root, err := s.skillsRoot()
	if err != nil {
		return nil, err
	}
	state, err := s.loadState(root)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("读取技能目录失败: %w", err)
	}
	result := make([]Skill, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		skill, ok, readErr := readSkillDirectory(filepath.Join(root, entry.Name()))
		if readErr != nil {
			continue
		}
		if !ok {
			continue
		}
		if value, exists := state.Enabled[skill.Name]; exists {
			skill.Enabled = value
		}
		result = append(result, skill)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

// ListSkillHubSkills returns a public, searchable page from the SkillHub catalog.
func (s *SkillService) ListSkillHubSkills(page, pageSize int, keyword string) (SkillHubPage, error) {
	if s == nil || s.client == nil {
		return SkillHubPage{}, errors.New("技能服务未初始化")
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 50 {
		pageSize = 12
	}
	query := url.Values{}
	query.Set("page", strconv.Itoa(page))
	query.Set("pageSize", strconv.Itoa(pageSize))
	if keyword = strings.TrimSpace(keyword); keyword != "" {
		query.Set("keyword", keyword)
	}
	endpoint := "https://api.skillhub.cn/api/skills?" + query.Encode()
	resp, err := s.client.Get(endpoint)
	if err != nil {
		return SkillHubPage{}, fmt.Errorf("加载 SkillHub 列表失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return SkillHubPage{}, fmt.Errorf("SkillHub 列表返回 HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Skills []SkillHubSkill `json:"skills"`
			Total  int             `json:"total"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 5<<20)).Decode(&payload); err != nil {
		return SkillHubPage{}, fmt.Errorf("解析 SkillHub 列表失败: %w", err)
	}
	if payload.Code != 0 {
		return SkillHubPage{}, fmt.Errorf("SkillHub 列表错误: %s", payload.Message)
	}
	for i := range payload.Data.Skills {
		if payload.Data.Skills[i].Description == "" {
			payload.Data.Skills[i].Description = payload.Data.Skills[i].Fallback
		}
	}
	pages := 0
	if payload.Data.Total > 0 {
		pages = (payload.Data.Total + pageSize - 1) / pageSize
	}
	return SkillHubPage{Skills: payload.Data.Skills, Total: payload.Data.Total, Page: page, Pages: pages}, nil
}

// InstallSkillHub installs a catalog entry by its public slug.
func (s *SkillService) InstallSkillHub(slug string) (Skill, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" || strings.ContainsAny(slug, "/\\") {
		return Skill{}, errors.New("SkillHub skill slug 无效")
	}
	return s.ImportFromSkillHub("https://api.skillhub.cn/api/v1/download?slug=" + url.QueryEscape(slug))
}

func (s *SkillService) SetEnabled(name string, enabled bool) error {
	name = normalizeSkillName(name)
	if name == "" {
		return errors.New("技能名称不能为空")
	}
	root, err := s.skillsRoot()
	if err != nil {
		return err
	}
	if _, ok, err := readSkillDirectory(filepath.Join(root, name)); err != nil || !ok {
		if err != nil {
			return err
		}
		return errors.New("技能不存在")
	}
	if enabled && s.dependencies != nil {
		plan, inspectErr := s.dependencies.InspectSkillDependencies(name)
		if inspectErr != nil {
			return inspectErr
		}
		if plan.NeedsReview {
			return errors.New("Skill 依赖需要确认后才能启用")
		}
		if plan.Runtime != "" {
			if _, initErr := s.dependencies.InitializeSkill(name); initErr != nil {
				return initErr
			}
		}
	}
	state, err := s.loadState(root)
	if err != nil {
		return err
	}
	state.Enabled[name] = enabled
	return s.saveState(root, state)
}

func (s *SkillService) Delete(name string) error {
	name = normalizeSkillName(name)
	if name == "" || isBuiltinSkill(name) {
		return errors.New("内置技能不可删除")
	}
	root, err := s.skillsRoot()
	if err != nil {
		return err
	}
	dir := filepath.Join(root, name)
	if _, ok, readErr := readSkillDirectory(dir); readErr != nil {
		return readErr
	} else if !ok {
		return errors.New("技能不存在")
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("删除技能失败: %w", err)
	}
	if s.dependencies != nil {
		_ = s.dependencies.RemoveSkillEnvironment(name)
	}
	state, err := s.loadState(root)
	if err == nil {
		delete(state.Enabled, name)
		return s.saveState(root, state)
	}
	return nil
}

// ImportLocal imports a directory containing SKILL.md or a ZIP archive.
func (s *SkillService) ImportLocal(path string) (Skill, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "." || path == "" {
		return Skill{}, errors.New("技能路径不能为空")
	}
	info, err := os.Stat(path)
	if err != nil {
		return Skill{}, fmt.Errorf("读取技能路径失败: %w", err)
	}
	if info.IsDir() {
		return s.installDirectory(path, "local", "")
	}
	if strings.EqualFold(filepath.Ext(path), ".zip") {
		return s.installZip(path, "local", "")
	}
	return Skill{}, errors.New("本地技能需要选择包含 SKILL.md 的目录或 ZIP 文件")
}

// ImportFromSkillHub accepts a SkillHub detail/download URL. The response may be SKILL.md, JSON, or ZIP.
func (s *SkillService) ImportFromSkillHub(url string) (Skill, error) {
	url = strings.TrimSpace(url)
	if !strings.HasPrefix(strings.ToLower(url), "https://") && !strings.HasPrefix(strings.ToLower(url), "http://") {
		return Skill{}, errors.New("SkillHub 地址必须是 http(s) URL")
	}
	if s == nil || s.client == nil {
		return Skill{}, errors.New("技能服务未初始化")
	}
	url = normalizeSkillHubDownloadURL(url)
	resp, err := s.client.Get(url)
	if err != nil {
		return Skill{}, fmt.Errorf("下载 SkillHub 技能失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Skill{}, fmt.Errorf("SkillHub 返回 HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSkillPackageBytes+1))
	if err != nil {
		return Skill{}, fmt.Errorf("读取 SkillHub 技能失败: %w", err)
	}
	if len(body) > maxSkillPackageBytes {
		return Skill{}, errors.New("SkillHub 技能超过 20 MB 限制")
	}
	if strings.EqualFold(filepath.Ext(strings.Split(url, "?")[0]), ".zip") || strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "zip") || (len(body) >= 4 && string(body[:4]) == "PK\x03\x04") {
		return s.installZipBytes(body, "skillhub", url)
	}
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "json") || strings.HasPrefix(strings.TrimSpace(string(body)), "{") {
		var payload struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Content     string `json:"content"`
			SkillMD     string `json:"skillMd"`
			DownloadURL string `json:"downloadUrl"`
		}
		if json.Unmarshal(body, &payload) == nil {
			if payload.Content == "" {
				payload.Content = payload.SkillMD
			}
			if payload.Content != "" {
				return s.installMarkdown(payload.Name, payload.Description, payload.Content, "skillhub", url)
			}
			if payload.DownloadURL != "" && payload.DownloadURL != url {
				return s.ImportFromSkillHub(payload.DownloadURL)
			}
		}
	}
	return s.installMarkdown("", "", string(body), "skillhub", url)
}

func normalizeSkillHubDownloadURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || !strings.Contains(strings.ToLower(parsed.Host), "skillhub") {
		return raw
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for i, part := range parts {
		if strings.EqualFold(part, "skills") && i+1 < len(parts) && parts[i+1] != "" {
			return "https://api.skillhub.cn/api/v1/download?slug=" + url.QueryEscape(parts[i+1])
		}
	}
	return raw
}

func (s *SkillService) installDirectory(source, origin, sourceURL string) (Skill, error) {
	content, err := os.ReadFile(filepath.Join(source, "SKILL.md"))
	if err != nil {
		return Skill{}, errors.New("技能目录必须包含 SKILL.md")
	}
	name, description := parseSkillDocument(string(content))
	if name == "" {
		name = filepath.Base(source)
	}
	installed, err := s.installMarkdown(name, description, string(content), origin, sourceURL)
	if err != nil {
		return Skill{}, err
	}
	if err := copySkillFiles(source, installed.Path); err != nil {
		return Skill{}, err
	}
	return s.finalizeImportedSkill(installed)
}

func (s *SkillService) finalizeImportedSkill(installed Skill) (Skill, error) {
	if s.dependencies == nil {
		return installed, nil
	}
	plan, err := s.dependencies.InspectSkillDependencies(installed.Name)
	if err != nil {
		return Skill{}, err
	}
	if plan.Runtime == "" {
		return installed, nil
	}
	root, err := s.skillsRoot()
	if err != nil {
		return Skill{}, err
	}
	state, err := s.loadState(root)
	if err != nil {
		return Skill{}, err
	}
	state.Enabled[installed.Name] = false
	if err := s.saveState(root, state); err != nil {
		return Skill{}, err
	}
	installed.Enabled = false
	return installed, nil
}

func (s *SkillService) installMarkdown(name, description, content, origin, sourceURL string) (Skill, error) {
	parsedName, parsedDescription := parseSkillDocument(content)
	if parsedName != "" {
		name = parsedName
	}
	if parsedDescription != "" {
		description = parsedDescription
	}
	if strings.TrimSpace(name) == "" {
		for _, line := range strings.Split(content, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "# ") {
				name = strings.TrimSpace(strings.TrimPrefix(line, "# "))
				break
			}
		}
	}
	name = normalizeSkillName(name)
	if name == "" || name == "." {
		return Skill{}, errors.New("SKILL.md 缺少有效名称")
	}
	if isBuiltinSkill(name) {
		return Skill{}, errors.New("不能覆盖内置技能")
	}
	root, err := s.skillsRoot()
	if err != nil {
		return Skill{}, err
	}
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Skill{}, fmt.Errorf("创建技能目录失败: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		return Skill{}, fmt.Errorf("保存 SKILL.md 失败: %w", err)
	}
	meta := skillInstallMeta{Source: origin, Imported: time.Now().Unix(), SourceURL: sourceURL}
	metaBytes, _ := json.MarshalIndent(meta, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, ".blankmind.json"), metaBytes, 0o644)
	return Skill{Name: name, Description: description, Source: origin, Enabled: true, Path: dir, UpdatedAt: time.Now().Unix()}, nil
}

func (s *SkillService) installZip(path, origin, sourceURL string) (Skill, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Skill{}, err
	}
	if info.Size() > maxSkillPackageBytes {
		return Skill{}, errors.New("技能 ZIP 超过 20 MB 限制")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, err
	}
	return s.installZipBytes(data, origin, sourceURL)
}

func (s *SkillService) installZipBytes(data []byte, origin, sourceURL string) (Skill, error) {
	if len(data) > maxSkillPackageBytes {
		return Skill{}, errors.New("技能 ZIP 超过 20 MB 限制")
	}
	tmp, err := os.CreateTemp("", "blankmind-skill-*.zip")
	if err != nil {
		return Skill{}, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return Skill{}, err
	}
	if err := tmp.Close(); err != nil {
		return Skill{}, err
	}
	archive, err := zip.OpenReader(tmpPath)
	if err != nil {
		return Skill{}, errors.New("无效的技能 ZIP 文件")
	}
	defer archive.Close()
	var skillFile *zip.File
	totalSize := int64(0)
	for _, file := range archive.File {
		name := filepath.ToSlash(filepath.Clean(file.Name))
		if name == "SKILL.md" || strings.HasSuffix(name, "/SKILL.md") {
			if skillFile == nil || strings.Count(name, "/") < strings.Count(skillFile.Name, "/") {
				skillFile = file
			}
		}
		if strings.HasPrefix(name, "../") || filepath.IsAbs(name) {
			return Skill{}, errors.New("技能 ZIP 包含无效路径")
		}
		if !file.FileInfo().IsDir() {
			if file.UncompressedSize64 > uint64(maxSkillPackageBytes) || totalSize > int64(maxSkillPackageBytes)-int64(file.UncompressedSize64) {
				return Skill{}, errors.New("技能 ZIP 超过 20 MB 限制")
			}
			totalSize += int64(file.UncompressedSize64)
		}
	}
	if skillFile == nil {
		return Skill{}, errors.New("技能 ZIP 中未找到 SKILL.md")
	}
	tmpDir, err := os.MkdirTemp("", "blankmind-skill-extract-")
	if err != nil {
		return Skill{}, err
	}
	defer os.RemoveAll(tmpDir)
	for _, file := range archive.File {
		name := filepath.ToSlash(filepath.Clean(file.Name))
		if name == "." || file.FileInfo().IsDir() {
			continue
		}
		target := filepath.Join(tmpDir, filepath.FromSlash(name))
		if !isWithin(tmpDir, target) {
			return Skill{}, errors.New("技能 ZIP 包含无效路径")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return Skill{}, err
		}
		rc, openErr := file.Open()
		if openErr != nil {
			return Skill{}, openErr
		}
		data, readErr := io.ReadAll(io.LimitReader(rc, maxSkillPackageBytes+1))
		rc.Close()
		if readErr != nil {
			return Skill{}, readErr
		}
		if len(data) > maxSkillPackageBytes {
			return Skill{}, errors.New("技能 ZIP 文件过大")
		}
		if writeErr := os.WriteFile(target, data, 0o644); writeErr != nil {
			return Skill{}, writeErr
		}
	}
	return s.installDirectory(filepath.Dir(filepath.Join(tmpDir, filepath.FromSlash(skillFile.Name))), origin, sourceURL)
}

func copySkillFiles(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == source || entry.Name() == ".blankmind.json" || entry.Name() == ".git" {
			if entry.IsDir() && path != source {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, rel)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(destination, data, 0o644)
	})
}

func readSkillDirectory(dir string) (Skill, bool, error) {
	content, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if errors.Is(err, os.ErrNotExist) {
		return Skill{}, false, nil
	}
	if err != nil {
		return Skill{}, false, err
	}
	name, description := parseSkillDocument(string(content))
	if name == "" {
		name = filepath.Base(dir)
	}
	source := "local"
	if isBuiltinSkill(name) {
		source = "builtin"
	} else if metaBytes, readErr := os.ReadFile(filepath.Join(dir, ".blankmind.json")); readErr == nil {
		var meta skillInstallMeta
		if json.Unmarshal(metaBytes, &meta) == nil && meta.Source != "" {
			source = meta.Source
		}
	}
	info, _ := os.Stat(filepath.Join(dir, "SKILL.md"))
	updated := int64(0)
	if info != nil {
		updated = info.ModTime().Unix()
	}
	return Skill{Name: normalizeSkillName(name), Description: description, Source: source, Enabled: true, Path: dir, UpdatedAt: updated}, true, nil
}

func parseSkillDocument(content string) (name, description string) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	inFrontmatter := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "---" {
			if inFrontmatter {
				break
			}
			inFrontmatter = true
			continue
		}
		if !inFrontmatter {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), "\"'")
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "name":
			name = value
		case "description":
			description = value
		}
	}
	return strings.TrimSpace(name), strings.TrimSpace(description)
}

func normalizeSkillName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, " ", "-")
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "-_")
}

func isBuiltinSkill(name string) bool {
	switch normalizeSkillName(name) {
	case "todo", "reminder", "office", "websearch":
		return true
	default:
		return false
	}
}

func (s *SkillService) loadState(root string) (skillState, error) {
	state := skillState{Enabled: map[string]bool{}}
	data, err := os.ReadFile(filepath.Join(root, ".skill-state.json"))
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, fmt.Errorf("读取技能状态失败: %w", err)
	}
	if state.Enabled == nil {
		state.Enabled = map[string]bool{}
	}
	return state, nil
}

func (s *SkillService) saveState(root string, state skillState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, ".skill-state.json"), data, 0o644)
}

// managedSkillRepository keeps the agent view in sync with the user's enabled flags.
type managedSkillRepository struct {
	base    agentskill.Repository
	enabled map[string]bool
}

func newManagedSkillRepository(root string) (agentskill.Repository, error) {
	base, err := agentskill.NewFSRepository(root)
	if err != nil {
		return nil, err
	}
	state, err := (&SkillService{manager: NewDirectoryManager(filepath.Dir(root))}).loadState(root)
	if err != nil {
		return nil, err
	}
	return &managedSkillRepository{base: base, enabled: state.Enabled}, nil
}

func (r *managedSkillRepository) isEnabled(name string) bool {
	value, exists := r.enabled[normalizeSkillName(name)]
	return !exists || value
}

func (r *managedSkillRepository) Summaries() []agentskill.Summary {
	rows := r.base.Summaries()
	filtered := make([]agentskill.Summary, 0, len(rows))
	for _, row := range rows {
		if r.isEnabled(row.Name) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func (r *managedSkillRepository) Get(name string) (*agentskill.Skill, error) {
	if !r.isEnabled(name) {
		return nil, fmt.Errorf("skill %q is disabled", name)
	}
	return r.base.Get(name)
}

func (r *managedSkillRepository) Path(name string) (string, error) {
	if !r.isEnabled(name) {
		return "", fmt.Errorf("skill %q is disabled", name)
	}
	return r.base.Path(name)
}
