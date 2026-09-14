package app

import (
	"log"
	"os"
	"path/filepath"
	"testing"
)

func TestDirectoryManagerKeepsStableLayout(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Miel")
	manager := NewDirectoryManager(root)
	if err := manager.EnsureAll(); err != nil {
		t.Fatal(err)
	}
	paths := manager.Paths()
	for _, path := range []string{
		paths.Root, paths.Logs, paths.Screenshots, paths.ClipboardSources, paths.Skills,
		paths.Reports, paths.Documents, paths.Tables, paths.Memories,
		paths.Artifacts,
		paths.ChatDrafts, paths.ChatFiles, paths.ChatThumbnails,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("managed path %q: %v", path, err)
		}
		if !info.IsDir() {
			t.Fatalf("managed path %q is not a directory", path)
		}
	}
	if paths.Database != filepath.Join(root, "blankmind.db") || paths.MemoryDatabase != filepath.Join(root, "memory.db") || paths.AGUIDatabase != filepath.Join(root, "agui.db") {
		t.Fatalf("database paths moved unexpectedly: %#v", paths)
	}
}

func TestDefaultDataRootPrefersMielAndFallsBackToLegacy(t *testing.T) {
	t.Run("fresh install", func(t *testing.T) {
		dataHome := t.TempDir()
		if got, want := defaultDataRoot(dataHome), filepath.Join(dataHome, "Miel"); got != want {
			t.Fatalf("defaultDataRoot() = %q, want %q", got, want)
		}
	})

	t.Run("legacy install", func(t *testing.T) {
		dataHome := t.TempDir()
		legacy := filepath.Join(dataHome, "BlankMind")
		if err := os.MkdirAll(legacy, 0o755); err != nil {
			t.Fatal(err)
		}
		if got := defaultDataRoot(dataHome); got != legacy {
			t.Fatalf("defaultDataRoot() = %q, want legacy root %q", got, legacy)
		}
	})

	t.Run("renamed install", func(t *testing.T) {
		dataHome := t.TempDir()
		legacy := filepath.Join(dataHome, "BlankMind")
		current := filepath.Join(dataHome, "Miel")
		for _, path := range []string{legacy, current} {
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if got := defaultDataRoot(dataHome); got != current {
			t.Fatalf("defaultDataRoot() = %q, want current root %q", got, current)
		}
	})
}

func TestDirectoryManagerOutputPathValidatesFilename(t *testing.T) {
	manager := NewDirectoryManager(t.TempDir())
	path, err := manager.OutputPath("reports", "report.md")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "report.md" {
		t.Fatalf("output path = %q", path)
	}
	if _, err := manager.OutputPath("../outside", "report.md"); err == nil {
		t.Fatal("expected output category traversal to fail")
	}
	if _, err := manager.OutputPath("reports", filepath.Join("nested", "report.md")); err == nil {
		t.Fatal("expected output filename traversal to fail")
	}
}

func TestDirectoryManagerConfigureLogging(t *testing.T) {
	manager := NewDirectoryManager(t.TempDir())
	old := log.Writer()
	defer log.SetOutput(old)
	closer, err := manager.ConfigureLogging()
	if err != nil {
		t.Fatal(err)
	}
	log.Print("directory manager test")
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(manager.LogFilePath())
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "" {
		t.Fatal("log file is empty")
	}
}
