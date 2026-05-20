package importer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	t              *testing.T
	createManifest bool
	commands       []string
}

func (r *fakeRunner) Run(ctx context.Context, dir string, name string, args ...string) (string, error) {
	r.commands = append(r.commands, name+" "+strings.Join(args, " "))
	if len(args) >= 3 && args[0] == "clone" {
		dest := args[2]
		if err := os.MkdirAll(filepath.Join(dest, ".git"), 0o755); err != nil {
			r.t.Fatal(err)
		}
		if r.createManifest {
			manifest := strings.Join([]string{
				"id: sample",
				"name: Sample",
				"description: imported sample service",
				"type: service",
				"root: .",
				"capabilities:",
				"  - health",
			}, "\n")
			if err := os.WriteFile(filepath.Join(dest, "agent.yaml"), []byte(manifest), 0o644); err != nil {
				r.t.Fatal(err)
			}
		}
	}
	return "ok", nil
}

func TestImportRegistersRepositoryWithManifest(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	configPath := filepath.Join(root, ".gsa", "config.yaml")
	runner := &fakeRunner{t: t, createManifest: true}
	result, err := Import(context.Background(), Options{
		RepoURL:    "https://github.com/example/sample.git",
		Workspace:  workspace,
		Home:       root,
		ConfigPath: configPath,
		Runner:     runner,
		Now:        fixedNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "cloned" {
		t.Fatalf("action = %q, want cloned", result.Action)
	}
	if !result.Registered {
		t.Fatalf("expected imported repository to be registered")
	}
	if result.ManifestPath == "" {
		t.Fatalf("expected manifest path")
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "sample/agent.yaml") && !strings.Contains(string(data), "sample\\agent.yaml") {
		t.Fatalf("config does not contain imported manifest:\n%s", string(data))
	}
	if _, err := os.Stat(result.ReportPath); err != nil {
		t.Fatal(err)
	}
}

func TestImportWritesReportWhenManifestMissing(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	configPath := filepath.Join(root, ".gsa", "config.yaml")
	runner := &fakeRunner{t: t}
	result, err := Import(context.Background(), Options{
		RepoURL:    "https://github.com/example/no-agent.git",
		Workspace:  workspace,
		Home:       root,
		ConfigPath: configPath,
		Runner:     runner,
		Now:        fixedNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Registered {
		t.Fatalf("repository without agent.yaml should not be registered")
	}
	if result.ReportPath == "" {
		t.Fatalf("expected import report path")
	}
	report, err := os.ReadFile(result.ReportPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(report), "agent_yaml: not found") {
		t.Fatalf("report should mention missing agent.yaml:\n%s", string(report))
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("config should not be written when no manifest is registered")
	}
}

func TestImportPullsExistingRepository(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	dest := filepath.Join(workspace, "sample")
	if err := os.MkdirAll(filepath.Join(dest, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{t: t}
	result, err := Import(context.Background(), Options{
		RepoURL:   "https://github.com/example/sample.git",
		Workspace: workspace,
		Home:      root,
		Dest:      dest,
		Runner:    runner,
		Now:       fixedNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "pulled" {
		t.Fatalf("action = %q, want pulled", result.Action)
	}
	if len(runner.commands) != 1 || !strings.Contains(runner.commands[0], "pull --ff-only") {
		t.Fatalf("unexpected commands: %#v", runner.commands)
	}
}

func TestRepoName(t *testing.T) {
	tests := map[string]string{
		"https://github.com/example/sample.git": "sample",
		"git@github.com:example/GameOps.git":    "GameOps",
		"F:/tmp/local-service":                  "local-service",
	}
	for input, want := range tests {
		if got := repoName(input); got != want {
			t.Fatalf("repoName(%q) = %q, want %q", input, got, want)
		}
	}
}

func fixedNow() time.Time {
	return time.Date(2026, 5, 21, 9, 0, 0, 0, time.UTC)
}
