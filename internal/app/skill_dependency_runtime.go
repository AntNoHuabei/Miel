package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const maxSkillProcessOutput = 1 << 20

func (s *SkillDependencyService) install(ctx context.Context, plan SkillDependencyPlan, skillDir, environment string) error {
	cacheRoot, err := s.manager.Ensure(DirectoryRuntimeCache)
	if err != nil {
		return err
	}
	runtimeRoot := s.resolveRuntime()
	basePath := filepath.Join(runtimeRoot, "node") + string(os.PathListSeparator) + filepath.Join(runtimeRoot, "python") + string(os.PathListSeparator) + filepath.Join(runtimeRoot, "uv")
	if plan.Runtime == "node" {
		if plan.DependencyFile == "" {
			return nil
		}
		if err := copyFile(filepath.Join(skillDir, plan.DependencyFile), filepath.Join(environment, "package.json")); err != nil {
			return err
		}
		if plan.Lockfile != "" {
			if err := copyFile(filepath.Join(skillDir, plan.Lockfile), filepath.Join(environment, filepath.Base(plan.Lockfile))); err != nil {
				return err
			}
		}
		args := []string{"install", "--ignore-scripts"}
		if strings.EqualFold(filepath.Base(plan.Lockfile), "package-lock.json") {
			args = []string{"ci", "--ignore-scripts"}
		}
		env := []string{
			"PATH=" + basePath + string(os.PathListSeparator) + os.Getenv("PATH"),
			"npm_config_cache=" + filepath.Join(cacheRoot, "npm"),
			"npm_config_ignore_scripts=true",
		}
		s.notify("skill.dependency.progress", SkillInstallProgress{Skill: plan.Skill, Stage: "install", Message: "安装 npm 依赖", State: "initializing"})
		_, err = s.run(ctx, filepath.Join(runtimeRoot, "node", "npm.cmd"), args, environment, env)
		return err
	}
	python := filepath.Join(runtimeRoot, "python", "python.exe")
	uv := filepath.Join(runtimeRoot, "uv", "uv.exe")
	uvEnv := []string{
		"PATH=" + basePath + string(os.PathListSeparator) + os.Getenv("PATH"),
		"UV_CACHE_DIR=" + filepath.Join(cacheRoot, "uv"),
		"UV_PYTHON=" + python,
		"UV_NO_MANAGED_PYTHON=true",
	}
	s.notify("skill.dependency.progress", SkillInstallProgress{Skill: plan.Skill, Stage: "environment", Message: "使用内置 Python 创建 uv 环境", State: "initializing"})
	if _, err := s.run(ctx, uv, []string{"venv", "--python", python, environment}, skillDir, uvEnv); err != nil {
		return fmt.Errorf("创建 uv Python 环境失败: %w", err)
	}
	if plan.DependencyFile == "" {
		return nil
	}
	venvPython := filepath.Join(environment, "Scripts", "python.exe")
	uvEnv = append(uvEnv, "PATH="+filepath.Join(environment, "Scripts")+string(os.PathListSeparator)+basePath+string(os.PathListSeparator)+os.Getenv("PATH"))
	args := []string{"pip", "sync", "--python", venvPython, "--no-build", "--require-hashes", filepath.Join(skillDir, plan.Lockfile)}
	message := "仅同步预编译 wheel"
	if strings.EqualFold(filepath.Base(plan.DependencyFile), "pyproject.toml") {
		args = []string{"sync", "--project", skillDir, "--frozen", "--no-install-project", "--no-install-workspace", "--no-build"}
		uvEnv = append(uvEnv, "UV_PROJECT_ENVIRONMENT="+environment)
		message = "按 uv.lock 同步预编译 wheel"
	}
	s.notify("skill.dependency.progress", SkillInstallProgress{Skill: plan.Skill, Stage: "sync", Message: message, State: "initializing"})
	_, err = s.run(ctx, uv, args, skillDir, uvEnv)
	return err
}

