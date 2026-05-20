package setup

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jingjie2002/GameServerProjectAgent/internal/projects"
)

type Config struct {
	Version   int
	Home      string
	Workspace string
	LLM       LLMConfig
	Projects  []ProjectConfig
	CreatedAt string
}

type LLMConfig struct {
	Provider    string
	BaseURL     string
	Model       string
	APIKeyEnv   string
	SecretsFile string
}

type ProjectConfig struct {
	ManifestPath string
}

type WizardOptions struct {
	Home       string
	Workspace  string
	ConfigPath string
	In         io.Reader
	Out        io.Writer
	Now        func() time.Time
}

func ResolveHome(cwd string, executable string) string {
	if env := os.Getenv("GSA_HOME"); strings.TrimSpace(env) != "" {
		return filepath.Clean(env)
	}
	for _, candidate := range homeCandidates(cwd, executable) {
		if markerExists(candidate) {
			return filepath.Clean(candidate)
		}
		parent := filepath.Dir(candidate)
		if parent != candidate && markerExists(parent) {
			return filepath.Clean(parent)
		}
	}
	return filepath.Clean(cwd)
}

func ConfigPath(home string) string {
	if env := os.Getenv("GSA_CONFIG"); strings.TrimSpace(env) != "" {
		return filepath.Clean(env)
	}
	return filepath.Join(home, ".gsa", "config.yaml")
}

func ConfigExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func LoadConfig(path string) (Config, error) {
	data, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer data.Close()

	cfg := Config{}
	scanner := bufio.NewScanner(data)
	var section string
	for scanner.Scan() {
		raw := stripComment(scanner.Text())
		if strings.TrimSpace(raw) == "" {
			continue
		}
		indent := leadingSpaces(raw)
		line := strings.TrimSpace(raw)
		key, value, ok := splitKV(line)
		if indent == 0 && ok {
			section = key
			switch key {
			case "version":
				n, _ := strconv.Atoi(value)
				cfg.Version = n
			case "home":
				cfg.Home = filepath.Clean(value)
			case "workspace":
				cfg.Workspace = filepath.Clean(value)
			case "created_at":
				cfg.CreatedAt = value
			}
			continue
		}
		switch section {
		case "llm":
			if indent >= 2 && ok {
				switch key {
				case "provider":
					cfg.LLM.Provider = value
				case "base_url":
					cfg.LLM.BaseURL = value
				case "model":
					cfg.LLM.Model = value
				case "api_key_env":
					cfg.LLM.APIKeyEnv = value
				case "secrets_file":
					cfg.LLM.SecretsFile = filepath.Clean(value)
				}
			}
		case "projects":
			if item, ok := listItem(line); ok {
				itemKey, itemValue, hasKV := splitKV(item)
				if hasKV && itemKey == "manifest" {
					cfg.Projects = append(cfg.Projects, ProjectConfig{ManifestPath: filepath.Clean(itemValue)})
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func WriteConfig(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	fmt.Fprintf(&b, "version: %d\n", cfg.Version)
	if cfg.Home != "" {
		fmt.Fprintf(&b, "home: %s\n", cfg.Home)
	}
	if cfg.Workspace != "" {
		fmt.Fprintf(&b, "workspace: %s\n", cfg.Workspace)
	}
	if cfg.CreatedAt != "" {
		fmt.Fprintf(&b, "created_at: %s\n", cfg.CreatedAt)
	}
	b.WriteString("llm:\n")
	fmt.Fprintf(&b, "  provider: %s\n", cfg.LLM.Provider)
	fmt.Fprintf(&b, "  base_url: %s\n", cfg.LLM.BaseURL)
	fmt.Fprintf(&b, "  model: %s\n", cfg.LLM.Model)
	fmt.Fprintf(&b, "  api_key_env: %s\n", cfg.LLM.APIKeyEnv)
	if cfg.LLM.SecretsFile != "" {
		fmt.Fprintf(&b, "  secrets_file: %s\n", cfg.LLM.SecretsFile)
	}
	b.WriteString("projects:\n")
	for _, project := range cfg.Projects {
		fmt.Fprintf(&b, "  - manifest: %s\n", project.ManifestPath)
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

func (c Config) ProjectManifestPaths() []string {
	paths := make([]string, 0, len(c.Projects))
	for _, project := range c.Projects {
		if strings.TrimSpace(project.ManifestPath) != "" {
			paths = append(paths, project.ManifestPath)
		}
	}
	return paths
}

func RunWizard(opts WizardOptions) (Config, error) {
	if opts.In == nil {
		opts.In = os.Stdin
	}
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Home == "" {
		cwd, _ := os.Getwd()
		opts.Home = filepath.Clean(cwd)
	}
	if opts.ConfigPath == "" {
		opts.ConfigPath = ConfigPath(opts.Home)
	}
	if opts.Workspace == "" {
		opts.Workspace = projects.FindWorkspace(opts.Home)
	}

	reader := bufio.NewScanner(opts.In)
	fmt.Fprintln(opts.Out, "欢迎使用 GameServerProjectAgent 初始化向导")
	fmt.Fprintln(opts.Out, "这个向导会生成本机配置，不会把密钥提交到 Git。")
	fmt.Fprintln(opts.Out)

	workspace := ask(reader, opts.Out, "请选择服务端工作区目录", opts.Workspace)
	providerChoice := askChoice(reader, opts.Out, "请选择模型供应商（可暂不配置，后续再接入）", []choice{
		{Label: "暂不配置模型", Value: "none"},
		{Label: "DeepSeek", Value: "deepseek"},
		{Label: "OpenAI", Value: "openai"},
		{Label: "OpenAI-compatible", Value: "openai-compatible"},
	}, "1")

	llmCfg := defaultLLM(providerChoice)
	if providerChoice != "none" {
		llmCfg.BaseURL = ask(reader, opts.Out, "模型 API Base URL", llmCfg.BaseURL)
		llmCfg.Model = ask(reader, opts.Out, "模型名称", llmCfg.Model)
		llmCfg.APIKeyEnv = ask(reader, opts.Out, "API Key 环境变量名", llmCfg.APIKeyEnv)
		apiKey := ask(reader, opts.Out, "API Key（可回车跳过，稍后用环境变量配置）", "")
		if strings.TrimSpace(apiKey) != "" {
			secretsPath := filepath.Join(opts.Home, ".gsa", "secrets.env")
			if err := writeSecret(secretsPath, llmCfg.APIKeyEnv, apiKey); err != nil {
				return Config{}, err
			}
			llmCfg.SecretsFile = filepath.Join(".gsa", "secrets.env")
		}
	}

	manifestPaths := projects.FindDefaultManifestPaths(workspace)
	cfg := Config{
		Version:   1,
		Home:      filepath.Clean(opts.Home),
		Workspace: filepath.Clean(workspace),
		LLM:       llmCfg,
		CreatedAt: opts.Now().Format(time.RFC3339),
	}
	for _, path := range manifestPaths {
		if fileExists(path) {
			cfg.Projects = append(cfg.Projects, ProjectConfig{ManifestPath: filepath.Clean(path)})
		}
	}
	if err := WriteConfig(opts.ConfigPath, cfg); err != nil {
		return Config{}, err
	}

	fmt.Fprintln(opts.Out)
	fmt.Fprintln(opts.Out, "初始化完成。")
	fmt.Fprintln(opts.Out, "配置文件："+opts.ConfigPath)
	if llmCfg.SecretsFile != "" {
		fmt.Fprintln(opts.Out, "密钥文件："+filepath.Join(opts.Home, llmCfg.SecretsFile))
	}
	fmt.Fprintf(opts.Out, "已注册模块：%d 个\n", len(cfg.Projects))
	fmt.Fprintln(opts.Out, "下一步可以运行：")
	fmt.Fprintln(opts.Out, "  gsa projects")
	fmt.Fprintln(opts.Out, "  gsa health")
	fmt.Fprintln(opts.Out, "  gsa diagnose")
	return cfg, nil
}

type choice struct {
	Label string
	Value string
}

func ask(reader *bufio.Scanner, out io.Writer, prompt string, def string) string {
	if def != "" {
		fmt.Fprintf(out, "%s [%s]: ", prompt, def)
	} else {
		fmt.Fprintf(out, "%s: ", prompt)
	}
	if !reader.Scan() {
		return def
	}
	value := strings.TrimSpace(reader.Text())
	if value == "" {
		return def
	}
	return value
}

func askChoice(reader *bufio.Scanner, out io.Writer, prompt string, choices []choice, def string) string {
	fmt.Fprintln(out, prompt+"：")
	for i, choice := range choices {
		fmt.Fprintf(out, "  %d. %s\n", i+1, choice.Label)
	}
	selected := ask(reader, out, "请输入序号", def)
	idx, err := strconv.Atoi(selected)
	if err != nil || idx < 1 || idx > len(choices) {
		idx, _ = strconv.Atoi(def)
	}
	return choices[idx-1].Value
}

func defaultLLM(provider string) LLMConfig {
	switch provider {
	case "deepseek":
		return LLMConfig{
			Provider:  "deepseek",
			BaseURL:   "https://api.deepseek.com",
			Model:     "deepseek-chat",
			APIKeyEnv: "GSA_LLM_API_KEY",
		}
	case "openai":
		return LLMConfig{
			Provider:  "openai",
			BaseURL:   "https://api.openai.com/v1",
			Model:     "gpt-4.1-mini",
			APIKeyEnv: "GSA_LLM_API_KEY",
		}
	case "openai-compatible":
		return LLMConfig{
			Provider:  "openai-compatible",
			BaseURL:   "http://127.0.0.1:11434/v1",
			Model:     "local-model",
			APIKeyEnv: "GSA_LLM_API_KEY",
		}
	default:
		return LLMConfig{Provider: "none", APIKeyEnv: "GSA_LLM_API_KEY"}
	}
}

func writeSecret(path string, envName string, value string) error {
	if strings.TrimSpace(envName) == "" {
		return fmt.Errorf("api key env name is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	safe := strings.NewReplacer("\r", "", "\n", "").Replace(value)
	return os.WriteFile(path, []byte(envName+"="+safe+"\n"), 0o600)
}

func homeCandidates(cwd string, executable string) []string {
	var candidates []string
	if executable != "" {
		candidates = append(candidates, filepath.Dir(executable))
	}
	if cwd != "" {
		candidates = append(candidates, cwd)
	}
	return candidates
}

func markerExists(path string) bool {
	return fileExists(filepath.Join(path, "configs", "projects.example.yaml"))
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func splitKV(line string) (string, string, bool) {
	idx := strings.Index(line, ":")
	if idx < 0 {
		return "", "", false
	}
	key := strings.TrimSpace(line[:idx])
	value := strings.TrimSpace(line[idx+1:])
	value = strings.Trim(value, `"'`)
	return key, value, true
}

func listItem(line string) (string, bool) {
	if !strings.HasPrefix(line, "- ") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(line, "- ")), true
}

func stripComment(line string) string {
	if idx := strings.Index(line, "#"); idx >= 0 {
		return line[:idx]
	}
	return line
}

func leadingSpaces(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}
