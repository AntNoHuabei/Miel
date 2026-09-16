package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	skillManifestFile          = ".blankmind.skill.json"
	skillManifestSchemaVersion = 2
	managedRequirementsLock    = ".blankmind.requirements.lock"
)

type skillManifest struct {
	SchemaVersion int    `json:"schemaVersion"`
	Runtime       string `json:"runtime"`
	Entry         struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	} `json:"entry"`
	PackageManager struct {
		Name              string   `json:"name"`
		Lockfile          string   `json:"lockfile"`
		DependencyFile    string   `json:"dependencyFile"`
		DependencySources []string `json:"dependencySources,omitempty"`
	} `json:"packageManager"`
	Network bool `json:"network"`
}

func readSkillManifest(path string) (SkillDependencyPlan, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return SkillDependencyPlan{}, false, nil
	}
	if err != nil {
		return SkillDependencyPlan{}, false, err
	}
	var manifest skillManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return SkillDependencyPlan{}, false, fmt.Errorf("解析 Skill manifest 失败: %w", err)
	}
	runtimeName := strings.ToLower(strings.TrimSpace(manifest.Runtime))
	if runtimeName != "" && runtimeName != "node" && runtimeName != "python" {
		return SkillDependencyPlan{}, false, errors.New("Skill manifest runtime 无效")
	}
	if manifest.SchemaVersion != 1 && manifest.SchemaVersion != skillManifestSchemaVersion {
		return SkillDependencyPlan{}, false, errors.New("仅支持 schemaVersion 1 或 2")
	}
	if err := validateRelativeSkillFile(manifest.PackageManager.Lockfile); err != nil {
		return SkillDependencyPlan{}, false, err
	}
	if err := validateRelativeSkillFile(manifest.PackageManager.DependencyFile); err != nil {
		return SkillDependencyPlan{}, false, err
	}
	manager := strings.ToLower(strings.TrimSpace(manifest.PackageManager.Name))
	legacy := manifest.SchemaVersion == 1 && runtimeName == "python" && manager == "pip"
	if legacy {
		manager = "uv"
	}
	plan := SkillDependencyPlan{
		SchemaVersion: skillManifestSchemaVersion, Runtime: runtimeName,
		Evidence:       []string{},
		PackageManager: manager,
		Lockfile:       manifest.PackageManager.Lockfile, DependencyFile: manifest.PackageManager.DependencyFile,
		DependencySources: nonNilStrings(manifest.PackageManager.DependencySources),
		EntryCommand:      manifest.Entry.Command, EntryArgs: nonNilStrings(manifest.Entry.Args),
		Network: manifest.Network, Source: "manifest", Confidence: "confirmed", AutoUpdate: runtimeName == "python", legacy: legacy,
	}
	plan.Kind = "executable"
	plan.CanRun = runtimeName != "" && manifest.Entry.Command != "" && len(manifest.Entry.Args) > 0
	if !plan.CanRun {
		plan.Kind, plan.Reason = "unresolved", "manifest 缺少可执行入口"
		plan.NeedsReview = true
	}
	if err := validateDependencyPlan(plan); err != nil {
		return SkillDependencyPlan{}, false, err
	}
	return plan, true, nil
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func detectSkillDependencies(dir, name string) SkillDependencyPlan {
	plan := SkillDependencyPlan{SchemaVersion: skillManifestSchemaVersion, Skill: name, Source: "detected", Confidence: "none", Evidence: []string{}, Kind: "instruction", Reason: "仅包含 Skill 文档"}
	var nodeFiles, pythonFiles, unsupportedFiles []string
	_ = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry == nil {
			return nil
		}
		if entry.IsDir() {
			if path != dir && (entry.Name() == ".git" || entry.Name() == "node_modules" || entry.Name() == ".venv") {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return nil
		}
		base := strings.ToLower(entry.Name())
		switch base {
		case "package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock":
			nodeFiles = append(nodeFiles, filepath.ToSlash(rel))
		case "requirements.txt", "pyproject.toml", "uv.lock", "poetry.lock", "pipfile":
			pythonFiles = append(pythonFiles, filepath.ToSlash(rel))
		}
		switch strings.ToLower(filepath.Ext(base)) {
		case ".js", ".mjs", ".cjs", ".ts", ".tsx":
			nodeFiles = append(nodeFiles, filepath.ToSlash(rel))
		case ".py":
			pythonFiles = append(pythonFiles, filepath.ToSlash(rel))
		case ".sh", ".ps1", ".bat", ".cmd", ".exe", ".rb", ".pl":
			unsupportedFiles = append(unsupportedFiles, filepath.ToSlash(rel))
		}
		return nil
	})
	nodeFiles = uniqueStrings(nodeFiles)
	pythonFiles = uniqueStrings(pythonFiles)
	switch {
	case hasNodeProject(nodeFiles) && hasNodeSource(nodeFiles) && len(pythonFiles) == 0:
		plan.Runtime, plan.Confidence, plan.Evidence = "node", "high", nodeFiles
		plan.PackageManager, plan.DependencyFile, plan.Network = "npm", "package.json", true
		plan.NeedsReview = true
		if includesBase(nodeFiles, "package-lock.json") {
			plan.Lockfile = "package-lock.json"
		}
	case hasPythonProject(pythonFiles) && hasPythonSource(pythonFiles) && len(nodeFiles) == 0:
		plan.Runtime, plan.Confidence, plan.Evidence = "python", "high", pythonFiles
		plan.PackageManager, plan.Network, plan.AutoUpdate = "uv", true, true
		plan.NeedsReview = true
		if includesBase(pythonFiles, "pyproject.toml") && includesBase(pythonFiles, "uv.lock") {
			plan.DependencyFile, plan.Lockfile = "pyproject.toml", "uv.lock"
			plan.DependencySources = []string{"pyproject.toml", "uv.lock"}
		} else if includesBase(pythonFiles, "requirements.txt") {
			plan.DependencyFile, plan.Lockfile = "requirements.txt", managedRequirementsLock
			plan.DependencySources = []string{"requirements.txt"}
		} else {
			plan.DependencyFile, plan.Lockfile = "pyproject.toml", "uv.lock"
			plan.DependencySources = []string{"pyproject.toml"}
		}
	default:
		plan.Evidence = append(nodeFiles, pythonFiles...)
		if len(plan.Evidence) > 0 {
			plan.Confidence, plan.NeedsReview = "low", true
			plan.Kind, plan.Reason = "unresolved", "发现脚本或依赖文件，但无法唯一确定执行入口"
		}
	}
	if plan.Runtime != "" {
		if entry := inferDocumentedSkillEntry(dir, plan.Runtime, plan.Evidence); entry != "" {
			plan.EntryCommand, plan.EntryArgs = plan.Runtime, []string{entry}
		}
	}
	if plan.Runtime != "" {
		if plan.EntryCommand != "" && len(plan.EntryArgs) > 0 {
			plan.Kind, plan.CanRun, plan.Reason = "executable", true, ""
		} else {
			plan.Kind, plan.CanRun = "unresolved", false
			plan.Reason = "运行时存在但未找到可执行入口"
		}
	}
	if len(unsupportedFiles) > 0 && plan.Kind == "instruction" {
		plan.Kind, plan.NeedsReview, plan.Confidence = "unresolved", true, "low"
		plan.Reason = "发现当前受管理运行时不支持的脚本或程序，需要检查入口"
		plan.Evidence = append(plan.Evidence, unsupportedFiles...)
	}
	return plan
}

