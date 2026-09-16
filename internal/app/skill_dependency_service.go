package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

type skillEnvironmentRecord struct {
	SkillEnvironmentStatus
	Plan SkillDependencyPlan `json:"plan"`
}

// SkillDependencyService detects, installs and runs isolated Skill environments.
type SkillDependencyService struct {
	manager        *DirectoryManager
	settings       *SettingsService
	mu             sync.Mutex
	notify         func(string, any)
	run            func(context.Context, string, []string, string, []string) ([]byte, error)
	resolveRuntime func() string
}

func NewSkillDependencyService(manager *DirectoryManager, settings *SettingsService) *SkillDependencyService {
	if manager == nil {
		manager = DefaultDirectoryManager()
	}
	return &SkillDependencyService{
		manager: manager, settings: settings,
		notify: func(name string, data any) { Emit(name, data) },
		run:    runSkillProcess, resolveRuntime: installedRuntimeRoot,
	}
}

func (s *SkillDependencyService) setNotify(fn func(string, any)) {
	if fn != nil {
		s.notify = fn
	}
}

func installedRuntimeRoot() string {
	configured := strings.TrimSpace(os.Getenv("MIEL_RUNTIME_ROOT"))
	if configured == "" {
		configured = readLocalEnvValue(".env.local", "MIEL_RUNTIME_ROOT")
	}
	if configured != "" {
		if absolute, err := filepath.Abs(configured); err == nil {
			return filepath.Clean(absolute)
		}
		return filepath.Clean(configured)
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(exe), "runtime")
}

func readLocalEnvValue(path, key string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, found := strings.Cut(line, "=")
		if found && strings.TrimSpace(name) == key {
			return strings.Trim(strings.TrimSpace(value), "\"'")
		}
	}
	return ""
}

func (s *SkillDependencyService) ListRuntimeStatus() []SkillRuntimeStatus {
	root := s.resolveRuntime()
	return []SkillRuntimeStatus{
		makeRuntimeStatus("node", filepath.Join(root, "node", "node.exe")),
		makeRuntimeStatus("python", filepath.Join(root, "python", "python.exe")),
		makeRuntimeStatus("uv", filepath.Join(root, "uv", "uv.exe")),
	}
}

func makeRuntimeStatus(name, path string) SkillRuntimeStatus {
	status := SkillRuntimeStatus{Name: name, Version: readRuntimeVersion(filepath.Dir(path)), Path: path}
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		status.Available = true
	} else {
		status.Message = "内置运行时未释放"
	}
	return status
}

func (s *SkillDependencyService) InspectSkillDependencies(name string) (SkillDependencyPlan, error) {
	dir, err := s.skillDirectory(name)
	if err != nil {
		return SkillDependencyPlan{}, err
	}
	plan := resolveSkillCapability(dir, normalizeSkillName(name))
	s.notify("skill.dependency.detected", plan)
	if plan.NeedsReview {
		s.notify("skill.dependency.confirmation-required", plan)
	}
	return plan, nil
}

// GenerateSkillDependencyPlan asks the configured model for a reviewable draft.
// It never writes the draft or installs dependencies.
func (s *SkillDependencyService) GenerateSkillDependencyPlan(name string) (SkillDependencyPlan, error) {
	dir, err := s.skillDirectory(name)
	if err != nil {
		return SkillDependencyPlan{}, err
	}
	detected := detectSkillDependencies(dir, normalizeSkillName(name))
	if s.settings == nil {
		return detected, errors.New("模型设置未初始化")
	}
	provider, err := s.settings.DefaultProvider()
	if err != nil {
		return detected, errors.New("尚未配置默认模型，无法生成依赖草案")
	}
	llm, err := buildModel(provider)
	if err != nil {
		return detected, err
	}
	prompt := "分析本地 Skill 的依赖证据，只返回 JSON，不要 Markdown。不得建议 shell 命令或安装后脚本。" +
		"字段必须为 runtime(node/python/空)、packageManager(npm/uv/空)、lockfile、dependencyFile、dependencySources、entryCommand、entryArgs、network。" +
		" skill=" + detected.Skill + " evidence=" + strings.Join(detected.Evidence, ", ") +
		" snippets=" + skillEvidenceSnippets(dir, detected.Evidence)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	responses, err := llm.GenerateContent(ctx, model.NewRequest([]model.Message{model.NewUserMessage(prompt)}))
	if err != nil {
		return detected, err
	}
	var output strings.Builder
	for response := range responses {
		if response.Error != nil {
			return detected, fmt.Errorf("模型生成依赖草案失败: %v", response.Error)
		}
		for _, choice := range response.Choices {
			if choice.Delta.Content != "" {
				output.WriteString(choice.Delta.Content)
			} else {
				output.WriteString(choice.Message.Content)
			}
		}
	}
	var draft SkillDependencyPlan
	text := stripJSONFence(output.String())
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &draft); err != nil {
		return detected, fmt.Errorf("模型未返回有效依赖 JSON: %w", err)
	}
	draft.Skill, draft.SchemaVersion, draft.Source = detected.Skill, skillManifestSchemaVersion, "ai"
	draft.Evidence, draft.Confidence, draft.NeedsReview = detected.Evidence, "ai", true
	normalizePythonPlan(&draft)
	normalizePlanSlices(&draft)
	if err := validateDependencyPlan(draft); err != nil {
		return detected, err
	}
	s.notify("skill.dependency.confirmation-required", draft)
	return draft, nil
}

