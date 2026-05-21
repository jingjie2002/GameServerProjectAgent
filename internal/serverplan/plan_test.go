package serverplan

import (
	"strings"
	"testing"
)

func TestBuildAndFormat(t *testing.T) {
	plan := Build(Options{
		Home:       `C:\gsa`,
		Workspace:  `C:\workspace`,
		ConfigPath: `C:\gsa\.gsa\config.yaml`,
		Root:       "/opt/game-agent",
	})
	if plan.LinuxHome != "/opt/game-agent" {
		t.Fatalf("LinuxHome = %q", plan.LinuxHome)
	}
	if plan.DashboardCommand == "" {
		t.Fatalf("DashboardCommand is empty")
	}
	text := Format(plan)
	for _, want := range []string{
		"轻量级服务器部署约定",
		"gsa setup",
		"gsa onboard <repo-url>",
		"dashboard 第一版只读展示",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("formatted plan missing %q:\n%s", want, text)
		}
	}
}
