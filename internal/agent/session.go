package agent

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jingjie2002/GameServerProjectAgent/internal/audit"
	"github.com/jingjie2002/GameServerProjectAgent/internal/diagnostics"
	"github.com/jingjie2002/GameServerProjectAgent/internal/llm"
	"github.com/jingjie2002/GameServerProjectAgent/internal/permissions"
	"github.com/jingjie2002/GameServerProjectAgent/internal/projects"
	"github.com/jingjie2002/GameServerProjectAgent/internal/tools"
)

type Session struct {
	Mode           permissions.Mode
	Projects       []projects.Manifest
	Audit          *audit.Store
	Runner         tools.Runner
	MockLLM        llm.Mock
	Out            io.Writer
	AgentSessionID string
	Now            func() time.Time
}

func NewSession(mode permissions.Mode, manifests []projects.Manifest, store *audit.Store, out io.Writer) *Session {
	now := time.Now
	return &Session{
		Mode:           mode,
		Projects:       manifests,
		Audit:          store,
		Runner:         tools.Runner{Audit: store},
		MockLLM:        llm.Mock{},
		Out:            out,
		AgentSessionID: "gsa-" + strconv.FormatInt(now().UnixMilli(), 10),
		Now:            now,
	}
}

func (s *Session) Handle(ctx context.Context, input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}
	if !strings.HasPrefix(input, "/") {
		input = s.MockLLM.Plan(input)
	}
	fields, err := splitCommandLine(input)
	if err != nil {
		return "命令解析失败：" + err.Error()
	}
	if len(fields) == 0 {
		return ""
	}
	command := strings.TrimPrefix(fields[0], "/")
	args := fields[1:]
	switch command {
	case "帮助", "help":
		return s.help()
	case "模式", "mode":
		return s.setMode(args)
	case "项目", "projects":
		return s.listProjects()
	case "能力", "capabilities":
		return s.capabilities(ctx, args)
	case "检查", "check":
		return s.runChecks(ctx, args)
	case "诊断", "diagnose", "状态", "status":
		return s.diagnose(ctx, true)
	case "健康", "health":
		return s.diagnose(ctx, false)
	case "风险", "risk":
		return s.risk(ctx)
	case "gm", "GM":
		return s.handleGM(ctx, args)
	case "审计", "audit":
		return s.auditLog(args)
	case "退出", "exit", "quit":
		return "bye"
	default:
		return "未知命令，输入 /帮助 查看可用命令。"
	}
}

func (s *Session) help() string {
	return strings.Join([]string{
		"可用命令：",
		"/项目",
		"/能力 [project_id]",
		"/模式 默认权限|自动审查|完全访问权限",
		"/检查 [all|project_id] [test|vet|smoke|all]",
		"/健康",
		"/诊断",
		"/风险",
		"/gm preview-mail|send-mail|ban-player|unban-player|freeze-cdk ...",
		"/审计 [limit]",
		"gsa setup（重新运行初始化向导）",
		"/退出",
	}, "\n")
}

func (s *Session) setMode(args []string) string {
	if len(args) == 0 {
		return "当前模式：" + s.Mode.String()
	}
	mode, err := permissions.ParseMode(strings.Join(args, " "))
	if err != nil {
		return err.Error()
	}
	s.Mode = mode
	if s.Audit != nil {
		_ = s.Audit.Append(audit.Event{Mode: s.Mode.String(), Action: "set_mode", Status: "ok"})
	}
	return "已切换模式：" + s.Mode.String()
}

func (s *Session) listProjects() string {
	var lines []string
	lines = append(lines, "可管理项目：")
	for _, project := range s.Projects {
		lines = append(lines, fmt.Sprintf("- %s (%s)：%s", project.ID, project.Name, project.Description))
	}
	return strings.Join(lines, "\n")
}

