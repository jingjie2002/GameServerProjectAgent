package generated

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jingjie2002/GameServerProjectAgent/internal/setup"
)

func TestRegisterGeneratedPreviewDoesNotWriteConfig(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "workspace", "sample-service")
	manifestPath := filepath.Join(project, "agent.generated.yaml")
	writeGeneratedFile(t, manifestPath)
	configPath := filepath.Join(root, ".gsa", "config.yaml")

	result, err := Register(Options{
		Path:       project,
		Home:       root,
		Workspace:  filepath.Dir(project),
		ConfigPath: configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Confirmed || result.Registered {
		t.Fatalf("result = %#v, want preview only", result)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("preview should not write config")
	}
	output := FormatResult(result)
	if !strings.Contains(output, "status: preview_only") || !strings.Contains(output, "--confirm") {
		t.Fatalf("preview output missing confirmation hint:\n%s", output)
	}
}

func TestRegisterGeneratedConfirmWritesConfig(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "workspace", "sample-service")
	manifestPath := filepath.Join(project, "agent.generated.yaml")
	writeGeneratedFile(t, manifestPath)
	configPath := filepath.Join(root, ".gsa", "config.yaml")

	result, err := Register(Options{
		Path:       manifestPath,
		Home:       root,
		Workspace:  filepath.Dir(project),
		ConfigPath: configPath,
		Confirm:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Confirmed || !result.Registered {
		t.Fatalf("result = %#v, want confirmed registration", result)
	}
	cfg, err := setup.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Projects) != 1 || cfg.Projects[0].ManifestPath != filepath.Clean(manifestPath) {
		t.Fatalf("config projects = %#v", cfg.Projects)
	}
}

func TestRegisterGeneratedRejectsMissingPath(t *testing.T) {
	if _, err := Register(Options{Path: filepath.Join(t.TempDir(), "missing")}); err == nil {
		t.Fatalf("expected missing path to fail")
	}
}

func writeGeneratedFile(t *testing.T, path string) {
	t.Helper()
	data := strings.Join([]string{
		"id: sample-service",
		"name: Sample Service",
		"description: generated sample",
		"type: imported-service",
		"root: .",
		"health:",
		"  url: http://127.0.0.1:18090/healthz",
		"capabilities_endpoint:",
		"  url: http://127.0.0.1:18090/api/agent/capabilities",
		"capabilities:",
		"  - health",
	}, "\n")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}
