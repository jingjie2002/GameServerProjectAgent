package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jingjie2002/GameServerProjectAgent/internal/permissions"
	"github.com/jingjie2002/GameServerProjectAgent/internal/projects"
)

func TestCapabilitiesIncludesLiveDiff(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"capabilities":["health","live_only"],"agent_tools":{"tool_live":"GET /live"}}`))
	}))
	defer server.Close()

	session := NewSession(permissions.DefaultMode, []projects.Manifest{{
		ID:           "demo",
		Name:         "Demo",
		Capabilities: []string{"health", "manifest_only"},
		CapabilitiesEndpoint: projects.Endpoint{
			URL: server.URL,
		},
		AgentTools: map[string]string{"tool_manifest": "GET /manifest"},
	}}, nil, nil)

	got := session.Handle(context.Background(), "/capabilities demo")
	for _, want := range []string{"live: ok", "missing_in_live=manifest_only", "missing_in_manifest=live_only", "live_agent_tools_diff"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected output to contain %q, got %q", want, got)
		}
	}
}
