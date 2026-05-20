package onboard

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jingjie2002/GameServerProjectAgent/internal/generated"
	"github.com/jingjie2002/GameServerProjectAgent/internal/importer"
	"github.com/jingjie2002/GameServerProjectAgent/internal/scanner"
)

type Options struct {
	RepoURL     string
	Dest        string
	Home        string
	Workspace   string
	ConfigPath  string
	AutoApprove bool
	In          io.Reader
	Out         io.Writer
	Runner      importer.Runner
	Now         func() time.Time
}

type Result struct {
	RepoURL             string
	Dest                string
	ImportResult        importer.Result
	ScanResult          scanner.Result
	PreviewResult       generated.Result
	RegisterResult      generated.Result
	Scanned             bool
	Registered          bool
	SkippedScan         bool
	SkippedRegistration bool
}

func Run(ctx context.Context, opts Options) (Result, error) {
	if opts.In == nil {
		opts.In = os.Stdin
	}
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	input := bufio.NewScanner(opts.In)

	fmt.Fprintln(opts.Out, "GameServerProjectAgent 模块导入向导")
	fmt.Fprintln(opts.Out, "本向导只会 clone/pull、扫描并注册本机配置，不会修改源码、不会部署、不会执行数据库迁移。")
	fmt.Fprintln(opts.Out)

	repoURL := strings.TrimSpace(opts.RepoURL)
	if repoURL == "" {
		repoURL = ask(input, opts.Out, "请输入 Git 仓库 URL 或本地 Git 仓库路径", "")
	}
	if repoURL == "" {
		return Result{}, fmt.Errorf("repo url is required")
	}
	dest := strings.TrimSpace(opts.Dest)
	if dest == "" && !opts.AutoApprove {
		dest = ask(input, opts.Out, "目标目录（回车使用工作区默认目录）", "")
	}

	result := Result{RepoURL: repoURL, Dest: dest}
	fmt.Fprintln(opts.Out)
	fmt.Fprintln(opts.Out, "步骤 1/3：导入仓库")
	importResult, err := importer.Import(ctx, importer.Options{
		RepoURL:    repoURL,
		Workspace:  opts.Workspace,
		Dest:       dest,
		Home:       opts.Home,
		ConfigPath: opts.ConfigPath,
		Runner:     opts.Runner,
		Now:        opts.Now,
	})
	if err != nil {
		return result, err
	}
	result.ImportResult = importResult
	if result.Dest == "" {
		result.Dest = importResult.Dest
	}
	fmt.Fprintln(opts.Out, importer.FormatResult(importResult))

	if importResult.ManifestValid && (importResult.Registered || importResult.AlreadyExists) {
		fmt.Fprintln(opts.Out)
		fmt.Fprintln(opts.Out, "导入向导完成：仓库已包含合法 agent.yaml，已可被 gsa 管理。")
		result.Registered = importResult.Registered || importResult.AlreadyExists
		return result, nil
	}

	if !opts.AutoApprove && !askYesNo(input, opts.Out, "未发现可直接注册的 agent.yaml，是否现在扫描并生成 generated 配置？", true) {
		result.SkippedScan = true
		fmt.Fprintln(opts.Out, "已跳过扫描。后续可手动运行：gsa scan "+importResult.Dest)
		return result, nil
	}

	fmt.Fprintln(opts.Out)
	fmt.Fprintln(opts.Out, "步骤 2/3：扫描仓库并生成配置草稿")
	scanResult, err := scanner.Scan(scanner.Options{Path: importResult.Dest, Home: opts.Home, Now: opts.Now})
	if err != nil {
		return result, err
	}
	result.ScanResult = scanResult
	result.Scanned = true
	fmt.Fprintln(opts.Out, scanner.FormatResult(scanResult))

	fmt.Fprintln(opts.Out)
	fmt.Fprintln(opts.Out, "步骤 3/3：预览 generated 配置")
	preview, err := generated.Register(generated.Options{
		Path:       importResult.Dest,
		Home:       opts.Home,
		Workspace:  opts.Workspace,
		ConfigPath: opts.ConfigPath,
	})
	if err != nil {
		return result, err
	}
	result.PreviewResult = preview
	fmt.Fprintln(opts.Out, generated.FormatResult(preview))

	shouldRegister := opts.AutoApprove
	if !opts.AutoApprove {
		shouldRegister = askYesNo(input, opts.Out, "确认把这个 generated 配置注册到 gsa 吗？", false)
	}
	if !shouldRegister {
		result.SkippedRegistration = true
		fmt.Fprintln(opts.Out, "已跳过注册。确认后可手动运行：gsa register-generated "+scanResult.AgentGeneratedPath+" --confirm")
		return result, nil
	}

	registerResult, err := generated.Register(generated.Options{
		Path:       importResult.Dest,
		Home:       opts.Home,
		Workspace:  opts.Workspace,
		ConfigPath: opts.ConfigPath,
		Confirm:    true,
	})
	if err != nil {
		return result, err
	}
	result.RegisterResult = registerResult
	result.Registered = registerResult.Registered || registerResult.AlreadyExists
	fmt.Fprintln(opts.Out, generated.FormatResult(registerResult))
	fmt.Fprintln(opts.Out)
	fmt.Fprintln(opts.Out, "导入向导完成。下一步可运行：gsa projects")
	return result, nil
}

func ask(input *bufio.Scanner, out io.Writer, prompt string, def string) string {
	if def != "" {
		fmt.Fprintf(out, "%s [%s]: ", prompt, def)
	} else {
		fmt.Fprintf(out, "%s: ", prompt)
	}
	if !input.Scan() {
		return def
	}
	value := strings.TrimSpace(input.Text())
	if value == "" {
		return def
	}
	return value
}

func askYesNo(input *bufio.Scanner, out io.Writer, prompt string, def bool) bool {
	suffix := " [y/N]: "
	if def {
		suffix = " [Y/n]: "
	}
	fmt.Fprint(out, prompt+suffix)
	if !input.Scan() {
		return def
	}
	value := strings.ToLower(strings.TrimSpace(input.Text()))
	if value == "" {
		return def
	}
	switch value {
	case "y", "yes", "是", "确认", "true", "1":
		return true
	case "n", "no", "否", "取消", "false", "0":
		return false
	default:
		return def
	}
}
