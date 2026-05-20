package scanner

import (
	"bufio"
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type Options struct {
	Path string
	Home string
	Now  func() time.Time
}

type Result struct {
	Name                string
	Path                string
	Language            string
	Confidence          string
	ModuleName          string
	Entrypoints         []string
	Ports               []int
	Dependencies        []string
	HealthPaths         []string
	EnvVars             []string
	StartupHints        []string
	Docs                []string
	HasDockerfile       bool
	HasDockerCompose    bool
	HasEnvExample       bool
	AgentGeneratedPath  string
	DeployGeneratedPath string
	ReportPath          string
}

func Scan(opts Options) (Result, error) {
	if strings.TrimSpace(opts.Path) == "" {
		return Result{}, fmt.Errorf("scan path is required")
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	root, err := filepath.Abs(filepath.Clean(opts.Path))
	if err != nil {
		return Result{}, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return Result{}, err
	}
	if !info.IsDir() {
		return Result{}, fmt.Errorf("scan path is not a directory: %s", root)
	}

	result := Result{
		Name:       sanitizeName(filepath.Base(root)),
		Path:       root,
		Language:   "unknown",
		Confidence: "low",
	}
	if result.Name == "" {
		result.Name = "imported-service"
	}
	if fileExists(filepath.Join(root, "go.mod")) {
		result.Language = "go"
		result.ModuleName = readGoModule(filepath.Join(root, "go.mod"))
	}
	result.HasDockerfile = fileExists(filepath.Join(root, "Dockerfile"))
	result.HasDockerCompose = fileExists(filepath.Join(root, "docker-compose.yml")) || fileExists(filepath.Join(root, "docker-compose.yaml")) || fileExists(filepath.Join(root, "compose.yml")) || fileExists(filepath.Join(root, "compose.yaml"))
	result.HasEnvExample = fileExists(filepath.Join(root, ".env.example"))
	result.Entrypoints = findGoEntrypoints(root)
	result.EnvVars = readEnvExample(filepath.Join(root, ".env.example"))
	result.Docs = findDocs(root)
	result.StartupHints = readStartupHints(root, result.Docs)

	ports := map[int]bool{}
	deps := map[string]bool{}
	health := map[string]bool{}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path == root {
			return nil
		}
		if d.IsDir() {
			if shouldSkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !shouldInspectFile(path) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || len(data) > 512*1024 {
			return nil
		}
		text := string(data)
		for _, port := range extractPorts(text) {
			ports[port] = true
		}
		for _, dep := range extractDependencies(text) {
			deps[dep] = true
		}
		for _, hp := range extractHealthPaths(text) {
			health[hp] = true
		}
		return nil
	})
	result.Ports = sortedInts(ports)
	result.Dependencies = sortedStrings(deps)
	result.HealthPaths = sortedStrings(health)
	result.Confidence = confidence(result)

	if err := writeGeneratedFiles(&result, opts); err != nil {
		return result, err
	}
	return result, nil
}

func FormatResult(result Result) string {
	var lines []string
	lines = append(lines, "仓库扫描完成")
	lines = append(lines, "name: "+result.Name)
	lines = append(lines, "path: "+result.Path)
	lines = append(lines, "language: "+result.Language)
	lines = append(lines, "confidence: "+result.Confidence)
	if result.ModuleName != "" {
		lines = append(lines, "module: "+result.ModuleName)
	}
	if len(result.Entrypoints) > 0 {
		lines = append(lines, "entrypoints: "+strings.Join(result.Entrypoints, ", "))
	}
	if len(result.Ports) > 0 {
		lines = append(lines, "ports: "+joinInts(result.Ports))
	}
	if len(result.Dependencies) > 0 {
		lines = append(lines, "dependencies: "+strings.Join(result.Dependencies, ", "))
	}
	if len(result.HealthPaths) > 0 {
		lines = append(lines, "health_paths: "+strings.Join(result.HealthPaths, ", "))
	}
	lines = append(lines, "agent_generated: "+result.AgentGeneratedPath)
	lines = append(lines, "deploy_generated: "+result.DeployGeneratedPath)
	lines = append(lines, "report: "+result.ReportPath)
	lines = append(lines, "请先人工检查 generated 文件；当前阶段不会自动改源码或部署。")
	return strings.Join(lines, "\n")
}

func writeGeneratedFiles(result *Result, opts Options) error {
	agentPath := filepath.Join(result.Path, "agent.generated.yaml")
	deployPath := filepath.Join(result.Path, "deploy.generated.yaml")
	reportPath := filepath.Join(result.Path, "gsa-scan-report.md")
	if opts.Home != "" {
		reportDir := filepath.Join(opts.Home, ".gsa", "scans")
		if err := os.MkdirAll(reportDir, 0o755); err != nil {
			return err
		}
		reportPath = filepath.Join(reportDir, result.Name+"-scan-report.md")
	}
	result.AgentGeneratedPath = agentPath
	result.DeployGeneratedPath = deployPath
	result.ReportPath = reportPath
	if err := os.WriteFile(agentPath, []byte(renderAgentYAML(*result)), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(deployPath, []byte(renderDeployYAML(*result)), 0o644); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(reportPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(reportPath, []byte(renderReport(*result, opts.Now())), 0o644); err != nil {
		return err
	}
	return nil
}

func renderAgentYAML(result Result) string {
	port := firstServicePort(result.Ports, 8080)
	healthPath := firstHealthPath(result.HealthPaths)
	var b strings.Builder
	fmt.Fprintf(&b, "id: %s\n", strings.ToLower(result.Name))
	fmt.Fprintf(&b, "name: %s\n", result.Name)
	b.WriteString("description: Generated by gsa scan. Review before renaming to agent.yaml.\n")
	b.WriteString("type: imported-service\n")
	fmt.Fprintf(&b, "root: %s\n", filepath.ToSlash(result.Path))
	if result.Language != "" {
		fmt.Fprintf(&b, "language: %s\n", result.Language)
	}
	b.WriteString("health:\n")
	fmt.Fprintf(&b, "  url: http://127.0.0.1:%d%s\n", port, healthPath)
	b.WriteString("metrics:\n")
	fmt.Fprintf(&b, "  url: http://127.0.0.1:%d/metrics\n", port)
	b.WriteString("capabilities_endpoint:\n")
	fmt.Fprintf(&b, "  url: http://127.0.0.1:%d/api/agent/capabilities\n", port)
	b.WriteString("commands:\n")
	b.WriteString("  test:\n")
	b.WriteString("    command: go test ./...\n")
	b.WriteString("    mode: auto-review\n")
	b.WriteString("  vet:\n")
	b.WriteString("    command: go vet ./...\n")
	b.WriteString("    mode: auto-review\n")
	b.WriteString("docs:\n")
	if len(result.Docs) == 0 {
		b.WriteString("  - README.md\n")
	} else {
		for _, doc := range result.Docs {
			fmt.Fprintf(&b, "  - %s\n", filepath.ToSlash(doc))
		}
	}
	b.WriteString("capabilities:\n")
	b.WriteString("  - health\n")
	if contains(result.HealthPaths, "/metrics") {
		b.WriteString("  - metrics\n")
	}
	b.WriteString("forbidden:\n")
	b.WriteString("  - auto_deploy_without_confirmation\n")
	b.WriteString("  - direct_database_write_without_confirmation\n")
	return b.String()
}

func renderDeployYAML(result Result) string {
	port := firstServicePort(result.Ports, 8080)
	healthPath := firstHealthPath(result.HealthPaths)
	runCommand := inferRunCommand(result)
	var b strings.Builder
	fmt.Fprintf(&b, "runtime: %s\n", firstString([]string{result.Language}, "unknown"))
	fmt.Fprintf(&b, "confidence: %s\n", result.Confidence)
	b.WriteString("build:\n")
	if result.Language == "go" {
		b.WriteString("  command: go build ./...\n")
	} else {
		b.WriteString("  command: TODO\n")
	}
	b.WriteString("run:\n")
	fmt.Fprintf(&b, "  command: %s\n", runCommand)
	b.WriteString("ports:\n")
	fmt.Fprintf(&b, "  - name: http\n    value: %d\n", port)
	b.WriteString("dependencies:\n")
	if len(result.Dependencies) == 0 {
		b.WriteString("  - none-detected\n")
	} else {
		for _, dep := range result.Dependencies {
			fmt.Fprintf(&b, "  - %s\n", dep)
		}
	}
	b.WriteString("env:\n")
	if len(result.EnvVars) == 0 {
		b.WriteString("  - name: TODO\n    required: false\n")
	} else {
		for _, env := range result.EnvVars {
			fmt.Fprintf(&b, "  - name: %s\n    required: false\n", env)
		}
	}
	b.WriteString("health_check:\n")
	fmt.Fprintf(&b, "  path: %s\n", healthPath)
	b.WriteString("notes:\n")
	b.WriteString("  - Generated by gsa scan. Review before deployment.\n")
	if result.HasDockerfile {
		b.WriteString("  - Dockerfile detected.\n")
	}
	if result.HasDockerCompose {
		b.WriteString("  - Docker Compose file detected.\n")
	}
	return b.String()
}

func renderReport(result Result, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Scan Report: %s\n\n", result.Name)
	fmt.Fprintf(&b, "- time: %s\n", now.Format(time.RFC3339))
	fmt.Fprintf(&b, "- path: %s\n", result.Path)
	fmt.Fprintf(&b, "- language: %s\n", result.Language)
	fmt.Fprintf(&b, "- confidence: %s\n", result.Confidence)
	if result.ModuleName != "" {
		fmt.Fprintf(&b, "- module: %s\n", result.ModuleName)
	}
	fmt.Fprintf(&b, "- agent_generated: %s\n", result.AgentGeneratedPath)
	fmt.Fprintf(&b, "- deploy_generated: %s\n", result.DeployGeneratedPath)
	b.WriteString("\n## Findings\n\n")
	writeList(&b, "Entrypoints", result.Entrypoints)
	writeList(&b, "Ports", intStrings(result.Ports))
	writeList(&b, "Dependencies", result.Dependencies)
	writeList(&b, "Health Paths", result.HealthPaths)
	writeList(&b, "Environment Variables", result.EnvVars)
	writeList(&b, "Startup Hints", result.StartupHints)
	b.WriteString("\n## Safety\n\n")
	b.WriteString("- This scan only generated draft files.\n")
	b.WriteString("- It did not modify source code.\n")
	b.WriteString("- It did not start services.\n")
	b.WriteString("- Review generated YAML files before registration or deployment.\n")
	return b.String()
}

func writeList(b *strings.Builder, title string, values []string) {
	fmt.Fprintf(b, "### %s\n\n", title)
	if len(values) == 0 {
		b.WriteString("- none detected\n\n")
		return
	}
	for _, value := range values {
		fmt.Fprintf(b, "- %s\n", value)
	}
	b.WriteString("\n")
}

func readGoModule(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[0] == "module" {
			return fields[1]
		}
	}
	return ""
}

func findGoEntrypoints(root string) []string {
	var entries []string
	if fileExists(filepath.Join(root, "main.go")) {
		entries = append(entries, ".")
	}
	cmdRoot := filepath.Join(root, "cmd")
	_ = filepath.WalkDir(cmdRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != cmdRoot && shouldSkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "main.go" {
			return nil
		}
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err == nil {
			entries = append(entries, filepath.ToSlash(rel))
		}
		return nil
	})
	return sortedUnique(entries)
}

func readEnvExample(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var values []string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx <= 0 {
			continue
		}
		name := strings.TrimSpace(line[:idx])
		if isEnvName(name) {
			values = append(values, name)
		}
	}
	return sortedUnique(values)
}