func (s *SkillDependencyService) ConfirmSkillDependencyPlan(name string, plan SkillDependencyPlan) (SkillDependencyPlan, error) {
	dir, err := s.skillDirectory(name)
	if err != nil {
		return SkillDependencyPlan{}, err
	}
	plan.Skill, plan.SchemaVersion, plan.NeedsReview = normalizeSkillName(name), skillManifestSchemaVersion, false
	plan.Runtime = strings.ToLower(strings.TrimSpace(plan.Runtime))
	plan.PackageManager = strings.ToLower(strings.TrimSpace(plan.PackageManager))
	normalizePythonPlan(&plan)
	normalizePlanSlices(&plan)
	if plan.Source == "" {
		plan.Source = "confirmed"
	}
	if err := validateDependencyPlan(plan); err != nil {
		return SkillDependencyPlan{}, err
	}
	if plan.EntryCommand != "" {
		entry := filepath.Join(dir, plan.EntryArgs[0])
		info, statErr := os.Stat(entry)
		if statErr != nil || info.IsDir() {
			return SkillDependencyPlan{}, errors.New("Skill 入口文件不存在")
		}
	}
	if err := writeSkillManifest(filepath.Join(dir, skillManifestFile), plan); err != nil {
		return SkillDependencyPlan{}, err
	}
	return resolveSkillCapability(dir, normalizeSkillName(name)), nil
}

func normalizePlanSlices(plan *SkillDependencyPlan) {
	plan.Evidence = nonNilStrings(plan.Evidence)
	plan.DependencySources = nonNilStrings(plan.DependencySources)
	plan.EntryArgs = nonNilStrings(plan.EntryArgs)
}

func validateDependencyPlan(plan SkillDependencyPlan) error {
	runtimeName := strings.ToLower(strings.TrimSpace(plan.Runtime))
	manager := strings.ToLower(strings.TrimSpace(plan.PackageManager))
	if runtimeName != "" && runtimeName != "node" && runtimeName != "python" {
		return errors.New("仅支持 node、python 或空运行时")
	}
	if manager != "" && !((runtimeName == "node" && manager == "npm") || (runtimeName == "python" && manager == "uv")) {
		return errors.New("Skill 包管理器与运行时不匹配")
	}
	if err := validateRelativeSkillFile(plan.Lockfile); err != nil {
		return err
	}
	if err := validateRelativeSkillFile(plan.DependencyFile); err != nil {
		return err
	}
	for _, source := range plan.DependencySources {
		if err := validateRelativeSkillFile(source); err != nil {
			return err
		}
	}
	if plan.EntryCommand != "" {
		if strings.ToLower(strings.TrimSpace(plan.EntryCommand)) != runtimeName {
			return errors.New("Skill 入口必须使用声明的内置运行时")
		}
		if len(plan.EntryArgs) == 0 || strings.HasPrefix(plan.EntryArgs[0], "-") {
			return errors.New("Skill 入口必须以 Skill 内脚本文件开头")
		}
		if err := validateRelativeSkillFile(plan.EntryArgs[0]); err != nil {
			return err
		}
	}
	return nil
}

