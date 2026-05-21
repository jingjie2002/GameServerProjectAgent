package deploy

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jingjie2002/GameServerProjectAgent/internal/permissions"
	"github.com/jingjie2002/GameServerProjectAgent/internal/projects"
)

func TestParseSpec(t *testing.T) {
	spec, err := ParseSpec([]byte(strings.Join([]string{
		"runtime: go",
		"confidence: high",
		"build:",
		"  command: go build ./...",
		"run:",
		"  command: go run ./cmd/server",
		"ports:",
		"  - name: http",
		"    value: 18090",
		"dependencies:",
		"  - redis",
		"env:",
		"  - name: REDIS_ADDR",
		"    required: true",
		"health_check:",
		"  path: /healthz",
	}, "\n")))
	if err != nil {
		t.Fatal(err)
	}
	if spec.Runtime != "go" || spec.BuildCommand != "go build ./..." || spec.RunCommand != "go run ./cmd/server" {
		t.Fatalf("unexpected spec: %#v", spec)
	}
	if len(spec.Ports) != 1 || spec.Ports[0].Name != "http" || spec.Ports[0].Value != 18090 {
		t.Fatalf("ports = %#v", spec.Ports)
	}
	if len(spec.Env) != 1 || !spec.Env[0].Required {
		t.Fatalf("env = %#v", spec.Env)
	}
	if spec.HealthPath != "/healthz" {
		t.Fatalf("health path = %q", spec.HealthPath)
	}
}

func TestResolveSpecPathAndProjectRoot(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "sample")
	writeFile(t, filepath.Join(projectDir, "agent.generated.yaml"), "id: sample\nname: Sample\nroot: .\n")
	writeFile(t, filepath.Join(projectDir, "deploy.generated.yaml"), "run:\n  command: go run .\n")
	project := projects.Manifest{ID: "sample", Name: "Sample", Root: ".", ManifestPath: filepath.Join(projectDir, "agent.generated.yaml")}

	gotRoot := ProjectRoot(project)
	if gotRoot != projectDir {
		t.Fatalf("root = %q, want %q", gotRoot, projectDir)
	}
	specPath, err := ResolveSpecPath(project, gotRoot)
	if err != nil {
		t.Fatal(err)
	}
	if specPath != filepath.Join(projectDir, "deploy.generated.yaml") {
		t.Fatalf("spec path = %q", specPath)
	}
}

func TestSaveLoadState(t *testing.T) {
	root := t.TempDir()
	state := ServiceState{
		ProjectID:   "sample",
		ProjectName: "Sample",
		Root:        root,
		Command:     "go run .",
		PID:         123,
		Status:      "running",
		LogPath:     filepath.Join(root, ".gsa", "services", "sample.log"),
	}
	if err := SaveState(root, state); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadState(root, "sample")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ProjectID != state.ProjectID || loaded.Command != state.Command {
		t.Fatalf("loaded = %#v", loaded)
	}
}

func TestManagerStartStop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows process trees are covered by command-level smoke; unit start/stop is flaky in sandbox")
	}
	root := t.TempDir()
	projectDir := filepath.Join(root, "sample")
	runCommand := "sleep 30"
	writeFile(t, filepath.Join(projectDir, "agent.generated.yaml"), "id: sample\nname: Sample\nroot: .\n")
	writeFile(t, filepath.Join(projectDir, "deploy.generated.yaml"), "runtime: test\nrun:\n  command: "+runCommand+"\n")
	project := projects.Manifest{ID: "sample", Name: "Sample", Root: ".", ManifestPath: filepath.Join(projectDir, "agent.generated.yaml")}
	manager := Manager{
		Home: root,
		Mode: permissions.FullAccessMode,
		Now:  func() time.Time { return time.Date(2026, 5, 21, 3, 10, 0, 0, time.UTC) },
	}

	state, err := manager.Start(project, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = manager.Stop(project.ID, true)
	}()
	if state.PID <= 0 || state.Status != "running" {
		t.Fatalf("state = %#v", state)
	}
	status := manager.Status(project)
	if status.Status != "running" {
		t.Fatalf("status = %#v, want running", status)
	}
	stopped, err := manager.Stop(project.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Status != "stopped" {
		t.Fatalf("stopped = %#v", stopped)
	}
}

func TestStartRequiresFullAccessAndConfirm(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "sample")
	writeFile(t, filepath.Join(projectDir, "agent.generated.yaml"), "id: sample\nname: Sample\nroot: .\n")
	writeFile(t, filepath.Join(projectDir, "deploy.generated.yaml"), "runtime: test\nrun:\n  command: echo ok\n")
	project := projects.Manifest{ID: "sample", Name: "Sample", Root: ".", ManifestPath: filepath.Join(projectDir, "agent.generated.yaml")}

	if _, err := (Manager{Home: root, Mode: permissions.FullAccessMode}).Start(project, false); err == nil {
		t.Fatalf("expected missing confirm to fail")
	}
	if _, err := (Manager{Home: root, Mode: permissions.DefaultMode}).Start(project, true); err == nil {
		t.Fatalf("expected default mode to fail")
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
