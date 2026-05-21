package deploy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/jingjie2002/GameServerProjectAgent/internal/permissions"
	"github.com/jingjie2002/GameServerProjectAgent/internal/projects"
)

type Spec struct {
	Path         string
	Runtime      string
	Confidence   string
	BuildCommand string
	RunCommand   string
	Ports        []Port
	Dependencies []string
	Env          []EnvVar
	HealthPath   string
	Notes        []string
}

type Port struct {
	Name  string
	Value int
}

type EnvVar struct {
	Name     string
	Required bool
}

type ServiceState struct {
	ProjectID   string `json:"project_id"`
	ProjectName string `json:"project_name"`
	Root        string `json:"root"`
	Command     string `json:"command"`
	PID         int    `json:"pid"`
	Status      string `json:"status"`
	StartedAt   string `json:"started_at,omitempty"`
	StoppedAt   string `json:"stopped_at,omitempty"`
	LogPath     string `json:"log_path"`
	Ports       []int  `json:"ports,omitempty"`
}

type Manager struct {
	Home string
	Mode permissions.Mode
	Now  func() time.Time
}

func LoadSpec(path string) (Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Spec{}, err
	}
	spec, err := ParseSpec(data)
	if err != nil {
		return Spec{}, err
	}
	spec.Path = filepath.Clean(path)
	return spec, nil
}

func ParseSpec(data []byte) (Spec, error) {
	var spec Spec
	input := bufio.NewScanner(bytes.NewReader(data))
	var section string
	var currentPort *Port
	var currentEnv *EnvVar
	for input.Scan() {
		raw := strings.TrimPrefix(stripComment(input.Text()), "\ufeff")
		if strings.TrimSpace(raw) == "" {
			continue
		}
		indent := leadingSpaces(raw)
		line := strings.TrimSpace(raw)
		key, value, ok := splitKV(line)
		if indent == 0 && ok {
			section = key
			currentPort = nil
			currentEnv = nil
			switch key {
			case "runtime":
				spec.Runtime = value
			case "confidence":
				spec.Confidence = value
			}
			continue
		}
		switch section {
		case "build":
			if indent >= 2 && ok && key == "command" {
				spec.BuildCommand = value
			}
		case "run":
			if indent >= 2 && ok && key == "command" {
				spec.RunCommand = value
			}
		case "ports":
			if item, isItem := listItem(line); isItem {
				port := Port{}
				if itemKey, itemValue, hasKV := splitKV(item); hasKV {
					assignPort(&port, itemKey, itemValue)
				}
				spec.Ports = append(spec.Ports, port)
				currentPort = &spec.Ports[len(spec.Ports)-1]
				continue
			}
			if currentPort != nil && indent >= 4 && ok {
				assignPort(currentPort, key, value)
			}
		case "dependencies":
			if item, isItem := listItem(line); isItem {
				spec.Dependencies = append(spec.Dependencies, item)
			}
		case "env":
			if item, isItem := listItem(line); isItem {
				env := EnvVar{}
				if itemKey, itemValue, hasKV := splitKV(item); hasKV {
					assignEnv(&env, itemKey, itemValue)
				}
				spec.Env = append(spec.Env, env)
				currentEnv = &spec.Env[len(spec.Env)-1]
				continue
			}
			if currentEnv != nil && indent >= 4 && ok {
				assignEnv(currentEnv, key, value)
			}
		case "health_check":
			if indent >= 2 && ok && key == "path" {
				spec.HealthPath = value
			}
		case "notes":
			if item, isItem := listItem(line); isItem {
				spec.Notes = append(spec.Notes, item)
			}
		}
	}
	if err := input.Err(); err != nil {
		return Spec{}, err
	}
	if spec.RunCommand == "" {
		return Spec{}, fmt.Errorf("deploy spec must include run.command")
	}
	return spec, nil
}

func (m Manager) Plan(project projects.Manifest) (projects.Manifest, Spec, string, error) {
	root := ProjectRoot(project)
	specPath, err := ResolveSpecPath(project, root)
	if err != nil {
		return project, Spec{}, root, err
	}
	spec, err := LoadSpec(specPath)
	return project, spec, root, err
}