func normalizePythonPlan(plan *SkillDependencyPlan) {
	if plan.Runtime != "python" {
		return
	}
	if plan.PackageManager == "pip" || plan.PackageManager == "" {
		plan.PackageManager = "uv"
	}
	plan.AutoUpdate = true
	if strings.EqualFold(filepath.Base(plan.DependencyFile), "requirements.txt") {
		if plan.Lockfile == "" {
			plan.Lockfile = managedRequirementsLock
		}
		if len(plan.DependencySources) == 0 {
			plan.DependencySources = []string{plan.DependencyFile}
		}
	} else if strings.EqualFold(filepath.Base(plan.DependencyFile), "pyproject.toml") {
		if plan.Lockfile == "" {
			plan.Lockfile = "uv.lock"
		}
		if len(plan.DependencySources) == 0 {
			plan.DependencySources = []string{plan.DependencyFile}
		}
	}
}

func (s *SkillDependencyService) GetSkillEnvironmentStatus(name string) (SkillEnvironmentStatus, error) {
	dir, err := s.skillDirectory(name)
	if err != nil {
		return SkillEnvironmentStatus{}, err
	}
	capability := resolveSkillCapability(dir, normalizeSkillName(name))
	if capability.Kind == "instruction" || capability.Kind == "builtin" {
		return SkillEnvironmentStatus{Skill: name, State: "not-required"}, nil
	}
	if capability.Kind == "unresolved" {
		return SkillEnvironmentStatus{Skill: name, State: "needs-review", Error: capability.Reason}, nil
	}
	data, err := os.ReadFile(filepath.Join(dir, ".blankmind.skill-state.json"))
	if errors.Is(err, os.ErrNotExist) {
		plan, planErr := s.InspectSkillDependencies(name)
		if planErr != nil {
			return SkillEnvironmentStatus{}, planErr
		}
		state := "none"
		if plan.NeedsReview {
			state = "needs-review"
		}
		return SkillEnvironmentStatus{Skill: normalizeSkillName(name), State: state, Runtime: plan.Runtime}, nil
	}
	if err != nil {
		return SkillEnvironmentStatus{}, err
	}
	var record skillEnvironmentRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return SkillEnvironmentStatus{}, err
	}
	return record.SkillEnvironmentStatus, nil
}

func (s *SkillDependencyService) InitializeSkill(name string) (SkillEnvironmentStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	plan, err := s.InspectSkillDependencies(name)
	if err != nil {
		return SkillEnvironmentStatus{}, err
	}
	if plan.NeedsReview {
		return SkillEnvironmentStatus{Skill: plan.Skill, State: "needs-review", Runtime: plan.Runtime}, errors.New("Skill 依赖需要用户确认")
	}
	if plan.Runtime == "" {
		return SkillEnvironmentStatus{Skill: plan.Skill, State: "not-required"}, nil
	}
	if _, err := os.Stat(s.runtimeExecutable(plan.Runtime)); err != nil {
		status := SkillEnvironmentStatus{Skill: plan.Skill, State: "failed", Runtime: plan.Runtime, Error: "内置运行时不可用", UpdatedAt: time.Now().Unix()}
		return s.failed(name, plan, status, errors.New("内置运行时不可用"))
	}
	skillDir, _ := s.skillDirectory(name)
	runtimeVersion := readRuntimeVersion(filepath.Dir(s.runtimeExecutable(plan.Runtime)))
	managerVersion, dependencySourceHash, lockHash := "", "", ""
	if plan.Runtime == "python" {
		uv := s.runtimeExecutable("uv")
		if _, err := os.Stat(uv); err != nil {
			status := SkillEnvironmentStatus{Skill: plan.Skill, State: "failed", Runtime: plan.Runtime, Error: "内置 uv 不可用", UpdatedAt: time.Now().Unix()}
			return s.failed(name, plan, status, errors.New("内置 uv 不可用"))
		}
		managerVersion = readRuntimeVersion(filepath.Dir(uv))
		dependencySourceHash, err = pythonDependencySourceHash(skillDir, plan)
		if err != nil {
			return SkillEnvironmentStatus{}, err
		}
		if current, currentErr := s.GetSkillEnvironmentStatus(name); currentErr == nil && current.State == "ready" && current.RuntimeVersion == runtimeVersion && current.PackageManagerVersion == managerVersion && current.DependencySourceHash == dependencySourceHash {
			if info, statErr := os.Stat(current.Environment); statErr == nil && info.IsDir() {
				return current, nil
			}
		}
		if err := s.preparePythonLock(context.Background(), &plan, skillDir); err != nil {
			status := SkillEnvironmentStatus{Skill: plan.Skill, State: "failed", Runtime: plan.Runtime, RuntimeVersion: runtimeVersion, PackageManagerVersion: managerVersion, Error: err.Error(), UpdatedAt: time.Now().Unix()}
			return s.failed(name, plan, status, err)
		}
		if err := writeSkillManifest(filepath.Join(skillDir, skillManifestFile), plan); err != nil {
			return SkillEnvironmentStatus{}, err
		}
		dependencySourceHash, err = pythonDependencySourceHash(skillDir, plan)
		if err != nil {
			return SkillEnvironmentStatus{}, err
		}
		lockHash, err = hashSkillFile(skillDir, plan.Lockfile)
		if err != nil {
			return SkillEnvironmentStatus{}, err
		}
		runtimeVersion += "+uv:" + managerVersion
	} else if plan.legacy {
		if err := writeSkillManifest(filepath.Join(skillDir, skillManifestFile), plan); err != nil {
			return SkillEnvironmentStatus{}, err
		}
	}
	hash, err := hashSkillDirectory(skillDir, runtimeVersion, plan)
	if err != nil {
		return SkillEnvironmentStatus{}, err
	}
	root, err := s.manager.Ensure(DirectorySkillEnvironments)
	if err != nil {
		return SkillEnvironmentStatus{}, err
	}
	environment := filepath.Join(root, plan.Skill, hash)
	if current, currentErr := s.GetSkillEnvironmentStatus(name); currentErr == nil && current.State == "ready" && current.PlanHash == hash && current.Environment == environment {
		if info, statErr := os.Stat(environment); statErr == nil && info.IsDir() {
			return current, nil
		}
	}
	status := SkillEnvironmentStatus{Skill: plan.Skill, State: "initializing", Runtime: plan.Runtime, Environment: environment, PlanHash: hash, RuntimeVersion: readRuntimeVersion(filepath.Dir(s.runtimeExecutable(plan.Runtime))), PackageManagerVersion: managerVersion, DependencySourceHash: dependencySourceHash, LockHash: lockHash, UpdatedAt: time.Now().Unix()}
	s.notify("skill.dependency.progress", SkillInstallProgress{Skill: plan.Skill, Stage: "prepare", Message: "准备独立环境", State: status.State})
	if err := os.MkdirAll(environment, 0o755); err != nil {
		return s.failed(name, plan, status, err)
	}
	if err := s.install(context.Background(), plan, skillDir, environment); err != nil {
		return s.failed(name, plan, status, err)
	}
	status.State, status.UpdatedAt = "ready", time.Now().Unix()
	if err := s.saveStatus(name, plan, status); err != nil {
		return status, err
	}
	s.notify("skill.dependency.ready", status)
	return status, nil
}

