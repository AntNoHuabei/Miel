package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeTestSkill(t *testing.T, manager *DirectoryManager, name string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(manager.Path(DirectorySkills), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: test\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestInstalledRuntimeRootUsesEnvironmentAndLocalEnvFile(t *testing.T) {
	t.Setenv("MIEL_RUNTIME_ROOT", filepath.Join("configured", "runtime"))
	want, _ := filepath.Abs(filepath.Join("configured", "runtime"))
	if got := installedRuntimeRoot(); got != want {
		t.Fatalf("environment runtime root = %q, want %q", got, want)
	}

	t.Setenv("MIEL_RUNTIME_ROOT", "")
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	if err := os.WriteFile(filepath.Join(dir, ".env.local"), []byte("# local only\nMIEL_RUNTIME_ROOT='./runtime-stage'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	want = filepath.Join(dir, "runtime-stage")
	if got := installedRuntimeRoot(); got != want {
		t.Fatalf("local env runtime root = %q, want %q", got, want)
	}
}

func TestDetectSkillDependenciesRequiresProjectAndSourceEvidence(t *testing.T) {
	manager := NewDirectoryManager(t.TempDir())
	nodeDir := writeTestSkill(t, manager, "node-skill", map[string]string{"package.json": `{}`, "src/main.ts": "export {}"})
	plan := detectSkillDependencies(nodeDir, "node-skill")
	if plan.Runtime != "node" || !plan.NeedsReview || plan.DependencyFile != "package.json" {
		t.Fatalf("unexpected node plan: %#v", plan)
	}

	pythonDir := writeTestSkill(t, manager, "python-skill", map[string]string{"requirements.txt": "requests==2.0\n", "main.py": "print('ok')"})
	plan = detectSkillDependencies(pythonDir, "python-skill")
	if plan.Runtime != "python" || !plan.NeedsReview || plan.PackageManager != "uv" || plan.Lockfile != managedRequirementsLock {
		t.Fatalf("unexpected python plan: %#v", plan)
	}

	mixedDir := writeTestSkill(t, manager, "mixed-skill", map[string]string{"package.json": `{}`, "main.js": "", "requirements.txt": "", "main.py": ""})
	plan = detectSkillDependencies(mixedDir, "mixed-skill")
	if !plan.NeedsReview || plan.Runtime != "" {
		t.Fatalf("mixed skill must require review: %#v", plan)
	}
}

func TestDetectSkillDependenciesInfersSingleDocumentedPythonEntry(t *testing.T) {
	manager := NewDirectoryManager(t.TempDir())
	dir := writeTestSkill(t, manager, "watermark", map[string]string{
		"SKILL.md":             "---\nname: watermark\ndescription: test\n---\nRun `python scripts/watermark.py --input photo.jpg`.\n",
		"pyproject.toml":       "[project]\nname='watermark'\ndependencies=['Pillow>=10']\n",
		"scripts/watermark.py": "from PIL import Image\n",
	})
	plan := detectSkillDependencies(dir, "watermark")
	if !plan.NeedsReview || plan.EntryCommand != "python" || len(plan.EntryArgs) != 1 || plan.EntryArgs[0] != "scripts/watermark.py" {
		t.Fatalf("unexpected inferred watermark plan: %#v", plan)
	}
}

func TestConfirmSkillDependencyPlanRejectsExecutableInjection(t *testing.T) {
	manager := NewDirectoryManager(t.TempDir())
	writeTestSkill(t, manager, "unsafe", nil)
	service := NewSkillDependencyService(manager, nil)
	plan := SkillDependencyPlan{Runtime: "python", PackageManager: "uv", EntryCommand: "python", EntryArgs: []string{"-c", "import os"}}
	if _, err := service.ConfirmSkillDependencyPlan("unsafe", plan); err == nil {
		t.Fatal("python -c entry was accepted")
	}
	plan.EntryArgs = []string{"../outside.py"}
	if _, err := service.ConfirmSkillDependencyPlan("unsafe", plan); err == nil {
		t.Fatal("traversing entry was accepted")
	}
}

func TestConfirmLegacyPipPlanMigratesToUVManifest(t *testing.T) {
	manager := NewDirectoryManager(t.TempDir())
	writeTestSkill(t, manager, "legacy", nil)
	service := NewSkillDependencyService(manager, nil)
	plan, err := service.ConfirmSkillDependencyPlan("legacy", SkillDependencyPlan{
		SchemaVersion: 1, Runtime: "python", PackageManager: "pip", DependencyFile: "requirements.txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.SchemaVersion != skillManifestSchemaVersion || plan.PackageManager != "uv" || plan.Lockfile != managedRequirementsLock {
		t.Fatalf("legacy plan was not normalized: %#v", plan)
	}
	data, err := os.ReadFile(filepath.Join(manager.Path(DirectorySkills), "legacy", skillManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	var manifest skillManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != skillManifestSchemaVersion || manifest.PackageManager.Name != "uv" {
		t.Fatalf("legacy manifest was not upgraded: %#v", manifest)
	}
}

func TestInspectManifestReturnsNonNilSlices(t *testing.T) {
	manager := NewDirectoryManager(t.TempDir())
	dir := writeTestSkill(t, manager, "manifest-skill", map[string]string{"main.py": "print('ok')"})
	manifest := `{"schemaVersion":2,"runtime":"python","entry":{"command":"python","args":["main.py"]},"packageManager":{"name":"uv","lockfile":"","dependencyFile":""},"network":false}`
	if err := os.WriteFile(filepath.Join(dir, skillManifestFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	service := NewSkillDependencyService(manager, nil)
	plan, err := service.InspectSkillDependencies("manifest-skill")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Evidence == nil || plan.DependencySources == nil || plan.EntryArgs == nil {
		t.Fatalf("manifest plan contains nil slices: %#v", plan)
	}
}

func TestInitializePythonUsesBundledUVAndRefreshesRequirementsLock(t *testing.T) {
	manager := NewDirectoryManager(t.TempDir())
	dir := writeTestSkill(t, manager, "python-skill", map[string]string{
		"requirements.txt": "requests==2.0\n",
		"main.py":          "print('ok')",
	})
	runtimeRoot := filepath.Join(t.TempDir(), "runtime")
	for _, runtime := range []struct{ directory, executable, version string }{
		{"python", "python.exe", "3.14.7"},
		{"uv", "uv.exe", "0.12.13"},
	} {
		path := filepath.Join(runtimeRoot, runtime.directory)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, runtime.executable), []byte("test"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "VERSION"), []byte(runtime.version), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	service := NewSkillDependencyService(manager, nil)
	service.resolveRuntime = func() string { return runtimeRoot }
	plan, err := service.InspectSkillDependencies("python-skill")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConfirmSkillDependencyPlan("python-skill", plan); err != nil {
		t.Fatal(err)
	}
	var calls [][]string
	service.run = func(_ context.Context, command string, args []string, _ string, env []string) ([]byte, error) {
		if command != filepath.Join(runtimeRoot, "uv", "uv.exe") {
			t.Fatalf("Python flow executed %q instead of bundled uv", command)
		}
		if !containsEnv(env, "UV_CACHE_DIR="+filepath.Join(manager.Path(DirectoryRuntimeCache), "uv")) || !containsEnv(env, "UV_PYTHON="+filepath.Join(runtimeRoot, "python", "python.exe")) || !containsEnv(env, "UV_NO_MANAGED_PYTHON=true") {
			t.Fatalf("uv environment is incomplete: %#v", env)
		}
		calls = append(calls, append([]string{}, args...))
		if len(args) > 2 && args[0] == "pip" && args[1] == "compile" {
			for index, arg := range args {
				if arg == "--output-file" {
					if err := os.WriteFile(args[index+1], []byte("requests==2.0 --hash=sha256:abc\n"), 0o644); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
		return nil, nil
	}

	first, err := service.InitializeSkill("python-skill")
	if err != nil || first.State != "ready" || first.PackageManagerVersion != "0.12.13" || first.LockHash == "" {
		t.Fatalf("first Python initialization = %#v, %v", first, err)
	}
	if _, err := os.Stat(filepath.Join(dir, managedRequirementsLock)); err != nil {
		t.Fatalf("managed requirements lock missing: %v", err)
	}
	if !containsArgs(calls, "venv") || !containsArgs(calls, "sync") || !containsArgs(calls, "--no-build") {
		t.Fatalf("expected uv venv and wheel-only sync calls, got %#v", calls)
	}
	callCount := len(calls)
	cached, err := service.InitializeSkill("python-skill")
	if err != nil || cached.PlanHash != first.PlanHash || len(calls) != callCount {
		t.Fatalf("unchanged Python dependencies did not reuse environment: %#v, %v, calls=%d", cached, err, len(calls))
	}
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("requests==2.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := service.InitializeSkill("python-skill")
	if err != nil || second.PlanHash == first.PlanHash || second.Environment == first.Environment {
		t.Fatalf("changed requirements did not rebuild environment: first=%#v second=%#v err=%v", first, second, err)
	}
}

func TestValidatePythonDependencySourcesRejectsPathsOutsideSkill(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("package @ file:///C:/outside/package.whl\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validatePythonDependencySources(dir, "requirements.txt"); err == nil {
		t.Fatal("outside file URL was accepted")
	}
}

func TestExecuteSkillAutomaticallyInitializesHighConfidencePlan(t *testing.T) {
	manager := NewDirectoryManager(t.TempDir())
	skillDir := writeTestSkill(t, manager, "watermark", map[string]string{
		"SKILL.md":             "---\nname: watermark\ndescription: test\n---\nRun `python scripts/watermark.py`.\n",
		"pyproject.toml":       "[project]\nname='watermark'\ndependencies=['Pillow>=10']\n",
		"scripts/watermark.py": "print('ok')\n",
	})
	runtimeRoot := filepath.Join(t.TempDir(), "runtime")
	for _, runtime := range []struct{ directory, executable, version string }{{"python", "python.exe", "3.14.7"}, {"uv", "uv.exe", "0.12.13"}} {
		dir := filepath.Join(runtimeRoot, runtime.directory)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, runtime.executable), []byte("test"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "VERSION"), []byte(runtime.version), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	service := NewSkillDependencyService(manager, nil)
	service.resolveRuntime = func() string { return runtimeRoot }
	service.run = func(_ context.Context, command string, args []string, workdir string, _ []string) ([]byte, error) {
		if filepath.Base(command) == "uv.exe" && len(args) > 0 && args[0] == "lock" {
			if err := os.WriteFile(filepath.Join(skillDir, "uv.lock"), []byte("version = 1\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if filepath.Base(command) == "python.exe" && strings.HasSuffix(workdir, "watermark") {
			return []byte("ok"), nil
		}
		return nil, nil
	}
	output, err := service.ExecuteSkillContext(context.Background(), "watermark", []string{"--help"})
	if err != nil || string(output) != "ok" {
		t.Fatalf("automatic execution = %q, %v", output, err)
	}
	plan, err := service.InspectSkillDependencies("watermark")
	if err != nil || plan.NeedsReview || plan.EntryCommand != "python" {
		t.Fatalf("confirmed plan = %#v, %v", plan, err)
	}
	status, err := service.GetSkillEnvironmentStatus("watermark")
	if err != nil || status.State != "ready" {
		t.Fatalf("environment status = %#v, %v", status, err)
	}
}

func TestTrimRepeatedSkillEntry(t *testing.T) {
	if got := trimRepeatedSkillEntry([]string{"scripts/watermark.py", "--input", "photo.jpg"}, []string{"scripts/watermark.py"}); !reflect.DeepEqual(got, []string{"--input", "photo.jpg"}) {
		t.Fatalf("normalized arguments = %#v", got)
	}
	if got := trimRepeatedSkillEntry([]string{"--input", "photo.jpg"}, []string{"scripts/watermark.py"}); !reflect.DeepEqual(got, []string{"--input", "photo.jpg"}) {
		t.Fatalf("arguments changed unexpectedly = %#v", got)
	}
}

func containsEnv(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func containsArgs(calls [][]string, expected string) bool {
	for _, args := range calls {
		if strings.Join(args, " ") != "" && strings.Contains(strings.Join(args, " "), expected) {
			return true
		}
	}
	return false
}

func TestInitializeSkillCachesByContentHash(t *testing.T) {
	manager := NewDirectoryManager(t.TempDir())
	dir := writeTestSkill(t, manager, "node-skill", map[string]string{
		"package.json":      `{ "name": "node-skill" }`,
		"package-lock.json": `{ "name": "node-skill", "lockfileVersion": 3 }`,
		"main.js":           "console.log('one')",
	})
	runtimeRoot := filepath.Join(t.TempDir(), "runtime")
	if err := os.MkdirAll(filepath.Join(runtimeRoot, "node"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"node.exe", "npm.cmd"} {
		if err := os.WriteFile(filepath.Join(runtimeRoot, "node", name), []byte("test"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(runtimeRoot, "node", "VERSION"), []byte("24.21.0"), 0o644); err != nil {
		t.Fatal(err)
	}
	service := NewSkillDependencyService(manager, nil)
	service.resolveRuntime = func() string { return runtimeRoot }
	plan, err := service.InspectSkillDependencies("node-skill")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConfirmSkillDependencyPlan("node-skill", plan); err != nil {
		t.Fatal(err)
	}
	runs := 0
	service.run = func(context.Context, string, []string, string, []string) ([]byte, error) { runs++; return nil, nil }

	first, err := service.InitializeSkill("node-skill")
	if err != nil || first.State != "ready" || runs != 1 {
		t.Fatalf("first initialize = %#v, %v, runs=%d", first, err, runs)
	}
	second, err := service.InitializeSkill("node-skill")
	if err != nil || second.PlanHash != first.PlanHash || runs != 1 {
		t.Fatalf("cached initialize = %#v, %v, runs=%d", second, err, runs)
	}
	repaired, err := service.RepairSkillEnvironment("node-skill")
	if err != nil || repaired.PlanHash != first.PlanHash || runs != 2 {
		t.Fatalf("repair initialize = %#v, %v, runs=%d", repaired, err, runs)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.js"), []byte("console.log('two')"), 0o644); err != nil {
		t.Fatal(err)
	}
	third, err := service.InitializeSkill("node-skill")
	if err != nil || third.PlanHash == first.PlanHash || runs != 3 {
		t.Fatalf("changed initialize = %#v, %v, runs=%d", third, err, runs)
	}
}
