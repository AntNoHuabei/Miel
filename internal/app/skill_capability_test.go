package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillCapabilityMatrix(t *testing.T) {
	for _, tc := range []struct {
		name, kind string
		files      map[string]string
	}{
		{"docs", "instruction", map[string]string{"README.md": "example", "image.svg": "<svg/>"}},
		{"script", "unresolved", map[string]string{"main.py": "pass"}},
		{"shell", "unresolved", map[string]string{"run.sh": "echo hello"}},
		{"powershell", "unresolved", map[string]string{"run.ps1": "Write-Output hello"}},
		{"deps", "unresolved", map[string]string{"requirements.txt": "requests"}},
		{"mixed", "unresolved", map[string]string{"main.py": "pass", "main.js": "", "package.json": "{}"}},
		{"broken", "unresolved", map[string]string{skillManifestFile: "{"}},
		{"missing", "unresolved", map[string]string{skillManifestFile: `{"schemaVersion":2,"runtime":"python","entry":{"command":"python","args":["absent.py"]}}`}},
		{"legacy", "executable", map[string]string{skillManifestFile: `{"schemaVersion":1,"runtime":"python","entry":{"command":"python","args":["main.py"]},"packageManager":{"name":"pip"}}`, "main.py": "pass"}},
		{"node", "executable", map[string]string{"SKILL.md": "---\nname: node\ndescription: test\n---\nRun main.js", "main.js": "", "package.json": "{}"}},
		{"node-esm", "executable", map[string]string{"SKILL.md": "---\nname: node-esm\ndescription: test\n---\nRun main.mjs", "main.mjs": "", "package.json": "{}"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager := NewDirectoryManager(t.TempDir())
			dir := writeTestSkill(t, manager, tc.name, tc.files)
			plan := resolveSkillCapability(dir, tc.name)
			if plan.Kind != tc.kind || plan.CanRun != (tc.kind == "executable") {
				t.Fatalf("%#v", plan)
			}
		})
	}
}

func TestInstructionSkillImportLoadAndMisroute(t *testing.T) {
	manager := NewDirectoryManager(t.TempDir())
	source := writeTestSkill(t, NewDirectoryManager(t.TempDir()), "web-scraper", map[string]string{"README.md": "cascade reference", "SKILL.md": "---\nname: web-scraper\ndescription: scrape\n---\nOriginal cascade instructions\n```python\nprint(1)\n```"})
	svc := NewSkillService(manager)
	svc.dependencies = NewSkillDependencyService(manager, nil)
	svc.dependencies.run = func(context.Context, string, []string, string, []string) ([]byte, error) {
		t.Fatal("unexpected process")
		return nil, nil
	}
	installed, err := svc.ImportLocal(source)
	if err != nil || installed.Kind != "instruction" || !installed.Enabled {
		t.Fatalf("%#v %v", installed, err)
	}
	if err := svc.SetEnabled(installed.Name, true); err != nil {
		t.Fatal(err)
	}
	repo, err := newManagedSkillRepository(manager.Path(DirectorySkills))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := repo.Get(installed.Name)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(loaded.Body, "Original cascade instructions") || !strings.Contains(loaded.Body, "kind=instruction") || len(loaded.Docs) == 0 {
		t.Fatalf("%#v", loaded)
	}
	original := skillDependencySvc
	skillDependencySvc = svc.dependencies
	t.Cleanup(func() { skillDependencySvc = original })
	result, err := executeSkillCommand(context.Background(), nil, nil, skillRunRequest{Skill: installed.Name, Command: "run", Args: []string{"--help"}})
	if err != nil || result.ExitCode != 2 || !strings.Contains(result.Stderr, "instruction_only") {
		t.Fatalf("%#v %v", result, err)
	}
	if _, err := os.Stat(filepath.Join(installed.Path, ".blankmind.skill-state.json")); !os.IsNotExist(err) {
		t.Fatalf("unexpected environment state: %v", err)
	}
	if err := svc.SetEnabled(installed.Name, false); err != nil {
		t.Fatal(err)
	}
	installed, err = svc.ImportLocal(source)
	if err != nil || installed.Enabled {
		t.Fatalf("disabled state lost: %#v %v", installed, err)
	}
	if err := svc.ReconcileDependencyState(); err != nil {
		t.Fatal(err)
	}
	items, err := svc.ListSkills()
	if err != nil || len(items) != 1 || items[0].Enabled || items[0].Kind != "instruction" {
		t.Fatalf("%#v %v", items, err)
	}
}