func (s *SkillDependencyService) preparePythonLock(ctx context.Context, plan *SkillDependencyPlan, skillDir string) error {
	normalizePythonPlan(plan)
	if plan.DependencyFile == "" {
		return nil
	}
	if err := validatePythonDependencySources(skillDir, plan.DependencyFile); err != nil {
		return err
	}
	cacheRoot, err := s.manager.Ensure(DirectoryRuntimeCache)
	if err != nil {
		return err
	}
	runtimeRoot := s.resolveRuntime()
	python := filepath.Join(runtimeRoot, "python", "python.exe")
	uv := filepath.Join(runtimeRoot, "uv", "uv.exe")
	basePath := filepath.Join(runtimeRoot, "node") + string(os.PathListSeparator) + filepath.Join(runtimeRoot, "python") + string(os.PathListSeparator) + filepath.Join(runtimeRoot, "uv")
	env := []string{
		"PATH=" + basePath + string(os.PathListSeparator) + os.Getenv("PATH"),
		"UV_CACHE_DIR=" + filepath.Join(cacheRoot, "uv"),
		"UV_PYTHON=" + python,
		"UV_NO_MANAGED_PYTHON=true",
	}
	s.notify("skill.dependency.progress", SkillInstallProgress{Skill: plan.Skill, Stage: "lock", Message: "解析并更新 uv 锁文件", State: "initializing"})
	if strings.EqualFold(filepath.Base(plan.DependencyFile), "requirements.txt") {
		lockPath := filepath.Join(skillDir, plan.Lockfile)
		if !isWithin(skillDir, lockPath) {
			return errors.New("受管理 requirements 锁文件路径无效")
		}
		temporary := lockPath + ".tmp"
		_ = os.Remove(temporary)
		args := []string{"pip", "compile", "--python", python, "--generate-hashes", "--no-build", "--output-file", temporary, filepath.Join(skillDir, plan.DependencyFile)}
		if _, err := s.run(ctx, uv, args, skillDir, env); err != nil {
			_ = os.Remove(temporary)
			return fmt.Errorf("生成 requirements 锁文件失败: %w", err)
		}
		if err := os.Rename(temporary, lockPath); err != nil {
			_ = os.Remove(temporary)
			return fmt.Errorf("写入 requirements 锁文件失败: %w", err)
		}
	} else if strings.EqualFold(filepath.Base(plan.DependencyFile), "pyproject.toml") {
		if _, err := s.run(ctx, uv, []string{"lock", "--project", skillDir, "--no-build"}, skillDir, env); err != nil {
			return fmt.Errorf("更新 uv.lock 失败: %w", err)
		}
	}
	if _, err := os.Stat(filepath.Join(skillDir, plan.Lockfile)); err != nil {
		return fmt.Errorf("依赖锁文件不存在: %w", err)
	}
	return nil
}

func validatePythonDependencySources(skillDir, dependencyFile string) error {
	if !strings.EqualFold(filepath.Base(dependencyFile), "requirements.txt") {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(skillDir, dependencyFile))
	if err != nil {
		return err
	}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.SplitN(raw, "#", 2)[0])
		if line == "" {
			continue
		}
		candidate := ""
		if strings.HasPrefix(line, "-e ") || strings.HasPrefix(line, "--editable ") {
			candidate = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "-e "), "--editable "))
		} else if strings.HasPrefix(line, "-r ") || strings.HasPrefix(line, "--requirement ") || strings.HasPrefix(line, "--find-links ") {
			candidate = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(line, "-r "), "--requirement "), "--find-links "))
		} else if index := strings.Index(line, " @ file:"); index >= 0 {
			candidate = strings.TrimSpace(line[index+3:])
		} else if strings.HasPrefix(line, "file:") || strings.HasPrefix(line, "."+string(filepath.Separator)) || strings.HasPrefix(line, "../") || filepath.IsAbs(line) {
			candidate = line
		}
		if candidate == "" {
			continue
		}
		if strings.HasPrefix(candidate, "file:") {
			parsed, parseErr := url.Parse(candidate)
			if parseErr != nil || parsed.Host != "" {
				return errors.New("Skill 本地依赖 URL 无效")
			}
			candidate = filepath.FromSlash(parsed.Path)
		}
		resolved := candidate
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(skillDir, resolved)
		}
		if !isWithin(skillDir, resolved) {
			return errors.New("Skill 本地依赖路径必须位于 Skill 目录")
		}
	}
	return nil
}

// ExecuteSkill runs only the confirmed Node/Python entry through the bundled runtime.
//
//wails:ignore
func (s *SkillDependencyService) ExecuteSkill(name string, args []string) ([]byte, error) {
	return s.ExecuteSkillContext(context.Background(), name, args)
}

