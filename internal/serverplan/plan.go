package serverplan

import (
	"fmt"
	"path/filepath"
	"strings"
)

type Options struct {
	Home       string
	Workspace  string
	ConfigPath string
	Root       string
}

type Plan struct {
	Home              string
	Workspace         string
	ConfigPath        string
	ServiceStateDir   string
	ServiceLogPattern string
	ImportReportDir   string
	LinuxHome         string
	LinuxWorkspace    string
	LinuxConfigPath   string
	DashboardCommand  string
}

func Build(opts Options) Plan {
	home := cleanOrDot(opts.Home)
	workspace := cleanOrDot(opts.Workspace)
	configPath := cleanOrDot(opts.ConfigPath)
	linuxHome := "/opt/gsa"
	if strings.TrimSpace(opts.Root) != "" {
		linuxHome = filepath.ToSlash(filepath.Clean(opts.Root))
	}
	linuxWorkspace := "/srv/gsa/workspace"
	if strings.TrimSpace(opts.Root) != "" {
		linuxWorkspace = filepath.ToSlash(filepath.Join(filepath.Clean(opts.Root), "workspace"))
	}
	return Plan{
		Home:              home,
		Workspace:         workspace,
		ConfigPath:        configPath,
		ServiceStateDir:   filepath.Join(home, ".gsa", "services"),
		ServiceLogPattern: filepath.Join(home, ".gsa", "services", "<project-id>.log"),
		ImportReportDir:   filepath.Join(home, ".gsa", "imports"),
		LinuxHome:         linuxHome,
		LinuxWorkspace:    linuxWorkspace,
		LinuxConfigPath:   filepath.ToSlash(filepath.Join(linuxHome, ".gsa", "config.yaml")),
		DashboardCommand:  "gsa dashboard --host 127.0.0.1 --port 18088",
	}
}

func Format(plan Plan) string {
	var lines []string
	lines = append(lines, "轻量级服务器部署约定")
	lines = append(lines, "")
	lines = append(lines, "当前本机约定：")
	lines = append(lines, fmt.Sprintf("- GSA_HOME: %s", plan.Home))
	lines = append(lines, fmt.Sprintf("- workspace: %s", plan.Workspace))
	lines = append(lines, fmt.Sprintf("- config: %s", plan.ConfigPath))
	lines = append(lines, fmt.Sprintf("- service_state: %s", plan.ServiceStateDir))
	lines = append(lines, fmt.Sprintf("- service_logs: %s", plan.ServiceLogPattern))
	lines = append(lines, fmt.Sprintf("- import_reports: %s", plan.ImportReportDir))
	lines = append(lines, "")
	lines = append(lines, "Linux 轻量服务器建议：")
	lines = append(lines, fmt.Sprintf("- GSA_HOME=%s", plan.LinuxHome))
	lines = append(lines, fmt.Sprintf("- GSA_WORKSPACE=%s", plan.LinuxWorkspace))
	lines = append(lines, fmt.Sprintf("- GSA_CONFIG=%s", plan.LinuxConfigPath))
	lines = append(lines, "- modules=<workspace>/<module-name>")
	lines = append(lines, "- services=<GSA_HOME>/.gsa/services")
	lines = append(lines, "")
	lines = append(lines, "建议操作顺序：")
	lines = append(lines, "1. gsa setup")
	lines = append(lines, "2. gsa onboard <repo-url>")
	lines = append(lines, "3. gsa deploy plan <project_id>")
	lines = append(lines, "4. gsa dashboard --host 127.0.0.1 --port 18088")
	lines = append(lines, "")
	lines = append(lines, "安全边界：")
	lines = append(lines, "- 本命令只展示目录和管理约定，不创建目录，不启动服务。")
	lines = append(lines, "- dashboard 第一版只读展示，不提供启停按钮。")
	lines = append(lines, "- 真正 start/stop 仍需 --mode 完全访问权限 和 --confirm。")
	return strings.Join(lines, "\n")
}

func cleanOrDot(value string) string {
	if strings.TrimSpace(value) == "" {
		return "."
	}
	return filepath.Clean(value)
}
