package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/jingjie2002/GameServerProjectAgent/internal/permissions"
	"github.com/jingjie2002/GameServerProjectAgent/internal/projects"
)

func TestRunProjectCommandPermission(t *testing.T) {
	runner := Runner{}
	manifest := projects.Manifest{
		ID:   "demo",
		Root: t.TempDir(),
		Commands: map[string]projects.Command{
			"test": {Name: "test", Command: "go version", Mode: permissions.AutoReviewMode},
		},
	}
	_, err := runner.RunProjectCommand(context.Background(), permissions.DefaultMode, manifest, "test")
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expected permission denied, got %v", err)
	}
	if _, err := runner.RunProjectCommand(context.Background(), permissions.AutoReviewMode, manifest, "test"); err != nil {
		t.Fatalf("auto review should run command: %v", err)
	}
}

func TestNewGameOpsClientUsesAgentToolPaths(t *testing.T) {
	client, err := NewGameOpsClient(projects.Manifest{
		CapabilitiesEndpoint: projects.Endpoint{URL: "http://127.0.0.1:18090/api/agent/capabilities"},
		AgentTools: map[string]string{
			"gameops_preview_mail":     "POST /agent/mails/preview",
			"gameops_send_mail":        "POST /agent/mails",
			"gameops_ban_player":       "POST /agent/players/{player_id}/ban",
			"gameops_unban_player":     "POST /agent/players/{player_id}/unban",
			"gameops_freeze_cdk":       "POST /agent/cdk/{code}/freeze",
			"gameops_freeze_cdk_batch": "POST /agent/cdk/batches/{batch_id}/freeze",
		},
	})
	if err != nil {
		t.Fatalf("NewGameOpsClient: %v", err)
	}
	if client.PreviewMailPath != "/agent/mails/preview" || client.SendMailPath != "/agent/mails" {
		t.Fatalf("unexpected mail paths: %#v", client)
	}
	if got := replacePathValue(client.BanPlayerPath, "player_id", "player/a"); got != "/agent/players/player%2Fa/ban" {
		t.Fatalf("unexpected replaced path: %s", got)
	}
}
