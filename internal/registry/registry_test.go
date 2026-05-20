package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jingjie2002/GameServerProjectAgent/internal/setup"
)

func TestRegisterManifestWritesConfig(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	project := filepath.Join(workspace, "sample")
	manifestPath := filepath.Join(project, "agent.yaml")
	writeFile(t, manifestPath, strings.Join([]string{
		"id: sample",
		"name: Sample",
		"description: sample service",
		"type: service",
		"root: .",
		"capabilities:",
		"  - health",
	}, "\n"))
	configPath := filepath.Join(root, ".gsa", "config.yaml")

	result, err := RegisterManifest(Options{
		Home:       root,
		Workspace:  workspace,
		ConfigPath: configPath,
	}, manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Registered || result.AlreadyExists {
		t.Fatalf("result = %#v, want registered once", result)
	}
	cfg, err := setup.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Projects) != 1 {
		t.Fatalf("projects = %d, want 1", len(cfg.Projects))
	}
	if cfg.Projects[0].ManifestPath != filepath.Clean(manifestPath) {
		t.Fatalf("manifest path = %q", cfg.Projects[0].ManifestPath)
	}
}

func TestRegisterManifestSkipsDuplicate(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	manifestPath := filepath.Join(workspace, "sample", "agent.yaml")
	writeFile(t, manifestPath, "id: sample\nname: Sample\nroot: .\n")
	configPath := filepath.Join(root, ".gsa", "config.yaml")

	if _, err := RegisterManifest(Options{Home: root, Workspace: workspace, ConfigPath: configPath}, manifestPath); err != nil {
		t.Fatal(err)
	}
	result, err := RegisterManifest(Options{Home: root, Workspace: workspace, ConfigPath: configPath}, filepath.Clean(manifestPath))
	if err != nil {
		t.Fatal(err)
	}
	if result.Registered || !result.AlreadyExists {
		t.Fatalf("result = %#v, want duplicate", result)
	}
	cfg, err := setup.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Projects) != 1 {
		t.Fatalf("projects = %d, want 1", len(cfg.Projects))
	}
}

func TestRegisterManifestRejectsInvalidManifest(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "bad", "agent.yaml")
	writeFile(t, manifestPath, "id: bad\n")
	configPath := filepath.Join(root, ".gsa", "config.yaml")

	if _, err := RegisterManifest(Options{Home: root, Workspace: root, ConfigPath: configPath}, manifestPath); err == nil {
		t.Fatalf("expected invalid manifest to fail")
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("config should not be written for invalid manifest")
	}
}

func writeFile(t *testing.T, path string, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}
