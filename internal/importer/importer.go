package importer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/jingjie2002/GameServerProjectAgent/internal/projects"
	"github.com/jingjie2002/GameServerProjectAgent/internal/registry"
)

type Runner interface {
	Run(ctx context.Context, dir string, name string, args ...string) (string, error)
}

type GitRunner struct{}

func (GitRunner) Run(ctx context.Context, dir string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

type Options struct {
	RepoURL    string
	Workspace  string
	Dest       string
	Home       string
	ConfigPath string
	Runner     Runner
	Now        func() time.Time
}

type Result struct {
	Name          string
	Dest          string
	Action        string
	ManifestPath  string
	ManifestValid bool
	Registered    bool
	AlreadyExists bool
	ReportPath    string
	Message       string
}

func Import(ctx context.Context, opts Options) (Result, error) {
	if strings.TrimSpace(opts.RepoURL) == "" {
		return Result{}, fmt.Errorf("repo url is required")
	}
	if opts.Runner == nil {
		opts.Runner = GitRunner{}
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Workspace == "" {
		opts.Workspace = "."
	}
	name := repoName(opts.RepoURL)
	if name == "" {
		return Result{}, fmt.Errorf("cannot infer repository name from %q", opts.RepoURL)
	}
	dest := opts.Dest
	if dest == "" {
		dest = filepath.Join(opts.Workspace, name)
	}
	result := Result{Name: name, Dest: filepath.Clean(dest)}

	if fileExists(dest) {
		if !fileExists(filepath.Join(dest, ".git")) {
			return Result{}, fmt.Errorf("destination exists but is not a git repository: %s", dest)
		}
		if _, err := opts.Runner.Run(ctx, dest, "git", "pull", "--ff-only"); err != nil {
			return Result{}, err
		}
		result.Action = "pulled"
	} else {
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return Result{}, err
		}
		if _, err := opts.Runner.Run(ctx, "", "git", "clone", opts.RepoURL, dest); err != nil {
			return Result{}, err
		}
		result.Action = "cloned"
	}

	manifestPath := filepath.Join(dest, "agent.yaml")
	if fileExists(manifestPath) {
		if _, err := projects.LoadManifest(manifestPath); err != nil {
			result.ManifestPath = manifestPath
			result.Message = "发现 agent.yaml，但解析失败，暂未注册。"
			report, reportErr := writeReport(opts, result, err.Error())
			result.ReportPath = report
			if reportErr != nil {
				return result, reportErr
			}
			return result, nil
		}
		result.ManifestPath = manifestPath
		result.ManifestValid = true
		registered, err := registry.RegisterManifest(registry.Options{
			Home:       opts.Home,
			Workspace:  opts.Workspace,
			ConfigPath: opts.ConfigPath,
		}, manifestPath)
		if err != nil {
			return result, err
		}
		result.Registered = registered.Registered
		result.AlreadyExists = registered.AlreadyExists
		if registered.Registered {
			result.Message = "发现合法 agent.yaml，已注册到 gsa 配置。"
		} else {
			result.Message = "发现合法 agent.yaml，配置中已存在，未重复注册。"
		}
		report, err := writeReport(opts, result, "")
		result.ReportPath = report
		return result, err
	}

	result.Message = "未发现 agent.yaml，已生成导入报告，等待后续扫描和配置生成。"
	report, err := writeReport(opts, result, "")
	result.ReportPath = report
	return result, err
}

func FormatResult(result Result) string {
	var lines []string
	lines = append(lines, "仓库导入完成")
	lines = append(lines, "name: "+result.Name)
	lines = append(lines, "action: "+result.Action)
	lines = append(lines, "path: "+result.Dest)
	if result.ManifestPath != "" {
		lines = append(lines, "agent_yaml: "+result.ManifestPath)
	} else {
		lines = append(lines, "agent_yaml: not found")
	}
	if result.Registered {
		lines = append(lines, "registered: yes")
	} else {
		lines = append(lines, "registered: no")
	}
	if result.ReportPath != "" {
		lines = append(lines, "report: "+result.ReportPath)
	}
	if result.Message != "" {
		lines = append(lines, result.Message)
	}
	return strings.Join(lines, "\n")
}

func writeReport(opts Options, result Result, parseError string) (string, error) {
	home := opts.Home
	if home == "" {
		home = opts.Workspace
	}
	dir := filepath.Join(home, ".gsa", "imports")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	reportPath := filepath.Join(dir, result.Name+"-import-report.md")
	var b strings.Builder
	fmt.Fprintf(&b, "# Import Report: %s\n\n", result.Name)
	fmt.Fprintf(&b, "- time: %s\n", opts.Now().Format(time.RFC3339))
	fmt.Fprintf(&b, "- repo: %s\n", opts.RepoURL)
	fmt.Fprintf(&b, "- action: %s\n", result.Action)
	fmt.Fprintf(&b, "- path: %s\n", result.Dest)
	if result.ManifestPath != "" {
		fmt.Fprintf(&b, "- agent_yaml: %s\n", result.ManifestPath)
	} else {
		b.WriteString("- agent_yaml: not found\n")
	}
	fmt.Fprintf(&b, "- registered: %t\n\n", result.Registered)
	if parseError != "" {
		fmt.Fprintf(&b, "## Parse Error\n\n```text\n%s\n```\n\n", parseError)
	}
	b.WriteString("## Next Steps\n\n")
	if result.ManifestPath == "" {
		b.WriteString("- Run repository scanning in the next phase to generate `agent.generated.yaml` and `deploy.generated.yaml`.\n")
		b.WriteString("- Review the generated files before registering or deploying the module.\n")
	} else if result.Registered {
		b.WriteString("- Run `gsa projects` to confirm the module is registered.\n")
		b.WriteString("- Run `gsa capabilities <project_id>` after the service is started.\n")
	} else {
		b.WriteString("- Review the manifest and import report before manual registration.\n")
	}
	return reportPath, os.WriteFile(reportPath, []byte(b.String()), 0o644)
}

func repoName(repoURL string) string {
	trimmed := strings.TrimSpace(repoURL)
	trimmed = strings.TrimSuffix(trimmed, "/")
	trimmed = strings.TrimSuffix(trimmed, "\\")
	trimmed = strings.TrimSuffix(trimmed, ".git")
	trimmed = strings.ReplaceAll(trimmed, "\\", "/")
	base := trimmed
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
	}
	if idx := strings.LastIndex(base, ":"); idx >= 0 {
		base = base[idx+1:]
	}
	return sanitizeName(base)
}

func sanitizeName(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-.")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
