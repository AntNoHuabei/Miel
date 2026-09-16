package app

import (
	"fmt"
	"os"
	"path/filepath"
)

// resolveSkillCapability is read-only and shared by discovery, setup and execution.
func resolveSkillCapability(dir, name string) SkillDependencyPlan {
	if isBuiltinSkill(name) {
		return SkillDependencyPlan{Skill: name, Kind: "builtin", CanRun: true, Confidence: "confirmed", Evidence: []string{}, EntryArgs: []string{}, DependencySources: []string{}}
	}
	plan, found, err := readSkillManifest(filepath.Join(dir, skillManifestFile))
	if err != nil {
		return SkillDependencyPlan{Skill: name, Kind: "unresolved", Reason: err.Error(), NeedsReview: true, Evidence: []string{}, EntryArgs: []string{}, DependencySources: []string{}}
	}
	if !found {
		plan = detectSkillDependencies(dir, name)
	}
	plan.Skill = name
	normalizePlanSlices(&plan)
	if plan.CanRun {
		files := []string{plan.EntryArgs[0]}
		if plan.DependencyFile != "" {
			files = append(files, plan.DependencyFile)
		}
		for _, file := range files {
			path := filepath.Join(dir, file)
			resolved, resolveErr := filepath.EvalSymlinks(path)
			info, statErr := os.Stat(path)
			if resolveErr != nil || statErr != nil || info.IsDir() || !isWithin(dir, resolved) {
				plan.Kind, plan.CanRun, plan.NeedsReview, plan.Reason = "unresolved", false, true, fmt.Sprintf("入口或依赖文件不存在或越界: %s", file)
				break
			}
		}
	}
	return plan
}

func skillRoute(plan SkillDependencyPlan) string {
	switch plan.Kind {
	case "instruction":
		return "[kind=instruction; canRun=false] 文档型 Skill：skill_load 后遵循文档使用已授权工具；需要脚本时在工作区生成并运行。不要调用 skill_run，也不要用 --help 探测入口；不自动安装文档示例依赖。"
	case "executable":
		return "[kind=executable; canRun=true] 可执行 Skill：skill_load 后使用 skill_run(command=run) 执行受管理入口；失败时不得复制脚本绕过独立环境。"
	case "builtin":
		return "[kind=builtin; canRun=true] 内置 Skill：按文档使用 skill_run 的命令。"
	default:
		return "[kind=unresolved; canRun=false] 执行入口未确定：" + plan.Reason
	}
}
