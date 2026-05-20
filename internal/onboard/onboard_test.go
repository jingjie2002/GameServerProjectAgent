package onboard

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jingjie2002/GameServerProjectAgent/internal/setup"
)

type fakeRunner struct {
	t              *testing.T
	createManifest bool
}

func (r *fakeRunner) Run(ctx context.Context, dir string, name string, args ...string) (string, error) {
	if len(args) >= 3 && args[0] == "clone" {
		dest := args[2]
		if err := os.MkdirAll(filepath.Join(dest, ".git"), 0o755); err != nil {
			r.t.Fatal(err)
		}
		if r.createManifest {
			writeFile(tRef{r.t}, filepath.Join(dest, "agent.yaml"), strings.Join([]string{
				"id: sample-service",
				"name: Sample Service",
				"description: agent ready service",
				"type: service",
				"root: .",
				"capabilities:",
				"  - health",
			}, "\n"))
			return "ok", nil
		}
		writeFile(tRef{r.t}, filepath.Join(dest, "go.mod"), "module github.com/example/sample-service\n\ngo 1.25\n")
		writeFile(tRef{r.t}, filepath.Join(dest, "cmd", "server", "main.go"), `package main

import "net/http"

func main() {
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {})
	_ = http.ListenAndServe(":18090", nil)
}
`)
		writeFile(tRef{r.t}, filepath.Join(dest, "README.md"), "# Sample\n\ngo run ./cmd/server\n")
	}
	return "ok", nil
}

func TestRunImportsScansAndRegistersGenerated(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	configPath := filepath.Join(root, ".gsa", "config.yaml")
	var out bytes.Buffer

	result, err := Run(context.Background(), Options{
		RepoURL:    "https://github.com/example/sample-service.git",
		Home:       root,
		Workspace:  workspace,
		ConfigPath: configPath,
		In:         strings.NewReader("\ny\ny\n"),
		Out:        &out,
		Runner:     &fakeRunner{t: t},
		Now:        fixedNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Scanned || !result.Registered {
		t.Fatalf("result = %#v, want scanned and registered", result)
	}
	cfg, err := setup.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Projects) != 1 {
		t.Fatalf("projects = %d, want 1", len(cfg.Projects))
	}
	if !strings.HasSuffix(filepath.ToSlash(cfg.Projects[0].ManifestPath), "sample-service/agent.generated.yaml") {
		t.Fatalf("registered manifest = %q", cfg.Projects[0].ManifestPath)
	}
	if !strings.Contains(out.String(), "导入向导完成") {
		t.Fatalf("output missing completion message:\n%s", out.String())
	}
}

func TestRunStopsAfterAgentReadyManifest(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	configPath := filepath.Join(root, ".gsa", "config.yaml")
	var out bytes.Buffer

	result, err := Run(context.Background(), Options{
		RepoURL:    "https://github.com/example/sample-service.git",
		Home:       root,
		Workspace:  workspace,
		ConfigPath: configPath,
		In:         strings.NewReader("\n"),
		Out:        &out,
		Runner:     &fakeRunner{t: t, createManifest: true},
		Now:        fixedNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Scanned {
		t.Fatalf("agent-ready repository should not be scanned")
	}
	if !result.Registered {
		t.Fatalf("agent-ready repository should be registered")
	}
	cfg, err := setup.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Projects) != 1 || !strings.HasSuffix(filepath.ToSlash(cfg.Projects[0].ManifestPath), "sample-service/agent.yaml") {
		t.Fatalf("config projects = %#v", cfg.Projects)
	}
}

func TestRunCanSkipGeneratedRegistration(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	configPath := filepath.Join(root, ".gsa", "config.yaml")

	result, err := Run(context.Background(), Options{
		RepoURL:    "https://github.com/example/sample-service.git",
		Home:       root,
		Workspace:  workspace,
		ConfigPath: configPath,
		In:         strings.NewReader("\ny\nn\n"),
		Out:        &bytes.Buffer{},
		Runner:     &fakeRunner{t: t},
		Now:        fixedNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Scanned || !result.SkippedRegistration {
		t.Fatalf("result = %#v, want scanned and skipped registration", result)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("config should not be written when registration is skipped")
	}
}

type tRef struct {
	*testing.T
}

func writeFile(t tRef, path string, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fixedNow() time.Time {
	return time.Date(2026, 5, 21, 3, 0, 0, 0, time.UTC)
}
