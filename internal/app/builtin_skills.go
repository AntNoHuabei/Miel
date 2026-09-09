package app

import (
	"embed"
	"log"
	"os"
	"path/filepath"
)

//go:embed skills
var builtinSkills embed.FS

// builtinSkills 启动时确保数据目录 skills/ 内含内置 skill(todo/reminder/office)。
// 若同名 SKILL.md 已存在(用户自定义或旧内置)则跳过,不覆盖用户内容。
func ensureBuiltinSkills(skillsRoot string) {
	for _, name := range []string{"todo", "reminder", "office"} {
		data, err := builtinSkills.ReadFile("skills/" + name + "/SKILL.md")
		if err != nil {
			log.Println("read builtin skill", name, ":", err)
			continue
		}
		dir := filepath.Join(skillsRoot, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Println("mkdir skill dir:", err)
			continue
		}
		target := filepath.Join(dir, "SKILL.md")
		if _, err := os.Stat(target); err == nil {
			continue // 已有内容,不覆盖
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			log.Println("write builtin skill", name, ":", err)
		}
	}
}