func (s *SkillDependencyService) RepairSkillEnvironment(name string) (SkillEnvironmentStatus, error) {
	if err := s.RemoveSkillEnvironment(name); err != nil {
		return SkillEnvironmentStatus{}, err
	}
	return s.InitializeSkill(name)
}

func (s *SkillDependencyService) RemoveSkillEnvironment(name string) error {
	name = normalizeSkillName(name)
	if name == "" {
		return errors.New("技能名称不能为空")
	}
	root, err := s.manager.Ensure(DirectorySkillEnvironments)
	if err != nil {
		return err
	}
	target := filepath.Join(root, name)
	if !isWithin(root, target) {
		return errors.New("Skill 环境路径无效")
	}
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	if dir, dirErr := s.skillDirectory(name); dirErr == nil {
		if err := os.Remove(filepath.Join(dir, ".blankmind.skill-state.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func skillEvidenceSnippets(dir string, evidence []string) string {
	const maxFileBytes = 4096
	const maxTotalBytes = 24 << 10
	var result strings.Builder
	for _, relative := range evidence {
		if result.Len() >= maxTotalBytes {
			break
		}
		path := filepath.Join(dir, filepath.FromSlash(relative))
		if !isWithin(dir, path) {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if len(data) > maxFileBytes {
			data = data[:maxFileBytes]
		}
		remaining := maxTotalBytes - result.Len()
		if len(data) > remaining {
			data = data[:remaining]
		}
		result.WriteString("\n[" + relative + "]\n")
		result.Write(data)
	}
	return result.String()
}

func stripJSONFence(value string) string {
	text := strings.TrimSpace(value)
	if strings.HasPrefix(text, "```") {
		if newline := strings.IndexByte(text, '\n'); newline >= 0 {
			text = text[newline+1:]
		}
		if end := strings.LastIndex(text, "```"); end >= 0 {
			text = text[:end]
		}
	}
	return strings.TrimSpace(text)
}
