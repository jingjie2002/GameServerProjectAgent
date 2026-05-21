package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jingjie2002/GameServerProjectAgent/internal/deploy"
	"github.com/jingjie2002/GameServerProjectAgent/internal/permissions"
	"github.com/jingjie2002/GameServerProjectAgent/internal/projects"
)

func TestStatusAPIIncludesDeployState(t *testing.T) {
	home := t.TempDir()
	logPath := filepath.Join(home, ".gsa", "services", "sample.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte("line one\nline two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := deploy.SaveState(home, deploy.ServiceState{
		ProjectID:   "sample",
		ProjectName: "Sample",
		Root:        filepath.Join(home, "workspace", "sample"),
		Command:     "go run .",
		Status:      "stopped",
		LogPath:     logPath,
		Ports:       []int{18080},
	}); err != nil {
		t.Fatal(err)
	}

	handler := NewHandler(Options{
		Home:      home,
		Workspace: filepath.Join(home, "workspace"),
		Mode:      permissions.DefaultMode,
		Manifests: []projects.Manifest{{
			ID:           "sample",
			Name:         "Sample",
			Description:  "demo service",
			Type:         "go-service",
			Root:         ".",
			Capabilities: []string{"health"},
		}},
		Now: func() time.Time { return time.Date(2026, 5, 21, 16, 10, 0, 0, time.UTC) },
	})
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var summary Summary
	if err := json.Unmarshal(rec.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.ProjectCount != 1 || summary.Services[0].ID != "sample" {
		t.Fatalf("summary = %#v", summary)
	}
	if summary.Services[0].DeployStatus != "stopped" || summary.Services[0].LogURL == "" {
		t.Fatalf("service = %#v", summary.Services[0])
	}
}

func TestIndexAndLogs(t *testing.T) {
	home := t.TempDir()
	logPath := filepath.Join(home, ".gsa", "services", "sample.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte("first\nsecond\nthird\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := deploy.SaveState(home, deploy.ServiceState{
		ProjectID:   "sample",
		ProjectName: "Sample",
		Status:      "stopped",
		LogPath:     logPath,
	}); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(Options{
		Home: home,
		Mode: permissions.DefaultMode,
		Manifests: []projects.Manifest{{
			ID:   "sample",
			Name: "Sample",
			Root: ".",
		}},
	})

	index := httptest.NewRecorder()
	handler.ServeHTTP(index, httptest.NewRequest(http.MethodGet, "/", nil))
	if index.Code != http.StatusOK {
		t.Fatalf("index status = %d", index.Code)
	}
	if !strings.Contains(index.Body.String(), "GameServerProjectAgent") || !strings.Contains(index.Body.String(), "Sample") {
		t.Fatalf("index body missing expected content:\n%s", index.Body.String())
	}

	logs := httptest.NewRecorder()
	handler.ServeHTTP(logs, httptest.NewRequest(http.MethodGet, "/api/logs/sample?tail=2", nil))
	if logs.Code != http.StatusOK {
		t.Fatalf("logs status = %d, body = %s", logs.Code, logs.Body.String())
	}
	if got := logs.Body.String(); strings.Contains(got, "first") || !strings.Contains(got, "second") || !strings.Contains(got, "third") {
		t.Fatalf("unexpected logs:\n%s", got)
	}
}