// ExecuteSkillContext preserves the chat correlation and cancellation context.
//
//wails:ignore
func (s *SkillDependencyService) ExecuteSkillContext(parent context.Context, name string, args []string) (output []byte, retErr error) {
	startedAt := time.Now()
	skillName := normalizeSkillName(name)
	logInfo(parent, "skill.run.start", "skill", skillName, "argument_count", len(args))
	defer func() {
		attrs := []any{"skill", skillName, "status", "done", "duration_ms", time.Since(startedAt).Milliseconds(), "output_bytes", len(output)}
		if retErr != nil {
			attrs[3] = "failed"
			logError(parent, "skill.run.finish", retErr, attrs...)
			return
		}
		logInfo(parent, "skill.run.finish", attrs...)
	}()
	plan, err := s.InspectSkillDependencies(name)
	if err != nil {
		return nil, err
	}
	if plan.NeedsReview && plan.Confidence == "high" && plan.EntryCommand != "" && len(plan.EntryArgs) > 0 {
		plan, err = s.ConfirmSkillDependencyPlan(name, plan)
		if err != nil {
			return nil, fmt.Errorf("自动确认 Skill 依赖计划失败: %w", err)
		}
	}
	if plan.NeedsReview || plan.EntryCommand == "" {
		return nil, errors.New("无法安全确定 Skill 依赖或可执行入口，已中断调用")
	}
	if plan.EntryCommand != plan.Runtime {
		return nil, errors.New("Skill 入口必须使用声明的内置运行时")
	}
	status, err := s.InitializeSkill(name)
	if err != nil {
		return nil, err
	}
	skillDir, _ := s.skillDirectory(name)
	command := s.runtimeExecutable(plan.Runtime)
	envPath := filepath.Dir(command)
	if plan.Runtime == "python" {
		command = filepath.Join(status.Environment, "Scripts", "python.exe")
		envPath = filepath.Join(status.Environment, "Scripts")
	}
	args = trimRepeatedSkillEntry(args, plan.EntryArgs)
	processArgs := append(append([]string{}, plan.EntryArgs...), args...)
	entryPath := filepath.Join(skillDir, processArgs[0])
	if !isWithin(skillDir, entryPath) {
		return nil, errors.New("Skill 入口路径越界")
	}
	processArgs[0] = entryPath
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	return s.run(ctx, command, processArgs, skillDir, bundledRuntimeEnv(s.resolveRuntime(), s.manager, envPath))
}

// trimRepeatedSkillEntry handles models that copy the documented entry script
// into skill_run args even though the runtime already prepends it.
func trimRepeatedSkillEntry(args, entryArgs []string) []string {
	if len(args) == 0 || len(entryArgs) == 0 {
		return args
	}
	provided := filepath.Clean(strings.TrimSpace(args[0]))
	entry := filepath.Clean(strings.TrimSpace(entryArgs[0]))
	if provided == "." || entry == "." || !strings.EqualFold(provided, entry) {
		return args
	}
	return append([]string(nil), args[1:]...)
}

func bundledRuntimeEnv(runtimeRoot string, manager *DirectoryManager, prependPath string) []string {
	paths := make([]string, 0, 5)
	if strings.TrimSpace(prependPath) != "" {
		paths = append(paths, prependPath)
	}
	paths = append(paths,
		filepath.Join(runtimeRoot, "node"),
		filepath.Join(runtimeRoot, "python"),
		filepath.Join(runtimeRoot, "uv"),
	)
	if systemPath := os.Getenv("PATH"); systemPath != "" {
		paths = append(paths, systemPath)
	}
	cacheRoot := ""
	if manager != nil {
		cacheRoot, _ = manager.Ensure(DirectoryRuntimeCache)
	}
	return []string{
		"PATH=" + strings.Join(paths, string(os.PathListSeparator)),
		"UV_PYTHON=" + filepath.Join(runtimeRoot, "python", "python.exe"),
		"UV_NO_MANAGED_PYTHON=true",
		"UV_CACHE_DIR=" + filepath.Join(cacheRoot, "uv"),
		"npm_config_cache=" + filepath.Join(cacheRoot, "npm"),
		"npm_config_ignore_scripts=true",
	}
}

func (s *SkillDependencyService) runtimeExecutable(name string) string {
	switch name {
	case "node":
		return filepath.Join(s.resolveRuntime(), "node", "node.exe")
	case "python":
		return filepath.Join(s.resolveRuntime(), "python", "python.exe")
	case "uv":
		return filepath.Join(s.resolveRuntime(), "uv", "uv.exe")
	}
	return ""
}

func (s *SkillDependencyService) skillDirectory(name string) (string, error) {
	name = normalizeSkillName(name)
	if name == "" {
		return "", errors.New("技能名称不能为空")
	}
	root, err := s.manager.Ensure(DirectorySkills)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, name)
	if !isWithin(root, dir) {
		return "", errors.New("技能路径无效")
	}
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
		return "", errors.New("技能不存在")
	}
	return dir, nil
}