func findDocs(root string) []string {
	var docs []string
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		lower := strings.ToLower(name)
		if strings.HasPrefix(lower, "readme") && (strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".txt") || lower == "readme") {
			docs = append(docs, name)
		}
	}
	return sortedUnique(docs)
}

func readStartupHints(root string, docs []string) []string {
	var hints []string
	for _, doc := range docs {
		data, err := os.ReadFile(filepath.Join(root, doc))
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(bytes.NewReader(data))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			lower := strings.ToLower(line)
			if strings.Contains(lower, "go run") ||
				strings.Contains(lower, "go build") ||
				strings.Contains(lower, "docker compose") ||
				strings.Contains(lower, "docker-compose") ||
				strings.Contains(lower, "npm start") ||
				strings.Contains(lower, "python ") ||
				strings.Contains(lower, "java -jar") ||
				strings.Contains(lower, "make ") {
				hints = append(hints, trimFence(line))
			}
			if len(hints) >= 8 {
				break
			}
		}
	}
	return sortedUnique(hints)
}

var portPattern = regexp.MustCompile(`(?i)(?:port|addr|listen|localhost|127\.0\.0\.1|0\.0\.0\.0|:)(?:[^0-9]{0,24})([1-9][0-9]{2,4})`)

func extractPorts(text string) []int {
	var ports []int
	for _, match := range portPattern.FindAllStringSubmatch(text, -1) {
		if len(match) < 2 {
			continue
		}
		port, err := strconv.Atoi(match[1])
		if err == nil && port >= 1024 && port <= 65535 {
			ports = append(ports, port)
		}
	}
	return ports
}

