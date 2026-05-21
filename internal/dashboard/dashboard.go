package dashboard

import (
	"context"
	"encoding/json"
	"html/template"
	"net/http"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jingjie2002/GameServerProjectAgent/internal/deploy"
	"github.com/jingjie2002/GameServerProjectAgent/internal/diagnostics"
	"github.com/jingjie2002/GameServerProjectAgent/internal/permissions"
	"github.com/jingjie2002/GameServerProjectAgent/internal/projects"
)

type Options struct {
	Home               string
	Workspace          string
	Mode               permissions.Mode
	Manifests          []projects.Manifest
	DiagnosticsTimeout time.Duration
	Now                func() time.Time
}

type Summary struct {
	GeneratedAt  string        `json:"generated_at"`
	Home         string        `json:"home"`
	Workspace    string        `json:"workspace"`
	Mode         string        `json:"mode"`
	ProjectCount int           `json:"project_count"`
	Services     []ServiceView `json:"services"`
}

type ServiceView struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Description        string   `json:"description"`
	Type               string   `json:"type"`
	Root               string   `json:"root"`
	Capabilities       []string `json:"capabilities"`
	HealthURL          string   `json:"health_url"`
	HealthStatus       string   `json:"health_status"`
	CapabilitiesStatus string   `json:"capabilities_status"`
	MetricsStatus      string   `json:"metrics_status"`
	DeployStatus       string   `json:"deploy_status"`
	PID                int      `json:"pid"`
	Command            string   `json:"command"`
	LogPath            string   `json:"log_path"`
	LogURL             string   `json:"log_url"`
	Ports              []int    `json:"ports"`
	StartedAt          string   `json:"started_at,omitempty"`
	StoppedAt          string   `json:"stopped_at,omitempty"`
}

type server struct {
	home      string
	workspace string
	mode      permissions.Mode
	manifests []projects.Manifest
	timeout   time.Duration
	now       func() time.Time
}

func NewHandler(opts Options) http.Handler {
	timeout := opts.DiagnosticsTimeout
	if timeout <= 0 {
		timeout = time.Second
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	s := server{
		home:      opts.Home,
		workspace: opts.Workspace,
		mode:      opts.Mode,
		manifests: append([]projects.Manifest(nil), opts.Manifests...),
		timeout:   timeout,
		now:       now,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/logs/", s.handleLog)
	return mux
}

func (s server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, s.summary(r.Context()))
}

func (s server) handleLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.Trim(path.Clean(strings.TrimPrefix(r.URL.Path, "/api/logs/")), "/")
	if id == "" || id == "." {
		http.NotFound(w, r)
		return
	}
	project, ok := s.findProject(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	tail := 80
	if raw := r.URL.Query().Get("tail"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			tail = n
		}
	}
	state := deploy.Manager{Home: s.home, Mode: s.mode}.Status(project)
	if strings.TrimSpace(state.LogPath) == "" {
		http.Error(w, "log path is empty", http.StatusNotFound)
		return
	}
	if !withinDir(filepath.Join(s.home, ".gsa", "services"), state.LogPath) {
		http.Error(w, "log path is outside service log directory", http.StatusForbidden)
		return
	}
	content, err := deploy.ReadLogTail(state.LogPath, tail)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(content))
}

func (s server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pageTemplate.Execute(w, s.summary(r.Context()))
}

