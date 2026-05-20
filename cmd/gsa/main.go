package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jingjie2002/GameServerProjectAgent/internal/agent"
	"github.com/jingjie2002/GameServerProjectAgent/internal/audit"
	"github.com/jingjie2002/GameServerProjectAgent/internal/generated"
	"github.com/jingjie2002/GameServerProjectAgent/internal/importer"
	"github.com/jingjie2002/GameServerProjectAgent/internal/onboard"
	"github.com/jingjie2002/GameServerProjectAgent/internal/permissions"
	"github.com/jingjie2002/GameServerProjectAgent/internal/projects"
	"github.com/jingjie2002/GameServerProjectAgent/internal/scanner"
	"github.com/jingjie2002/GameServerProjectAgent/internal/setup"
)

func main() {
	modeFlag := flag.String("mode", "默认权限", "默认权限、自动审查或完全访问权限")
	flag.Parse()

	mode, err := permissions.ParseMode(*modeFlag)
	if err != nil {
		exitErr(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		exitErr(err)
	}
	executable, _ := os.Executable()
	home := setup.ResolveHome(cwd, executable)
	configPath := setup.ConfigPath(home)
	args := flag.Args()
	if len(args) > 0 && args[0] == "setup" {
		if _, err := setup.RunWizard(setup.WizardOptions{
			Home:       home,
			Workspace:  projects.FindWorkspace(home),
			ConfigPath: configPath,
			In:         os.Stdin,
			Out:        os.Stdout,
		}); err != nil {
			exitErr(err)
		}
		return
	}
	if len(args) > 0 && args[0] == "help" {
		fmt.Println(agent.HelpText())
		return
	}
	if len(args) > 0 && args[0] == "onboard" && !setup.ConfigExists(configPath) && !hasArg(args[1:], "--yes", "-y") {
		fmt.Println("未检测到本机配置，先进入初始化向导。")
		if _, err := setup.RunWizard(setup.WizardOptions{
			Home:       home,
			Workspace:  projects.FindWorkspace(home),
			ConfigPath: configPath,
			In:         os.Stdin,
			Out:        os.Stdout,
		}); err != nil {
			exitErr(err)
		}
	}
	if len(args) == 0 && !setup.ConfigExists(configPath) {
		fmt.Println("未检测到本机配置，先进入初始化向导。")
		if _, err := setup.RunWizard(setup.WizardOptions{
			Home:       home,
			Workspace:  projects.FindWorkspace(home),
			ConfigPath: configPath,
			In:         os.Stdin,
			Out:        os.Stdout,
		}); err != nil {
			exitErr(err)
		}
	}
	cfg, cfgErr := setup.LoadConfig(configPath)
	if cfgErr != nil && !os.IsNotExist(cfgErr) {
		exitErr(cfgErr)
	}
	workspace := projects.FindWorkspace(home)
	if cfg.Workspace != "" {
		workspace = cfg.Workspace
	}
	if len(args) > 0 && args[0] == "import" {
		output, err := runImportCommand(context.Background(), home, workspace, configPath, args[1:])
		if err != nil {
			exitErr(err)
		}
		if output != "" {
			fmt.Println(output)
		}
		return
	}
	if len(args) > 0 && args[0] == "scan" {
		output, err := runScanCommand(args[1:], home)
		if err != nil {
			exitErr(err)
		}
		if output != "" {
			fmt.Println(output)
		}
		return
	}
	if len(args) > 0 && args[0] == "register-generated" {
		output, err := runRegisterGeneratedCommand(args[1:], home, workspace, configPath)
		if err != nil {
			exitErr(err)
		}
		if output != "" {
			fmt.Println(output)
		}
		return
	}
	if len(args) > 0 && args[0] == "onboard" {
		if err := runOnboardCommand(context.Background(), home, workspace, configPath, args[1:]); err != nil {
			exitErr(err)
		}
		return
	}
	manifests, err := projects.LoadManifests(projectManifestPaths(workspace, cfg))
	if err != nil {
		exitErr(err)
	}
	store := audit.NewStore(auditPath(home))
	session := agent.NewSession(mode, manifests, store, os.Stdout)

	if len(args) == 0 {
		runInteractive(session)
		return
	}
	output := runOneShot(context.Background(), session, args)
	if output != "" {
		fmt.Println(output)
	}
}

func runInteractive(session *agent.Session) {
	fmt.Println("GameServerAgent V2")
	fmt.Println("输入 /帮助 查看命令，输入 /退出 结束。")
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("GameServerAgent > ")
		if !scanner.Scan() {
			break
		}
		output := session.Handle(context.Background(), scanner.Text())
		if output != "" {
			fmt.Println(output)
		}
		if strings.TrimSpace(output) == "bye" {
			break
		}
	}
}

func runOneShot(ctx context.Context, session *agent.Session, args []string) string {
	switch args[0] {
	case "import":
		return "usage: gsa import <repo-url> [--dest path]"
	case "scan":
		return "usage: gsa scan <path>"
	case "register-generated":
		return "usage: gsa register-generated <path> [--confirm]"
	case "onboard":
		return "usage: gsa onboard [repo-url] [--dest path] [--yes]"
	case "projects":
		return session.Handle(ctx, "/项目")
	case "capabilities":
		return session.Handle(ctx, "/能力 "+strings.Join(args[1:], " "))
	case "mode":
		return session.Handle(ctx, "/模式 "+strings.Join(args[1:], " "))
	case "audit":
		return session.Handle(ctx, "/审计")
	case "check", "run-checks":
		return session.Handle(ctx, "/检查 "+strings.Join(args[1:], " "))
	case "health":
		return session.Handle(ctx, "/健康")
	case "diagnose", "status":
		return session.Handle(ctx, "/诊断")
	case "risk":
		return session.Handle(ctx, "/风险")
	case "gm":
		return session.Handle(ctx, "/gm "+strings.Join(args[1:], " "))
	case "ask":
		return session.Handle(ctx, strings.Join(args[1:], " "))
	case "help":
		return session.Handle(ctx, "/帮助")
	default:
		return session.Handle(ctx, strings.Join(args, " "))
	}
}