func extractDependencies(text string) []string {
	lower := strings.ToLower(text)
	candidates := []string{"redis", "mysql", "postgres", "postgresql", "mongodb", "rabbitmq", "kafka", "etcd"}
	var values []string
	for _, candidate := range candidates {
		if strings.Contains(lower, candidate) {
			if candidate == "postgresql" {
				candidate = "postgres"
			}
			values = append(values, candidate)
		}
	}
	return sortedUnique(values)
}

func extractHealthPaths(text string) []string {
	var values []string
	if strings.Contains(text, "/healthz") {
		values = append(values, "/healthz")
	}
	if pathTokenExists(text, "/health") {
		values = append(values, "/health")
	}
	if pathTokenExists(text, "/ready") {
		values = append(values, "/ready")
	}
	if pathTokenExists(text, "/metrics") {
		values = append(values, "/metrics")
	}
	return sortedUnique(values)
}

func pathTokenExists(text string, path string) bool {
	idx := strings.Index(text, path)
	for idx >= 0 {
		after := idx + len(path)
		if after >= len(text) {
			return true
		}
		next := rune(text[after])
		if !(unicode.IsLetter(next) || unicode.IsDigit(next) || next == '_' || next == '-') {
			return true
		}
		nextIdx := strings.Index(text[after:], path)
		if nextIdx < 0 {
			return false
		}
		idx = after + nextIdx
	}
	return false
}