func (m Manager) Start(project projects.Manifest, confirm bool) (ServiceState, error) {
	if !confirm {
		return ServiceState{}, fmt.Errorf("deploy start requires --confirm")
	}
	if !m.Mode.Allows(permissions.FullAccessMode) {
		return ServiceState{}, fmt.Errorf("deploy start requires --mode 完全访问权限")
	}
	_, spec, root, err := m.Plan(project)
	if err != nil {
		return ServiceState{}, err
	}
	if strings.TrimSpace(spec.RunCommand) == "" || strings.EqualFold(strings.TrimSpace(spec.RunCommand), "TODO") {
		return ServiceState{}, fmt.Errorf("deploy run command is empty or TODO")
	}
	if err := os.MkdirAll(serviceDir(m.Home), 0o755); err != nil {
		return ServiceState{}, err
	}
	logPath := serviceLogPath(m.Home, project.ID)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return ServiceState{}, err
	}
	defer logFile.Close()

	cmd := shellCommand(spec.RunCommand)
	cmd.Dir = root
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		return ServiceState{}, err
	}
	now := m.now().Format(time.RFC3339)
	state := ServiceState{
		ProjectID:   project.ID,
		ProjectName: project.Name,
		Root:        root,
		Command:     spec.RunCommand,
		PID:         cmd.Process.Pid,
		Status:      "running",
		StartedAt:   now,
		LogPath:     logPath,
		Ports:       portValues(spec.Ports),
	}
	if err := SaveState(m.Home, state); err != nil {
		return state, err
	}
	return state, nil
}

func (m Manager) Stop(projectID string, confirm bool) (ServiceState, error) {
	if !confirm {
		return ServiceState{}, fmt.Errorf("deploy stop requires --confirm")
	}
	if !m.Mode.Allows(permissions.FullAccessMode) {
		return ServiceState{}, fmt.Errorf("deploy stop requires --mode 完全访问权限")
	}
	state, err := LoadState(m.Home, projectID)
	if err != nil {
		return ServiceState{}, err
	}
	if state.PID > 0 {
		if err := killProcess(state.PID); err != nil && ProcessAlive(state.PID) {
			return state, err
		}
		if !waitForExit(state.PID, 5*time.Second) {
			return state, fmt.Errorf("process %d is still alive after stop", state.PID)
		}
	}
	if !waitForPortsClosed(state.Ports, 60*time.Second) {
		return state, fmt.Errorf("service ports are still reachable after stop: %s", joinInts(state.Ports))
	}
	state.Status = "stopped"
	state.StoppedAt = m.now().Format(time.RFC3339)
	if err := SaveState(m.Home, state); err != nil {
		return state, err
	}
	return state, nil
}

func (m Manager) Status(project projects.Manifest) ServiceState {
	state, err := LoadState(m.Home, project.ID)
	if err != nil {
		return ServiceState{
			ProjectID:   project.ID,
			ProjectName: project.Name,
			Root:        ProjectRoot(project),
			Status:      "not_started",
			LogPath:     serviceLogPath(m.Home, project.ID),
		}
	}
	if state.PID > 0 && ProcessAlive(state.PID) {
		state.Status = "running"
	} else if anyPortOpen(state.Ports) {
		state.Status = "running"
	} else if state.Status == "running" {
		state.Status = "stopped"
	}
	return state
}

func FormatPlan(project projects.Manifest, spec Spec, root string) string {
	var lines []string
	lines = append(lines, "部署计划")
	lines = append(lines, "project_id: "+project.ID)
	lines = append(lines, "project_name: "+project.Name)
	lines = append(lines, "root: "+root)
	lines = append(lines, "deploy_yaml: "+spec.Path)
	lines = append(lines, "runtime: "+spec.Runtime)
	lines = append(lines, "confidence: "+spec.Confidence)
	lines = append(lines, "build: "+emptyAsNone(spec.BuildCommand))
	lines = append(lines, "run: "+spec.RunCommand)
	if len(spec.Ports) > 0 {
		lines = append(lines, "ports: "+formatPorts(spec.Ports))
	}
	if len(spec.Dependencies) > 0 {
		lines = append(lines, "dependencies: "+strings.Join(spec.Dependencies, ", "))
	}
	if spec.HealthPath != "" {
		lines = append(lines, "health_check: "+spec.HealthPath)
	}
	lines = append(lines, "安全提示：start/stop 需要 --mode 完全访问权限 和 --confirm。")
	return strings.Join(lines, "\n")
}

func FormatState(state ServiceState) string {
	lines := []string{
		"服务状态",
		"project_id: " + state.ProjectID,
		"project_name: " + state.ProjectName,
		"status: " + state.Status,
		"pid: " + strconv.Itoa(state.PID),
		"root: " + state.Root,
		"command: " + state.Command,
		"log: " + state.LogPath,
	}
	if state.StartedAt != "" {
		lines = append(lines, "started_at: "+state.StartedAt)
	}
	if state.StoppedAt != "" {
		lines = append(lines, "stopped_at: "+state.StoppedAt)
	}
	return strings.Join(lines, "\n")
}

func ReadLogTail(path string, limit int) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if limit <= 0 {
		limit = 80
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return strings.Join(lines, "\n"), nil
}

func ResolveSpecPath(project projects.Manifest, root string) (string, error) {
	var candidates []string
	if project.ManifestPath != "" {
		candidates = append(candidates, filepath.Join(filepath.Dir(project.ManifestPath), "deploy.generated.yaml"))
	}
	if root != "" {
		candidates = append(candidates, filepath.Join(root, "deploy.generated.yaml"))
	}
	for _, candidate := range candidates {
		if fileExists(candidate) {
			return filepath.Clean(candidate), nil
		}
	}
	return "", fmt.Errorf("deploy.generated.yaml not found for project %s; run gsa scan first", project.ID)
}