func (s server) summary(ctx context.Context) Summary {
	report := diagnostics.NewClient(s.timeout).Diagnose(ctx, s.mode.String(), s.manifests, diagnostics.Options{})
	diagnosticsByID := map[string]diagnostics.ServiceReport{}
	for _, service := range report.Services {
		diagnosticsByID[service.ProjectID] = service
	}
	manager := deploy.Manager{Home: s.home, Mode: s.mode}
	services := make([]ServiceView, 0, len(s.manifests))
	for _, manifest := range s.manifests {
		state := manager.Status(manifest)
		service := diagnosticsByID[manifest.ID]
		root := deploy.ProjectRoot(manifest)
		if state.Root != "" {
			root = state.Root
		}
		healthURL := manifest.Health.URL
		if healthURL == "" {
			healthURL = manifest.Health.LegacyURL
		}
		services = append(services, ServiceView{
			ID:                 manifest.ID,
			Name:               manifest.Name,
			Description:        manifest.Description,
			Type:               manifest.Type,
			Root:               root,
			Capabilities:       append([]string(nil), manifest.Capabilities...),
			HealthURL:          healthURL,
			HealthStatus:       emptyAs(service.Health.Status, "unknown"),
			CapabilitiesStatus: emptyAs(service.Capabilities.Status, "unknown"),
			MetricsStatus:      emptyAs(service.Metrics.Status, "unknown"),
			DeployStatus:       emptyAs(state.Status, "not_started"),
			PID:                state.PID,
			Command:            state.Command,
			LogPath:            state.LogPath,
			LogURL:             "/api/logs/" + manifest.ID + "?tail=120",
			Ports:              append([]int(nil), state.Ports...),
			StartedAt:          state.StartedAt,
			StoppedAt:          state.StoppedAt,
		})
	}
	return Summary{
		GeneratedAt:  s.now().Format("2006-01-02 15:04:05"),
		Home:         s.home,
		Workspace:    s.workspace,
		Mode:         s.mode.String(),
		ProjectCount: len(services),
		Services:     services,
	}
}

func (s server) findProject(id string) (projects.Manifest, bool) {
	for _, manifest := range s.manifests {
		if strings.EqualFold(manifest.ID, id) || strings.EqualFold(manifest.Name, id) {
			return manifest, true
		}
	}
	return projects.Manifest{}, false
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(value)
}