func (s *SkillDependencyService) failed(name string, plan SkillDependencyPlan, status SkillEnvironmentStatus, cause error) (SkillEnvironmentStatus, error) {
	status.State, status.Error, status.UpdatedAt = "failed", cause.Error(), time.Now().Unix()
	_ = s.saveStatus(name, plan, status)
	s.notify("skill.dependency.failed", status)
	return status, cause
}

func (s *SkillDependencyService) saveStatus(name string, plan SkillDependencyPlan, status SkillEnvironmentStatus) error {
	dir, err := s.skillDirectory(name)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(skillEnvironmentRecord{SkillEnvironmentStatus: status, Plan: plan}, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(dir, ".blankmind.skill-state.json"), data)
}

func atomicWrite(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func readRuntimeVersion(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "VERSION"))
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(data))
}

func copyFile(source, target string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(target, data, 0o644)
}

func runSkillProcess(ctx context.Context, command string, args []string, workdir string, env []string) ([]byte, error) {
	startedAt := time.Now()
	logInfo(ctx, "skill.process.start", "executable", logExecutable(command), "argument_count", len(args), "workdir", workdir)
	cmd := exec.CommandContext(ctx, command, args...)
	hideProcessWindow(cmd)
	cmd.Dir = workdir
	cmd.Env = append(os.Environ(), env...)
	var output limitedBuffer
	cmd.Stdout, cmd.Stderr = &output, &output
	err := cmd.Run()
	if ctx.Err() != nil {
		logError(ctx, "skill.process.finish", ctx.Err(), "executable", logExecutable(command), "status", "cancelled", "duration_ms", time.Since(startedAt).Milliseconds(), "output_bytes", output.Len())
		return output.Bytes(), errors.New("Skill 进程执行超时或已取消")
	}
	if err != nil {
		logError(ctx, "skill.process.finish", err, "executable", logExecutable(command), "status", "failed", "duration_ms", time.Since(startedAt).Milliseconds(), "output_bytes", output.Len())
		return output.Bytes(), fmt.Errorf("运行 %s 失败: %w: %s", filepath.Base(command), err, strings.TrimSpace(output.String()))
	}
	logInfo(ctx, "skill.process.finish", "executable", logExecutable(command), "status", "done", "duration_ms", time.Since(startedAt).Milliseconds(), "output_bytes", output.Len())
	return output.Bytes(), nil
}

type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(data []byte) (int, error) {
	original := len(data)
	remaining := maxSkillProcessOutput - b.Len()
	if remaining > 0 {
		_, _ = b.Buffer.Write(data[:min(remaining, len(data))])
	}
	return original, nil
}

func hashSkillDirectory(dir, runtimeVersion string, plan SkillDependencyPlan) (string, error) {
	files := []string{}
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != dir && (entry.Name() == ".git" || entry.Name() == "node_modules" || entry.Name() == ".venv") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".blankmind.skill-state") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	h := sha256.New()
	_, _ = io.WriteString(h, runtimeVersion)
	planData, _ := json.Marshal(plan)
	_, _ = h.Write(planData)
	for _, path := range files {
		rel, _ := filepath.Rel(dir, path)
		_, _ = io.WriteString(h, filepath.ToSlash(rel))
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return "", readErr
		}
		_, _ = h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}

func hashSkillFile(dir, relative string) (string, error) {
	if relative == "" {
		return "", nil
	}
	path := filepath.Join(dir, relative)
	if !isWithin(dir, path) {
		return "", errors.New("Skill 锁文件路径无效")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func hashSkillFiles(dir string, relatives []string) (string, error) {
	h := sha256.New()
	for _, relative := range relatives {
		if relative == "" {
			continue
		}
		path := filepath.Join(dir, relative)
		if !isWithin(dir, path) {
			return "", errors.New("Skill 依赖路径无效")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		_, _ = io.WriteString(h, filepath.ToSlash(relative))
		_, _ = h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func pythonDependencySourceHash(dir string, plan SkillDependencyPlan) (string, error) {
	inputs := []string{plan.DependencyFile}
	if strings.EqualFold(filepath.Base(plan.DependencyFile), "pyproject.toml") && plan.Lockfile != "" {
		if _, err := os.Stat(filepath.Join(dir, plan.Lockfile)); err == nil {
			inputs = append(inputs, plan.Lockfile)
		}
	}
	return hashSkillFiles(dir, inputs)
}
