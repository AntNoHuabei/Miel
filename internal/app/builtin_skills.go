package app

import (
	"embed"
	"log"
	"os"
	"path/filepath"
)

//go:embed skills
var builtinSkills embed.FS

// builtinSkills keeps the application-owned skills in sync with the
// command contract shipped by the current Miel version.
func ensureBuiltinSkills(skillsRoot string) {
	for _, name := range []string{"todo", "reminder", "office", "websearch"} {
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
		if err := os.WriteFile(target, data, 0o644); err != nil {
			log.Println("write builtin skill", name, ":", err)
		}
	}
}
