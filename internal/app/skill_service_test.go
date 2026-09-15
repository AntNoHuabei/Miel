package app

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestSkillServiceListsBuiltinsAndManagesImportedSkill(t *testing.T) {
	root := t.TempDir()
	manager := NewDirectoryManager(root)
	if err := manager.EnsureAll(); err != nil {
		t.Fatal(err)
	}
	ensureBuiltinSkills(manager.Path(DirectorySkills))
	svc := NewSkillService(manager)
	items, err := svc.ListSkills()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 || items[0].Source != "builtin" {
		t.Fatalf("unexpected builtin skills: %#v", items)
	}

	local := filepath.Join(root, "custom")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: meeting-notes\ndescription: Extract meeting notes\n---\n# Meeting notes\n"
	if err := os.WriteFile(filepath.Join(local, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	installed, err := svc.ImportLocal(local)
	if err != nil {
		t.Fatal(err)
	}
	if installed.Name != "meeting-notes" || installed.Source != "local" || !installed.Enabled {
		t.Fatalf("unexpected installed skill: %#v", installed)
	}
	if err := svc.SetEnabled("meeting-notes", false); err != nil {
		t.Fatal(err)
	}
	items, err = svc.ListSkills()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Name == "meeting-notes" && item.Enabled {
			t.Fatal("imported skill remained enabled")
		}
	}
	if err := svc.Delete("todo"); err == nil {
		t.Fatal("builtin skill was deleted")
	}
	if err := svc.Delete("meeting-notes"); err != nil {
		t.Fatal(err)
	}
}

func TestSkillServiceImportsZipAndRejectsTraversal(t *testing.T) {
	manager := NewDirectoryManager(t.TempDir())
	svc := NewSkillService(manager)
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	file, err := archive.Create("bundle/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("---\nname: zip-skill\ndescription: From zip\n---\n")); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(t.TempDir(), "skill.zip")
	if err := os.WriteFile(zipPath, buffer.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	item, err := svc.ImportLocal(zipPath)
	if err != nil || item.Name != "zip-skill" {
		t.Fatalf("zip import = %#v, %v", item, err)
	}

	var bad bytes.Buffer
	badArchive := zip.NewWriter(&bad)
	if _, err := badArchive.Create("../escape/SKILL.md"); err != nil {
		t.Fatal(err)
	}
	if err := badArchive.Close(); err != nil {
		t.Fatal(err)
	}
	badPath := filepath.Join(t.TempDir(), "bad.zip")
	if err := os.WriteFile(badPath, bad.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ImportLocal(badPath); err == nil {
		t.Fatal("traversal archive was accepted")
	}
}

func TestSkillServiceDisablesImportedRuntimeSkillUntilConfirmation(t *testing.T) {
	manager := NewDirectoryManager(t.TempDir())
	svc := NewSkillService(manager)
	svc.dependencies = NewSkillDependencyService(manager, nil)
	source := filepath.Join(t.TempDir(), "watermark")
	if err := os.MkdirAll(filepath.Join(source, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"SKILL.md":             "---\nname: watermark\ndescription: test\n---\nRun `python scripts/watermark.py`.\n",
		"pyproject.toml":       "[project]\nname='watermark'\ndependencies=['Pillow>=10']\n",
		"scripts/watermark.py": "from PIL import Image\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(source, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	installed, err := svc.ImportLocal(source)
	if err != nil {
		t.Fatal(err)
	}
	if installed.Enabled {
		t.Fatal("runtime skill was enabled before dependency confirmation")
	}
	items, err := svc.ListSkills()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Name == "watermark" && item.Enabled {
			t.Fatal("persisted runtime skill state remained enabled")
		}
	}
}

func TestManagedSkillRepositoryHidesDisabledSkills(t *testing.T) {
	root := t.TempDir()
	manager := NewDirectoryManager(root)
	if err := manager.EnsureAll(); err != nil {
		t.Fatal(err)
	}
	ensureBuiltinSkills(manager.Path(DirectorySkills))
	svc := NewSkillService(manager)
	if err := svc.SetEnabled("todo", false); err != nil {
		t.Fatal(err)
	}
	repo, err := newManagedSkillRepository(manager.Path(DirectorySkills))
	if err != nil {
		t.Fatal(err)
	}
	for _, summary := range repo.Summaries() {
		if summary.Name == "todo" {
			t.Fatal("disabled skill remained visible to the agent")
		}
	}
	if _, err := repo.Get("todo"); err == nil {
		t.Fatal("disabled skill remained loadable")
	}
}

func TestSkillServiceImportsSkillHubMarkdownResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/markdown")
		_, _ = w.Write([]byte("# remote-skill\n\nA remote skill."))
	}))
	defer server.Close()
	manager := NewDirectoryManager(t.TempDir())
	svc := NewSkillService(manager)
	svc.client = server.Client()
	item, err := svc.ImportFromSkillHub(server.URL + "/remote-skill")
	if err != nil {
		t.Fatal(err)
	}
	if item.Name != "remote-skill" || item.Source != "skillhub" {
		t.Fatalf("unexpected SkillHub import: %#v", item)
	}
}

func TestNormalizeSkillHubDownloadURL(t *testing.T) {
	got := normalizeSkillHubDownloadURL("https://skillhub.cn/skills/meeting-notes")
	want := "https://api.skillhub.cn/api/v1/download?slug=meeting-notes"
	if got != want {
		t.Fatalf("normalized URL = %q, want %q", got, want)
	}
}

func TestSkillServiceListsSkillHubCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("keyword") != "office" || request.URL.Query().Get("page") != "2" {
			t.Errorf("unexpected query: %s", request.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"skills":[{"slug":"office-tools","name":"Office Tools","description":"Fallback description","downloads":42,"version":"1.2.0"}],"total":13}}`))
	}))
	defer server.Close()

	svc := NewSkillService(NewDirectoryManager(t.TempDir()))
	svc.client = rewriteHostClient(server.URL, server.Client())
	page, err := svc.ListSkillHubSkills(2, 6, "office")
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 13 || page.Pages != 3 || len(page.Skills) != 1 || page.Skills[0].Description != "Fallback description" {
		t.Fatalf("unexpected catalog page: %#v", page)
	}
}

func rewriteHostClient(target string, base *http.Client) *http.Client {
	client := *base
	client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		clone := request.Clone(request.Context())
		parsed, _ := url.Parse(target)
		clone.URL.Scheme = parsed.Scheme
		clone.URL.Host = parsed.Host
		return http.DefaultTransport.RoundTrip(clone)
	})
	return &client
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return fn(request) }
