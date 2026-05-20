package setup

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResolveHomeFindsAgentRootFromBinExecutable(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "configs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "configs", "projects.example.yaml"), []byte("projects: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(root, "bin", "gsa.exe")
	got := ResolveHome(filepath.Join(root, "bin"), exe)
	if got != filepath.Clean(root) {
		t.Fatalf("ResolveHome() = %q, want %q", got, filepath.Clean(root))
	}
}

func TestRunWizardWritesConfigAndSecret(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	for _, name := range []string{"CoreRank", "ArenaGate", "GameOps"} {
		dir := filepath.Join(workspace, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte("id: x\nname: x\nroot: .\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	configPath := filepath.Join(root, ".gsa", "config.yaml")
	input := strings.Join([]string{
		"",           // workspace default
		"1",          // DeepSeek
		"",           // base url default
		"",           // model default
		"",           // api key env default
		"secret-key", // api key
	}, "\n")
	var out bytes.Buffer
	cfg, err := RunWizard(WizardOptions{
		Home:       root,
		Workspace:  workspace,
		ConfigPath: configPath,
		In:         strings.NewReader(input),
		Out:        &out,
		Now:        func() time.Time { return time.Date(2026, 5, 20, 23, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLM.Provider != "deepseek" {
		t.Fatalf("provider = %q, want deepseek", cfg.LLM.Provider)
	}
	if len(cfg.Projects) != 3 {
		t.Fatalf("projects = %d, want 3", len(cfg.Projects))
	}
	loaded, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Workspace != filepath.Clean(workspace) {
		t.Fatalf("workspace = %q, want %q", loaded.Workspace, filepath.Clean(workspace))
	}
	secretPath := filepath.Join(root, ".gsa", "secrets.env")
	data, err := os.ReadFile(secretPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "GSA_LLM_API_KEY=secret-key") {
		t.Fatalf("secret file does not contain expected env")
	}
	if !strings.Contains(out.String(), "初始化完成") {
		t.Fatalf("wizard output missing completion message")
	}
}

func TestRunWizardCanSkipModel(t *testing.T) {
	root := t.TempDir()
	workspace := t.TempDir()
	configPath := filepath.Join(root, ".gsa", "config.yaml")
	var out bytes.Buffer
	cfg, err := RunWizard(WizardOptions{
		Home:       root,
		Workspace:  workspace,
		ConfigPath: configPath,
		In:         strings.NewReader("\n4\n"),
		Out:        &out,
		Now:        func() time.Time { return time.Date(2026, 5, 20, 23, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLM.Provider != "none" {
		t.Fatalf("provider = %q, want none", cfg.LLM.Provider)
	}
	if _, err := os.Stat(filepath.Join(root, ".gsa", "secrets.env")); !os.IsNotExist(err) {
		t.Fatalf("secrets file should not be written when model is skipped")
	}
}