func ProjectRoot(project projects.Manifest) string {
	root := filepath.Clean(project.Root)
	if root == "." || root == "" {
		if project.ManifestPath != "" {
			return filepath.Dir(project.ManifestPath)
		}
		return "."
	}
	if filepath.IsAbs(root) {
		return root
	}
	if project.ManifestPath != "" {
		return filepath.Clean(filepath.Join(filepath.Dir(project.ManifestPath), root))
	}
	return root
}

func SaveState(home string, state ServiceState) error {
	if err := os.MkdirAll(serviceDir(home), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(statePath(home, state.ProjectID), data, 0o644)
}

func LoadState(home string, projectID string) (ServiceState, error) {
	data, err := os.ReadFile(statePath(home, projectID))
	if err != nil {
		return ServiceState{}, err
	}
	var state ServiceState
	if err := json.Unmarshal(data, &state); err != nil {
		return ServiceState{}, err
	}
	return state, nil
}

func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	if runtime.GOOS == "windows" {
		if alive, ok := windowsProcessAlive(pid); ok {
			return alive
		}
		out, err := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/FO", "CSV", "/NH").CombinedOutput()
		if err != nil {
			return false
		}
		return strings.Contains(string(out), `"`+strconv.Itoa(pid)+`"`)
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

func (m Manager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func shellCommand(command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/C", command)
	}
	return exec.Command("sh", "-c", command)
}

func killProcess(pid int) error {
	if runtime.GOOS == "windows" {
		if err := killWindowsProcessTree(pid); err == nil {
			return nil
		}
		if err := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F").Run(); err == nil {
			return nil
		}
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}

func killWindowsProcessTree(pid int) error {
	script := fmt.Sprintf(`$ErrorActionPreference = "SilentlyContinue";
function Stop-Tree([int]$Id) {
  Get-CimInstance Win32_Process -Filter "ParentProcessId=$Id" | ForEach-Object { Stop-Tree ([int]$_.ProcessId) }
  Stop-Process -Id $Id -Force -ErrorAction SilentlyContinue
}
Stop-Tree %d`, pid)
	return exec.Command("powershell", "-NoProfile", "-Command", script).Run()
}

func windowsProcessAlive(pid int) (bool, bool) {
	command := fmt.Sprintf("$p = Get-Process -Id %d -ErrorAction SilentlyContinue; if ($null -eq $p) { exit 2 }", pid)
	err := exec.Command("powershell", "-NoProfile", "-Command", command).Run()
	if err == nil {
		return true, true
	}
	if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 2 {
		return false, true
	}
	return false, false
}

func waitForExit(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if !ProcessAlive(pid) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func waitForPortsClosed(ports []int, timeout time.Duration) bool {
	if len(ports) == 0 {
		return true
	}
	deadline := time.Now().Add(timeout)
	for {
		if !anyPortOpen(ports) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func anyPortOpen(ports []int) bool {
	for _, port := range ports {
		if port <= 0 {
			continue
		}
		conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 250*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return true
		}
	}
	return false
}

func serviceDir(home string) string {
	return filepath.Join(home, ".gsa", "services")
}

func serviceLogPath(home string, projectID string) string {
	return filepath.Join(serviceDir(home), sanitizeName(projectID)+".log")
}

func statePath(home string, projectID string) string {
	return filepath.Join(serviceDir(home), sanitizeName(projectID)+".json")
}

func assignPort(port *Port, key string, value string) {
	switch key {
	case "name":
		port.Name = value
	case "value":
		port.Value, _ = strconv.Atoi(value)
	}
}

func assignEnv(env *EnvVar, key string, value string) {
	switch key {
	case "name":
		env.Name = value
	case "required":
		env.Required = strings.EqualFold(value, "true") || value == "1" || strings.EqualFold(value, "yes")
	}
}

func formatPorts(ports []Port) string {
	values := make([]string, 0, len(ports))
	for _, port := range ports {
		if port.Name != "" {
			values = append(values, fmt.Sprintf("%s=%d", port.Name, port.Value))
		} else {
			values = append(values, strconv.Itoa(port.Value))
		}
	}
	return strings.Join(values, ", ")
}

func portValues(ports []Port) []int {
	values := make([]int, 0, len(ports))
	for _, port := range ports {
		if port.Value > 0 {
			values = append(values, port.Value)
		}
	}
	return values
}

func joinInts(values []int) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, strconv.Itoa(value))
	}
	return strings.Join(parts, ", ")
}

func emptyAsNone(value string) string {
	if strings.TrimSpace(value) == "" {
		return "none"
	}
	return value
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
