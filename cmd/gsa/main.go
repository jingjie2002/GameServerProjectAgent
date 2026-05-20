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
	"github.com/jingjie2002/GameServerProjectAgent/internal/permissions"
	"github.com/jingjie2002/GameServerProjectAgent/internal/projects"
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
	workspace := projects.FindWorkspace(cwd)
	manifests, err := projects.LoadManifests(projectManifestPaths(workspace))
	if err != nil {
		exitErr(err)
	}
	store := audit.NewStore(auditPath(cwd))
	session := agent.NewSession(mode, manifests, store, os.Stdout)

	args := flag.Args()
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

func projectManifestPaths(workspace string) []string {
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
	return projects.FindDefaultManifestPaths(workspace)
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
