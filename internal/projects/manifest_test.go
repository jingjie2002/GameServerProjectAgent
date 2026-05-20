package projects

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseManifest(t *testing.T) {
	manifest, err := ParseManifest([]byte(`
id: demo
name: Demo
description: 示例
type: go-service
root: F:/AI编程/简历/Demo

health:
  url: http://127.0.0.1:1/healthz
commands:
  test:
    command: go test ./...
    mode: 自动审查
docs:
  - README.md
capabilities:
  - health
agent_tools:
  demo_tool: GET /demo
forbidden:
  - direct_delete
`))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if manifest.ID != "demo" || manifest.Name != "Demo" {
		t.Fatalf("unexpected identity: %#v", manifest)
	}
	if manifest.Commands["test"].Command != "go test ./..." {
		t.Fatalf("unexpected command: %#v", manifest.Commands)
	}
	if manifest.AgentTools["demo_tool"] != "GET /demo" {
		t.Fatalf("unexpected agent tools: %#v", manifest.AgentTools)
	}
}

func TestParseManifestWithUTF8BOM(t *testing.T) {
	manifest, err := ParseManifest([]byte("\ufeffid: demo\nname: Demo\nroot: .\n"))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if manifest.ID != "demo" {
		t.Fatalf("unexpected id: %q", manifest.ID)
	}
}

func TestFindWorkspaceWalksUp(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"CoreRank", "ArenaGate", "GameOps"} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte("id: test\n"), 0o644); err != nil {
			t.Fatalf("write manifest: %v", err)
		}
	}
	nested := filepath.Join(root, "GameServerProjectAgent", "cmd", "gsa")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	if got := FindWorkspace(nested); got != root {
		t.Fatalf("expected workspace %s, got %s", root, got)
	}
}