func runScanCommand(args []string, home string) (string, error) {
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		return "", fmt.Errorf("usage: gsa scan <path>")
	}
	result, err := scanner.Scan(scanner.Options{Path: args[0], Home: home})
	if err != nil {
		return "", err
	}
	return scanner.FormatResult(result), nil
}

func runRegisterGeneratedCommand(args []string, home string, workspace string, configPath string) (string, error) {
	path, confirm, err := parseRegisterGeneratedArgs(args)
	if err != nil {
		return "", err
	}
	result, err := generated.Register(generated.Options{
		Path:       path,
		Home:       home,
		Workspace:  workspace,
		ConfigPath: configPath,
		Confirm:    confirm,
	})
	if err != nil {
		return "", err
	}
	return generated.FormatResult(result), nil
}

func runOnboardCommand(ctx context.Context, home string, workspace string, configPath string, args []string) error {
	repoURL, dest, autoApprove, err := parseOnboardArgs(args)
	if err != nil {
		return err
	}
	_, err = onboard.Run(ctx, onboard.Options{
		RepoURL:     repoURL,
		Dest:        dest,
		Home:        home,
		Workspace:   workspace,
		ConfigPath:  configPath,
		AutoApprove: autoApprove,
		In:          os.Stdin,
		Out:         os.Stdout,
	})
	return err
}

func runImportCommand(ctx context.Context, home string, workspace string, configPath string, args []string) (string, error) {
	repoURL, dest, err := parseImportArgs(args)
	if err != nil {
		return "", err
	}
	result, err := importer.Import(ctx, importer.Options{
		RepoURL:    repoURL,
		Workspace:  workspace,
		Dest:       dest,
		Home:       home,
		ConfigPath: configPath,
	})
	if err != nil {
		return "", err
	}
	return importer.FormatResult(result), nil
}

func parseImportArgs(args []string) (string, string, error) {
	var repoURL string
	var dest string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--dest":
			if i+1 >= len(args) {
				return "", "", fmt.Errorf("usage: gsa import <repo-url> [--dest path]")
			}
			dest = args[i+1]
			i++
		default:
			if strings.HasPrefix(arg, "--dest=") {
				dest = strings.TrimPrefix(arg, "--dest=")
				continue
			}
			if strings.HasPrefix(arg, "-") {
				return "", "", fmt.Errorf("unknown import option: %s", arg)
			}
			if repoURL == "" {
				repoURL = arg
				continue
			}
			return "", "", fmt.Errorf("usage: gsa import <repo-url> [--dest path]")
		}
	}
	if repoURL == "" {
		return "", "", fmt.Errorf("usage: gsa import <repo-url> [--dest path]")
	}
	return repoURL, dest, nil
}

func parseRegisterGeneratedArgs(args []string) (string, bool, error) {
	var path string
	var confirm bool
	for _, arg := range args {
		switch arg {
		case "--confirm":
			confirm = true
		default:
			if strings.HasPrefix(arg, "-") {
				return "", false, fmt.Errorf("unknown register-generated option: %s", arg)
			}
			if path != "" {
				return "", false, fmt.Errorf("usage: gsa register-generated <path> [--confirm]")
			}
			path = arg
		}
	}
	if path == "" {
		return "", false, fmt.Errorf("usage: gsa register-generated <path> [--confirm]")
	}
	return path, confirm, nil
}

func parseOnboardArgs(args []string) (string, string, bool, error) {
	var repoURL string
	var dest string
	var autoApprove bool
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--yes", "-y":
			autoApprove = true
		case "--dest":
			if i+1 >= len(args) {
				return "", "", false, fmt.Errorf("usage: gsa onboard [repo-url] [--dest path] [--yes]")
			}
			dest = args[i+1]
			i++
		default:
			if strings.HasPrefix(arg, "--dest=") {
				dest = strings.TrimPrefix(arg, "--dest=")
				continue
			}
			if strings.HasPrefix(arg, "-") {
				return "", "", false, fmt.Errorf("unknown onboard option: %s", arg)
			}
			if repoURL != "" {
				return "", "", false, fmt.Errorf("usage: gsa onboard [repo-url] [--dest path] [--yes]")
			}
			repoURL = arg
		}
	}
	return repoURL, dest, autoApprove, nil
}

func projectManifestPaths(workspace string, cfg setup.Config) []string {
	if raw := os.Getenv("GSA_PROJECT_MANIFESTS"); raw != "" {
		parts := strings.Split(raw, string(os.PathListSeparator))
		paths := make([]string, 0, len(parts))
		for _, part := range parts {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				paths = append(paths, trimmed)
			}
		}
		return paths
	}
	if paths := cfg.ProjectManifestPaths(); len(paths) > 0 {
		return paths
	}
	return projects.FindDefaultManifestPaths(workspace)
}

func hasArg(args []string, names ...string) bool {
	for _, arg := range args {
		for _, name := range names {
			if arg == name {
				return true
			}
		}
	}
	return false
}

func auditPath(cwd string) string {
	if raw := os.Getenv("GSA_AUDIT_LOG"); raw != "" {
		return raw
	}
	return filepath.Join(cwd, ".gsa", "audit.log")
}

func exitErr(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