func inferDocumentedSkillEntry(dir, runtimeName string, evidence []string) string {
	document, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return ""
	}
	extensions := []string{".py"}
	if runtimeName == "node" {
		extensions = []string{".js", ".mjs", ".cjs"}
	}
	content := strings.ReplaceAll(strings.ToLower(string(document)), "\\", "/")
	candidates := make([]string, 0, 2)
	for _, relative := range evidence {
		normalized := filepath.ToSlash(relative)
		if hasExtension([]string{normalized}, extensions...) && strings.Contains(content, strings.ToLower(normalized)) {
			candidates = append(candidates, normalized)
		}
	}
	candidates = uniqueStrings(candidates)
	if len(candidates) == 1 {
		return candidates[0]
	}
	return ""
}

func writeSkillManifest(path string, plan SkillDependencyPlan) error {
	manifest := skillManifest{SchemaVersion: skillManifestSchemaVersion, Runtime: plan.Runtime, Network: plan.Network}
	manifest.Entry.Command, manifest.Entry.Args = plan.EntryCommand, plan.EntryArgs
	manifest.PackageManager.Name = plan.PackageManager
	manifest.PackageManager.Lockfile = plan.Lockfile
	manifest.PackageManager.DependencyFile = plan.DependencyFile
	manifest.PackageManager.DependencySources = plan.DependencySources
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, data)
}

func validateRelativeSkillFile(value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	clean := filepath.Clean(value)
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return errors.New("Skill manifest 依赖路径必须位于 Skill 目录")
	}
	return nil
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func includesBase(values []string, name string) bool {
	for _, value := range values {
		if strings.EqualFold(filepath.Base(value), name) {
			return true
		}
	}
	return false
}

func hasNodeProject(values []string) bool { return includesBase(values, "package.json") }
func hasPythonProject(values []string) bool {
	return includesBase(values, "requirements.txt") || includesBase(values, "pyproject.toml")
}
func hasNodeSource(values []string) bool {
	return hasExtension(values, ".js", ".mjs", ".cjs", ".ts", ".tsx")
}
func hasPythonSource(values []string) bool { return hasExtension(values, ".py") }

func hasExtension(values []string, extensions ...string) bool {
	for _, value := range values {
		for _, extension := range extensions {
			if strings.EqualFold(filepath.Ext(value), extension) {
				return true
			}
		}
	}
	return false
}
