package scanner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScanGoProjectGeneratesDrafts(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "sample-service")
	writeFile(t, filepath.Join(project, "go.mod"), "module github.com/example/sample-service\n\ngo 1.25\nrequire github.com/redis/go-redis/v9 v9.0.0\n")
	writeFile(t, filepath.Join(project, "cmd", "server", "main.go"), `package main

import "net/http"

func main() {
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {})
	http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {})
	_ = http.ListenAndServe(":18090", nil)
}
`)
	writeFile(t, filepath.Join(project, ".env.example"), "MYSQL_DSN=\nREDIS_ADDR=127.0.0.1:6379\n")
	writeFile(t, filepath.Join(project, "Dockerfile"), "FROM golang:1.25\nEXPOSE 18090\n")
	writeFile(t, filepath.Join(project, "README.md"), "# Sample\n\n```powershell\ngo run ./cmd/server\n```\n")

	result, err := Scan(Options{
		Path: project,
		Home: root,
		Now:  fixedNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Language != "go" {
		t.Fatalf("language = %q, want go", result.Language)
	}
	if result.ModuleName != "github.com/example/sample-service" {
		t.Fatalf("module = %q", result.ModuleName)
	}
	if !contains(result.Entrypoints, "cmd/server") {
		t.Fatalf("entrypoints = %#v, want cmd/server", result.Entrypoints)
	}
	if !containsInt(result.Ports, 18090) {
		t.Fatalf("ports = %#v, want 18090", result.Ports)
	}
	if !contains(result.Dependencies, "redis") || !contains(result.Dependencies, "mysql") {
		t.Fatalf("dependencies = %#v, want redis and mysql", result.Dependencies)
	}
	if !contains(result.HealthPaths, "/healthz") || !contains(result.HealthPaths, "/metrics") {
		t.Fatalf("health paths = %#v", result.HealthPaths)
	}
	for _, path := range []string{result.AgentGeneratedPath, result.DeployGeneratedPath, result.ReportPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected generated file %s: %v", path, err)
		}
	}
	agentData := readFile(t, result.AgentGeneratedPath)
	if !strings.Contains(agentData, "id: sample-service") || !strings.Contains(agentData, "http://127.0.0.1:18090/healthz") {
		t.Fatalf("unexpected agent generated yaml:\n%s", agentData)
	}
	deployData := readFile(t, result.DeployGeneratedPath)
	if !strings.Contains(deployData, "command: go run ./cmd/server") || !strings.Contains(deployData, "MYSQL_DSN") {
		t.Fatalf("unexpected deploy generated yaml:\n%s", deployData)
	}
}

func TestScanUnknownProjectStillWritesReport(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "unknown-service")
	writeFile(t, filepath.Join(project, "README.md"), "# Unknown\n")
	result, err := Scan(Options{Path: project, Home: root, Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	if result.Confidence != "low" {
		t.Fatalf("confidence = %q, want low", result.Confidence)
	}
	if _, err := os.Stat(result.ReportPath); err != nil {
		t.Fatalf("expected scan report: %v", err)
	}
}

func TestScanMissingPathFails(t *testing.T) {
	if _, err := Scan(Options{Path: filepath.Join(t.TempDir(), "missing")}); err == nil {
		t.Fatalf("expected missing path to fail")
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

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func containsInt(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func fixedNow() time.Time {
	return time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
}
