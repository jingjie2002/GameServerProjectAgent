package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/jingjie2002/GameServerProjectAgent/internal/permissions"
	"github.com/jingjie2002/GameServerProjectAgent/internal/projects"
)

func TestSessionProjectAndModeCommands(t *testing.T) {
	session := NewSession(permissions.DefaultMode, []projects.Manifest{{
		ID:           "demo",
		Name:         "Demo",
		Description:  "demo project",
		Capabilities: []string{"health"},
	}}, nil, nil)
	if got := session.Handle(context.Background(), "/项目"); !strings.Contains(got, "demo") {
		t.Fatalf("expected project list, got %q", got)
	}
	if got := session.Handle(context.Background(), "/模式 自动审查"); !strings.Contains(got, "自动审查") {
		t.Fatalf("expected mode switch, got %q", got)
	}
	if session.Mode != permissions.AutoReviewMode {
		t.Fatalf("expected auto review mode, got %s", session.Mode)
	}
}

func TestSessionHelpMentionsDiagnostics(t *testing.T) {
	session := NewSession(permissions.DefaultMode, nil, nil, nil)
	got := session.Handle(context.Background(), "/帮助")
	for _, want := range []string{"/健康", "/诊断", "/风险", "/gm"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected help to contain %s, got %q", want, got)
		}
	}
}