func shouldSkipDir(name string) bool {
	switch strings.ToLower(name) {
	case ".git", ".gsa", ".gocache", "vendor", "node_modules", "tmp", "bin", "dist", "build":
		return true
	default:
		return false
	}
}

func shouldInspectFile(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	ext := strings.ToLower(filepath.Ext(path))
	switch base {
	case "go.mod", "readme.md", "readme.txt", ".env.example", "dockerfile", "docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml":
		return true
	}
	switch ext {
	case ".go", ".yaml", ".yml", ".env", ".md", ".toml", ".json":
		return true
	default:
		return false
	}
}

func confidence(result Result) string {
	if result.Language == "go" && len(result.Entrypoints) > 0 && len(result.Ports) > 0 {
		return "high"
	}
	if result.Language == "go" && len(result.Entrypoints) > 0 {
		return "medium"
	}
	if result.Language != "unknown" || result.HasDockerfile || result.HasDockerCompose {
		return "medium"
	}
	return "low"
}

func inferRunCommand(result Result) string {
	if result.Language == "go" {
		if len(result.Entrypoints) > 0 {
			if result.Entrypoints[0] == "." {
				return "go run ."
			}
			return "go run ./" + result.Entrypoints[0]
		}
		return "go run ."
	}
	if len(result.StartupHints) > 0 {
		return result.StartupHints[0]
	}
	return "TODO"
}

func firstServicePort(values []int, def int) int {
	dependencyPorts := map[int]bool{
		2379:  true,
		3306:  true,
		5432:  true,
		5672:  true,
		6379:  true,
		9092:  true,
		27017: true,
	}
	for _, value := range values {
		if !dependencyPorts[value] {
			return value
		}
	}
	if len(values) == 0 {
		return def
	}
	return values[0]
}

func firstString(values []string, def string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" && value != "unknown" {
			return value
		}
	}
	return def
}

func firstHealthPath(values []string) string {
	preferred := []string{"/healthz", "/health", "/ready", "/metrics"}
	for _, candidate := range preferred {
		if contains(values, candidate) {
			return candidate
		}
	}
	return "/healthz"
}

func sortedInts(values map[int]bool) []int {
	var out []int
	for value := range values {
		out = append(out, value)
	}
	sort.Ints(out)
	return out
}

func sortedStrings(values map[string]bool) []string {
	var out []string
	for value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func sortedUnique(values []string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			seen[trimmed] = true
		}
	}
	return sortedStrings(seen)
}

func intStrings(values []int) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, strconv.Itoa(value))
	}
	return out
}

func joinInts(values []int) string {
	return strings.Join(intStrings(values), ", ")
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func isEnvName(value string) bool {
	if value == "" {
		return false
	}
	for i, r := range value {
		if i == 0 && !(unicode.IsLetter(r) || r == '_') {
			return false
		}
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_') {
			return false
		}
	}
	return true
}

func trimFence(value string) string {
	return strings.Trim(strings.TrimSpace(value), "`")
}

func sanitizeName(value string) string {
	value = strings.TrimSuffix(value, ".git")
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