func emptyAs(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func withinDir(dir string, file string) bool {
	base, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return false
	}
	target, err := filepath.Abs(filepath.Clean(file))
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func statusClass(value string) string {
	switch strings.ToLower(value) {
	case "ok", "running":
		return "ok"
	case "degraded":
		return "warn"
	case "unavailable", "stopped", "not_started", "unknown":
		return "muted"
	default:
		return "bad"
	}
}

var pageTemplate = template.Must(template.New("dashboard").Funcs(template.FuncMap{
	"statusClass": statusClass,
	"joinPorts": func(values []int) string {
		if len(values) == 0 {
			return "-"
		}
		parts := make([]string, 0, len(values))
		for _, value := range values {
			parts = append(parts, strconv.Itoa(value))
		}
		return strings.Join(parts, ", ")
	},
	"joinCaps": func(values []string) string {
		if len(values) == 0 {
			return "-"
		}
		return strings.Join(values, ", ")
	},
	"empty": func(value string) string {
		if strings.TrimSpace(value) == "" {
			return "-"
		}
		return value
	},
}).Parse(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>GameServerProjectAgent Dashboard</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f6f7f9;
      --panel: #ffffff;
      --line: #d9dee7;
      --text: #1b2430;
      --muted: #637083;
      --ok: #087f5b;
      --warn: #9a6700;
      --bad: #c92a2a;
      --soft-ok: #dff7ed;
      --soft-warn: #fff3bf;
      --soft-bad: #ffe3e3;
      --soft-muted: #edf1f5;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      background: var(--bg);
      color: var(--text);
      font-family: "Segoe UI", Arial, sans-serif;
      font-size: 14px;
      letter-spacing: 0;
    }
    header {
      border-bottom: 1px solid var(--line);
      background: var(--panel);
      padding: 18px 24px 14px;
    }
    h1 {
      margin: 0 0 8px;
      font-size: 22px;
      font-weight: 650;
    }
    .meta {
      display: flex;
      flex-wrap: wrap;
      gap: 10px 18px;
      color: var(--muted);
      font-size: 13px;
    }
    main {
      max-width: 1180px;
      margin: 0 auto;
      padding: 20px;
    }
    .toolbar {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 12px;
      margin-bottom: 14px;
    }
    .toolbar strong { font-size: 15px; }
    .links {
      display: flex;
      gap: 10px;
      flex-wrap: wrap;
    }
    a {
      color: #0b7285;
      text-decoration: none;
      font-weight: 600;
    }
    a:hover { text-decoration: underline; }
    .table-wrap {
      overflow-x: auto;
      border: 1px solid var(--line);
      border-radius: 8px;
      background: var(--panel);
    }
    table {
      width: 100%;
      min-width: 920px;
      border-collapse: collapse;
    }
    th, td {
      padding: 12px 14px;
      border-bottom: 1px solid var(--line);
      text-align: left;
      vertical-align: top;
    }
    th {
      background: #f0f3f6;
      color: #334155;
      font-size: 12px;
      text-transform: uppercase;
    }
    tr:last-child td { border-bottom: 0; }
    .name {
      font-weight: 700;
      margin-bottom: 4px;
    }
    .sub {
      color: var(--muted);
      font-size: 12px;
      line-height: 1.5;
      overflow-wrap: anywhere;
    }
    .pill {
      display: inline-flex;
      align-items: center;
      min-height: 24px;
      padding: 3px 8px;
      border-radius: 8px;
      font-size: 12px;
      font-weight: 700;
      white-space: nowrap;
    }
    .pill.ok { color: var(--ok); background: var(--soft-ok); }
    .pill.warn { color: var(--warn); background: var(--soft-warn); }
    .pill.bad { color: var(--bad); background: var(--soft-bad); }
    .pill.muted { color: var(--muted); background: var(--soft-muted); }
    .stack {
      display: grid;
      gap: 7px;
    }
    code {
      font-family: Consolas, "SFMono-Regular", monospace;
      font-size: 12px;
      color: #243447;
    }
    footer {
      padding: 16px 0 4px;
      color: var(--muted);
      font-size: 12px;
    }
    @media (max-width: 760px) {
      header { padding: 16px; }
      main { padding: 14px; }
      .toolbar { align-items: flex-start; flex-direction: column; }
      th, td { padding: 10px; }
    }
  </style>
</head>
<body>
  <header>
    <h1>GameServerProjectAgent</h1>
    <div class="meta">
      <span>模式：{{.Mode}}</span>
      <span>模块：{{.ProjectCount}}</span>
      <span>更新时间：{{.GeneratedAt}}</span>
      <span>工作区：{{.Workspace}}</span>
    </div>
  </header>
  <main>
    <div class="toolbar">
      <strong>服务模块状态</strong>
      <div class="links">
        <a href="/">刷新</a>
        <a href="/api/status">JSON</a>
        <a href="/healthz">healthz</a>
      </div>
    </div>
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>模块</th>
            <th>部署</th>
            <th>健康</th>
            <th>能力/指标</th>
            <th>端口</th>
            <th>日志</th>
          </tr>
        </thead>
        <tbody>
          {{range .Services}}
          <tr>
            <td>
              <div class="name">{{.Name}}</div>
              <div class="sub"><code>{{.ID}}</code> · {{empty .Type}}</div>
              <div class="sub">{{empty .Description}}</div>
              <div class="sub">{{.Root}}</div>
            </td>
            <td>
              <div class="stack">
                <span class="pill {{statusClass .DeployStatus}}">{{.DeployStatus}}</span>
                <span class="sub">PID: {{.PID}}</span>
                <span class="sub"><code>{{empty .Command}}</code></span>
              </div>
            </td>
            <td>
              <div class="stack">
                <span class="pill {{statusClass .HealthStatus}}">{{.HealthStatus}}</span>
                <span class="sub">{{empty .HealthURL}}</span>
              </div>
            </td>
            <td>
              <div class="stack">
                <span>capabilities <span class="pill {{statusClass .CapabilitiesStatus}}">{{.CapabilitiesStatus}}</span></span>
                <span>metrics <span class="pill {{statusClass .MetricsStatus}}">{{.MetricsStatus}}</span></span>
                <span class="sub">{{joinCaps .Capabilities}}</span>
              </div>
            </td>
            <td><code>{{joinPorts .Ports}}</code></td>
            <td>
              <div class="stack">
                <a href="{{.LogURL}}">查看日志</a>
                <span class="sub">{{empty .LogPath}}</span>
              </div>
            </td>
          </tr>
          {{else}}
          <tr>
            <td colspan="6">暂无已注册模块。</td>
          </tr>
          {{end}}
        </tbody>
      </table>
    </div>
    <footer>只读面板：启动、停止和高风险动作仍需在 CLI 中显式确认。</footer>
  </main>
</body>
</html>`))