func (s *Session) capabilities(ctx context.Context, args []string) string {
	selected := s.Projects
	if len(args) > 0 {
		project, ok := s.findProject(args[0])
		if !ok {
			return "找不到项目：" + args[0]
		}
		selected = []projects.Manifest{project}
	}
	var lines []string
	for _, project := range selected {
		lines = append(lines, fmt.Sprintf("%s (%s)", project.ID, project.Name))
		lines = append(lines, "  capabilities: "+strings.Join(project.Capabilities, ", "))
		if project.CapabilitiesEndpoint.URL != "" {
			lines = append(lines, "  endpoint: "+project.CapabilitiesEndpoint.URL)
		}
		if len(project.AgentTools) > 0 {
			var names []string
			for name := range project.AgentTools {
				names = append(names, name)
			}
			sort.Strings(names)
			lines = append(lines, "  agent_tools: "+strings.Join(names, ", "))
		}
		lines = append(lines, liveCapabilitiesLines(ctx, project)...)
	}
	return strings.Join(lines, "\n")
}

func (s *Session) runChecks(ctx context.Context, args []string) string {
	target := "all"
	commandName := "all"
	if len(args) > 0 {
		target = args[0]
	}
	if len(args) > 1 {
		commandName = args[1]
	}
	var selected []projects.Manifest
	if target == "all" || target == "三项目" || target == "全部" {
		selected = s.Projects
	} else if project, ok := s.findProject(target); ok {
		selected = []projects.Manifest{project}
	} else {
		return "找不到项目：" + target
	}
	names := []string{commandName}
	if commandName == "all" || commandName == "全部" {
		names = []string{"test", "vet"}
	}
	var lines []string
	for _, project := range selected {
		for _, name := range names {
			result, err := s.Runner.RunProjectCommand(ctx, s.Mode, project, name)
			if err != nil {
				detail := strings.TrimSpace(result.Output)
				if detail != "" {
					lines = append(lines, fmt.Sprintf("%s %s: %v\n%s", project.ID, name, err, detail))
				} else {
					lines = append(lines, fmt.Sprintf("%s %s: %v", project.ID, name, err))
				}
				continue
			}
			lines = append(lines, fmt.Sprintf("%s %s: %s (%s)", project.ID, name, result.Status, result.Duration.Round(1_000_000)))
		}
	}
	return strings.Join(lines, "\n")
}

func (s *Session) auditLog(args []string) string {
	if s.Audit == nil {
		return "审计日志未启用。"
	}
	limit := 10
	events, err := s.Audit.List(limit)
	if err != nil {
		return "读取审计日志失败：" + err.Error()
	}
	if len(events) == 0 {
		return "暂无审计日志。"
	}
	var lines []string
	for _, event := range events {
		lines = append(lines, fmt.Sprintf("%s %s %s %s %s", event.Time.Format("2006-01-02 15:04:05"), event.Mode, event.ProjectID, event.Action, event.Status))
	}
	return strings.Join(lines, "\n")
}

func (s *Session) diagnose(ctx context.Context, includeRisk bool) string {
	client := diagnostics.NewClient(3 * time.Second)
	report := client.Diagnose(ctx, s.Mode.String(), s.Projects, diagnostics.Options{IncludeRisk: includeRisk})
	action := "diagnose_health"
	if includeRisk {
		action = "diagnose_services"
	}
	if s.Audit != nil {
		_ = s.Audit.Append(audit.Event{
			Mode:   s.Mode.String(),
			Action: action,
			Status: report.OverallStatus,
		})
	}
	return diagnostics.FormatReport(report)
}

func (s *Session) risk(ctx context.Context) string {
	client := diagnostics.NewClient(3 * time.Second)
	report := client.Diagnose(ctx, s.Mode.String(), s.Projects, diagnostics.Options{IncludeRisk: true})
	status := report.OverallStatus
	if report.Risk != nil {
		status = report.Risk.Status
	}
	if s.Audit != nil {
		_ = s.Audit.Append(audit.Event{
			Mode:   s.Mode.String(),
			Action: "analyze_gm_risk",
			Status: status,
		})
	}
	return diagnostics.FormatRiskReport(report)
}

func (s *Session) findProject(id string) (projects.Manifest, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, project := range s.Projects {
		if strings.ToLower(project.ID) == id || strings.ToLower(project.Name) == id {
			return project, true
		}
	}
	return projects.Manifest{}, false
}
