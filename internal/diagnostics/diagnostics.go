package diagnostics

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jingjie2002/GameServerProjectAgent/internal/projects"
)

const (
	StatusOK          = "ok"
	StatusDegraded    = "degraded"
	StatusUnavailable = "unavailable"
)

type Options struct {
	IncludeRisk bool
}

type Client struct {
	HTTPClient *http.Client
	Now        func() time.Time
}

type Report struct {
	GeneratedAt     time.Time
	Mode            string
	OverallStatus   string
	Services        []ServiceReport
	Risk            *RiskReport
	Findings        []Finding
	Recommendations []string
}

type ServiceReport struct {
	ProjectID    string
	Name         string
	Status       string
	Health       EndpointCheck
	Capabilities EndpointCheck
	Metrics      MetricsReport
	Findings     []Finding
}

type EndpointCheck struct {
	URL        string
	Status     string
	HTTPStatus int
	Latency    time.Duration
	Summary    string
	Error      string
}

type MetricsReport struct {
	URL        string
	Status     string
	HTTPStatus int
	Latency    time.Duration
	Values     map[string]float64
	Highlights []string
	Error      string
}

type RiskReport struct {
	Status       string
	URL          string
	HTTPStatus   int
	RiskLevel    string
	Score        int
	AuditCount   int
	FindingCount int
	Summary      string
	Error        string
}

type Finding struct {
	Severity  string
	ProjectID string
	Type      string
	Message   string
}

type MetricSample struct {
	Name   string
	Labels map[string]string
	Value  float64
}

func NewClient(timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &Client{
		HTTPClient: &http.Client{Timeout: timeout},
		Now:        time.Now,
	}
}

func (c *Client) Diagnose(ctx context.Context, mode string, manifests []projects.Manifest, options Options) Report {
	now := c.now()
	report := Report{
		GeneratedAt: now,
		Mode:        mode,
		Services:    make([]ServiceReport, 0, len(manifests)),
	}
	for _, manifest := range manifests {
		service := c.diagnoseService(ctx, manifest)
		report.Services = append(report.Services, service)
		report.Findings = append(report.Findings, service.Findings...)
	}
	if options.IncludeRisk {
		if gameops, ok := findProject(manifests, "gameops"); ok {
			risk := c.analyzeGameOpsRisk(ctx, gameops)
			report.Risk = &risk
			if risk.Status == StatusUnavailable {
				report.Findings = append(report.Findings, Finding{
					Severity:  "medium",
					ProjectID: "gameops",
					Type:      "gm_risk_unavailable",
					Message:   "GameOps GM 风险分析暂不可用：" + risk.Error,
				})
			}
			if risk.Status == StatusDegraded {
				report.Findings = append(report.Findings, Finding{
					Severity:  severityFromRiskLevel(risk.RiskLevel),
					ProjectID: "gameops",
					Type:      "gm_risk_detected",
					Message:   fmt.Sprintf("GameOps 风险等级为 %s，分数 %d，命中 %d 类发现", risk.RiskLevel, risk.Score, risk.FindingCount),
				})
			}
		}
	}
	report.OverallStatus = overallStatus(report.Services, report.Risk, report.Findings)
	report.Recommendations = recommendations(report)
	return report
}

func (c *Client) diagnoseService(ctx context.Context, manifest projects.Manifest) ServiceReport {
	service := ServiceReport{
		ProjectID: manifest.ID,
		Name:      manifest.Name,
	}
	healthURL := manifest.Health.URL
	if healthURL == "" {
		healthURL = manifest.Health.LegacyURL
	}
	service.Health = c.getJSON(ctx, healthURL, "health")
	service.Capabilities = c.getJSON(ctx, manifest.CapabilitiesEndpoint.URL, "capabilities")
	service.Metrics = c.getMetrics(ctx, manifest)
	service.Findings = serviceFindings(manifest, service)
	service.Status = serviceStatus(service)
	return service
}

func (c *Client) getJSON(ctx context.Context, rawURL string, label string) EndpointCheck {
	check := EndpointCheck{URL: rawURL}
	if rawURL == "" {
		check.Status = StatusUnavailable
		check.Error = label + " endpoint not configured"
		return check
	}
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		check.Status = StatusUnavailable
		check.Error = err.Error()
		return check
	}
	resp, err := c.httpClient().Do(req)
	check.Latency = time.Since(start)
	if err != nil {
		check.Status = StatusUnavailable
		check.Error = err.Error()
		return check
	}
	defer resp.Body.Close()
	check.HTTPStatus = resp.StatusCode
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		check.Status = StatusDegraded
		check.Error = fmt.Sprintf("http %d", resp.StatusCode)
		check.Summary = trimForSummary(string(body))
		return check
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		check.Status = StatusDegraded
		check.Error = "invalid json: " + err.Error()
		check.Summary = trimForSummary(string(body))
		return check
	}
	bodyStatus := strings.ToLower(fmt.Sprint(payload["status"]))
	switch bodyStatus {
	case "", "<nil>", StatusOK, "healthy":
		check.Status = StatusOK
	default:
		check.Status = StatusDegraded
	}
	check.Summary = jsonSummary(payload)
	return check
}

func (c *Client) getMetrics(ctx context.Context, manifest projects.Manifest) MetricsReport {
	report := MetricsReport{
		URL:    manifest.Metrics.URL,
		Values: map[string]float64{},
	}
	if manifest.Metrics.URL == "" {
		report.Status = StatusUnavailable
		report.Error = "metrics endpoint not configured"
		return report
	}
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifest.Metrics.URL, nil)
	if err != nil {
		report.Status = StatusUnavailable
		report.Error = err.Error()
		return report
	}
	resp, err := c.httpClient().Do(req)
	report.Latency = time.Since(start)
	if err != nil {
		report.Status = StatusUnavailable
		report.Error = err.Error()
		return report
	}
	defer resp.Body.Close()
	report.HTTPStatus = resp.StatusCode
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		report.Status = StatusDegraded
		report.Error = fmt.Sprintf("http %d", resp.StatusCode)
		return report
	}
	samples := ParsePrometheusMetrics(string(body))
	report.Values, report.Highlights = interpretMetrics(manifest.ID, samples)
	report.Status = StatusOK
	return report
}

func (c *Client) analyzeGameOpsRisk(ctx context.Context, manifest projects.Manifest) RiskReport {
	base, err := endpointBase(manifest.CapabilitiesEndpoint.URL, manifest.Health.URL)
	if err != nil {
		return RiskReport{Status: StatusUnavailable, Error: err.Error()}
	}
	loginURL := base + "/api/admin/login"
	analyzeURL := base + "/api/risk/analyze"
	token, err := c.loginGameOps(ctx, loginURL)
	if err != nil {
		return RiskReport{Status: StatusUnavailable, URL: analyzeURL, Error: err.Error()}
	}

	now := c.now()
	from := startOfDay(now).UnixMilli()
	to := now.UnixMilli()
	body, _ := json.Marshal(map[string]any{
		"from_ms":     from,
		"to_ms":       to,
		"use_ai":      true,
		"ai_provider": "mock-ai",
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, analyzeURL, bytes.NewReader(body))
	if err != nil {
		return RiskReport{Status: StatusUnavailable, URL: analyzeURL, Error: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return RiskReport{Status: StatusUnavailable, URL: analyzeURL, Error: err.Error()}
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	risk := RiskReport{URL: analyzeURL, HTTPStatus: resp.StatusCode}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		risk.Status = StatusUnavailable
		risk.Error = fmt.Sprintf("http %d: %s", resp.StatusCode, trimForSummary(string(payload)))
		return risk
	}
	var decoded struct {
		RiskLevel  string          `json:"risk_level"`
		Score      int             `json:"score"`
		Summary    string          `json:"summary"`
		AuditCount int             `json:"audit_count"`
		Findings   json.RawMessage `json:"findings"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		risk.Status = StatusUnavailable
		risk.Error = "invalid risk response: " + err.Error()
		return risk
	}
	var findings []any
	_ = json.Unmarshal(decoded.Findings, &findings)
	risk.RiskLevel = decoded.RiskLevel
	if risk.RiskLevel == "" {
		risk.RiskLevel = "unknown"
	}
	risk.Score = decoded.Score
	risk.AuditCount = decoded.AuditCount
	risk.FindingCount = len(findings)
	risk.Summary = decoded.Summary
	if risk.RiskLevel == "normal" || risk.RiskLevel == "low" {
		risk.Status = StatusOK
	} else {
		risk.Status = StatusDegraded
	}
	return risk
}

func (c *Client) loginGameOps(ctx context.Context, loginURL string) (string, error) {
	var lastErr error
	for _, credential := range gameOpsCredentials() {
		body, _ := json.Marshal(map[string]string{
			"username": credential.username,
			"password": credential.password,
		})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, loginURL, bytes.NewReader(body))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.httpClient().Do(req)
		if err != nil {
			return "", err
		}
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("login failed for %s: http %d", credential.username, resp.StatusCode)
			continue
		}
		var decoded struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(payload, &decoded); err != nil {
			return "", err
		}
		if decoded.Token == "" {
			return "", errors.New("login response missing token")
		}
		return decoded.Token, nil
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", errors.New("no GameOps credential configured")
}

func ParsePrometheusMetrics(text string) []MetricSample {
	var samples []MetricSample
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		value, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			continue
		}
		name, labels := parseMetricIdentity(fields[0])
		if name == "" {
			continue
		}
		samples = append(samples, MetricSample{Name: name, Labels: labels, Value: value})
	}
	return samples
}

func FormatReport(report Report) string {
	var lines []string
	lines = append(lines, "多服务诊断报告")
	lines = append(lines, "生成时间："+report.GeneratedAt.Format("2006-01-02 15:04:05"))
	lines = append(lines, "当前模式："+report.Mode)
	lines = append(lines, "总体状态："+report.OverallStatus)
	lines = append(lines, "")
	lines = append(lines, "服务状态：")
	for _, service := range report.Services {
		lines = append(lines, fmt.Sprintf("- %s (%s)：%s", service.ProjectID, service.Name, service.Status))
		lines = append(lines, endpointLine("  health", service.Health))
		lines = append(lines, endpointLine("  capabilities", service.Capabilities))
		lines = append(lines, metricsLine("  metrics", service.Metrics))
		for _, highlight := range service.Metrics.Highlights {
			lines = append(lines, "  指标："+highlight)
		}
	}
	if report.Risk != nil {
		lines = append(lines, "")
		lines = append(lines, "GM 风险分析："+formatRiskLine(*report.Risk))
	}
	lines = append(lines, "")
	lines = append(lines, "诊断发现：")
	if len(report.Findings) == 0 {
		lines = append(lines, "- 未发现需要立即处理的异常。")
	} else {
		for _, finding := range report.Findings {
			lines = append(lines, fmt.Sprintf("- [%s] %s %s：%s", finding.Severity, finding.ProjectID, finding.Type, finding.Message))
		}
	}
	lines = append(lines, "")
	lines = append(lines, "建议：")
	for _, recommendation := range report.Recommendations {
		lines = append(lines, "- "+recommendation)
	}
	return strings.Join(lines, "\n")
}

func FormatRiskReport(report Report) string {
	if report.Risk == nil {
		return "未找到 GameOps 风险分析结果。"
	}
	risk := *report.Risk
	var lines []string
	lines = append(lines, "GameOps GM 风险诊断")
	lines = append(lines, "生成时间："+report.GeneratedAt.Format("2006-01-02 15:04:05"))
	lines = append(lines, "状态："+formatRiskLine(risk))
	if risk.Summary != "" {
		lines = append(lines, "摘要："+risk.Summary)
	}
	if risk.Error != "" {
		lines = append(lines, "错误："+risk.Error)
	}
	return strings.Join(lines, "\n")
}

func interpretMetrics(projectID string, samples []MetricSample) (map[string]float64, []string) {
	values := map[string]float64{}
	var highlights []string
	add := func(name string, value float64, label string) {
		values[name] = value
		highlights = append(highlights, fmt.Sprintf("%s=%.0f%s", name, value, label))
	}
	switch projectID {
	case "corerank":
		timeout := sumMetricWhere(samples, "corerank_matcher_match_total", "status", "timeout") +
			sumMetricWhere(samples, "corerank_matcher_ticket_events_total", "status", "timeout")
		queued := sumMetric(samples, "corerank_matcher_queued_tickets")
		roomFailures := sumMetric(samples, "corerank_room_assignment_failures_total")
		add("match_timeout_total", timeout, "")
		add("queued_tickets", queued, "")
		add("room_assignment_failures_total", roomFailures, "")
	case "arenagate":
		active := sumMetric(samples, "arenagate_active_sessions")
		errors := sumMetric(samples, "arenagate_errors_total")
		coreErrors := sumMetric(samples, "arenagate_core_errors_total")
		add("active_sessions", active, "")
		add("errors_total", errors, "")
		add("core_errors_total", coreErrors, "")
	case "gameops":
		errors := sumMetric(samples, "gameops_errors_total")
		riskAnalyses := sumMetric(samples, "gameops_risk_analyses_total")
		auditWrites := sumMetric(samples, "gameops_audit_writes_total")
		add("errors_total", errors, "")
		add("risk_analyses_total", riskAnalyses, "")
		add("audit_writes_total", auditWrites, "")
	default:
		for _, sample := range samples {
			values[sample.Name] += sample.Value
		}
	}
	sort.Strings(highlights)
	return values, highlights
}

func serviceFindings(manifest projects.Manifest, service ServiceReport) []Finding {
	var findings []Finding
	if service.Health.Status == StatusUnavailable {
		findings = append(findings, Finding{Severity: "medium", ProjectID: manifest.ID, Type: "health_unavailable", Message: service.Health.Error})
	}
	if service.Capabilities.Status == StatusUnavailable {
		findings = append(findings, Finding{Severity: "low", ProjectID: manifest.ID, Type: "capabilities_unavailable", Message: service.Capabilities.Error})
	}
	if service.Metrics.Status == StatusUnavailable {
		findings = append(findings, Finding{Severity: "medium", ProjectID: manifest.ID, Type: "metrics_unavailable", Message: service.Metrics.Error})
	}
	switch manifest.ID {
	case "corerank":
		if service.Metrics.Values["match_timeout_total"] > 0 {
			findings = append(findings, Finding{Severity: "medium", ProjectID: manifest.ID, Type: "match_timeout", Message: "匹配超时指标大于 0，建议联查匹配队列、房间分配和 ArenaGate 调用链路。"})
		}
		if service.Metrics.Values["queued_tickets"] >= 20 {
			findings = append(findings, Finding{Severity: "medium", ProjectID: manifest.ID, Type: "match_queue_backlog", Message: "匹配队列积压达到 20 或以上，可能导致匹配等待时间上升。"})
		}
		if service.Metrics.Values["room_assignment_failures_total"] > 0 {
			findings = append(findings, Finding{Severity: "medium", ProjectID: manifest.ID, Type: "room_assignment_failure", Message: "房间分配失败指标大于 0，建议检查可用房间服容量。"})
		}
	case "arenagate":
		if service.Metrics.Values["errors_total"] > 0 {
			findings = append(findings, Finding{Severity: "medium", ProjectID: manifest.ID, Type: "gateway_errors", Message: "长连接网关错误计数大于 0，建议查看协议错误与客户端断连情况。"})
		}
		if service.Metrics.Values["core_errors_total"] > 0 {
			findings = append(findings, Finding{Severity: "high", ProjectID: manifest.ID, Type: "corerank_call_errors", Message: "ArenaGate 调用 CoreRank 失败计数大于 0，匹配链路可能受影响。"})
		}
	case "gameops":
		if service.Metrics.Values["errors_total"] > 0 {
			findings = append(findings, Finding{Severity: "medium", ProjectID: manifest.ID, Type: "gameops_errors", Message: "GameOps HTTP 错误计数大于 0，建议检查最近 GM 请求和审计日志。"})
		}
	}
	return findings
}

func serviceStatus(service ServiceReport) string {
	statuses := []string{service.Health.Status, service.Capabilities.Status, service.Metrics.Status}
	unavailable := 0
	degraded := false
	for _, status := range statuses {
		switch status {
		case StatusUnavailable:
			unavailable++
		case StatusDegraded:
			degraded = true
		}
	}
	if unavailable == len(statuses) {
		return StatusUnavailable
	}
	if unavailable > 0 || degraded || len(service.Findings) > 0 {
		return StatusDegraded
	}
	return StatusOK
}

func overallStatus(services []ServiceReport, risk *RiskReport, findings []Finding) string {
	if len(services) == 0 {
		return StatusUnavailable
	}
	allUnavailable := true
	for _, service := range services {
		if service.Status != StatusUnavailable {
			allUnavailable = false
		}
	}
	if allUnavailable {
		return StatusUnavailable
	}
	for _, service := range services {
		if service.Status != StatusOK {
			return StatusDegraded
		}
	}
	if risk != nil && risk.Status != StatusOK {
		return StatusDegraded
	}
	for _, finding := range findings {
		if finding.Severity == "high" || finding.Severity == "medium" {
			return StatusDegraded
		}
	}
	return StatusOK
}

func recommendations(report Report) []string {
	seen := map[string]bool{}
	var items []string
	add := func(value string) {
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		items = append(items, value)
	}
	if report.OverallStatus == StatusOK {
		add("当前只读诊断未发现明显异常，可继续保留常规 health / metrics 巡检。")
	}
	for _, finding := range report.Findings {
		switch finding.Type {
		case "health_unavailable", "metrics_unavailable", "capabilities_unavailable":
			add("先确认对应服务是否已启动，并检查 agent.yaml 中的本地端口是否与实际监听端口一致。")
		case "match_timeout", "match_queue_backlog", "room_assignment_failure":
			add("匹配超时排查优先看 CoreRank 匹配队列、房间分配失败和 ArenaGate 调用 CoreRank 的错误计数。")
		case "gateway_errors", "corerank_call_errors":
			add("长连接异常排查优先看 ArenaGate 协议错误、CoreRank 调用失败和客户端重连峰值。")
		case "gm_risk_detected", "gameops_errors":
			add("GM 风险排查优先复核 GameOps 审计日志、管理员来源 IP 和高额奖励/封禁操作审批记录。")
		}
	}
	if len(items) == 0 {
		add("诊断信息不足，建议先启动三项目服务后重新执行 gsa diagnose。")
	}
	return items
}

func parseMetricIdentity(raw string) (string, map[string]string) {
	idx := strings.Index(raw, "{")
	if idx < 0 {
		return raw, nil
	}
	name := raw[:idx]
	end := strings.LastIndex(raw, "}")
	if end < idx {
		return name, nil
	}
	labels := map[string]string{}
	for _, part := range strings.Split(raw[idx+1:end], ",") {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		labels[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"`)
	}
	return name, labels
}

func sumMetric(samples []MetricSample, name string) float64 {
	var total float64
	for _, sample := range samples {
		if sample.Name == name {
			total += sample.Value
		}
	}
	return total
}

func sumMetricWhere(samples []MetricSample, name string, label string, value string) float64 {
	var total float64
	for _, sample := range samples {
		if sample.Name == name && sample.Labels[label] == value {
			total += sample.Value
		}
	}
	return total
}

func endpointBase(values ...string) (string, error) {
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		parsed, err := url.Parse(value)
		if err != nil {
			continue
		}
		if parsed.Scheme == "" || parsed.Host == "" {
			continue
		}
		return parsed.Scheme + "://" + parsed.Host, nil
	}
	return "", errors.New("GameOps endpoint not configured")
}

func findProject(manifests []projects.Manifest, id string) (projects.Manifest, bool) {
	for _, manifest := range manifests {
		if strings.EqualFold(manifest.ID, id) {
			return manifest, true
		}
	}
	return projects.Manifest{}, false
}

func severityFromRiskLevel(level string) string {
	switch strings.ToLower(level) {
	case "high":
		return "high"
	case "medium":
		return "medium"
	default:
		return "low"
	}
}

func formatRiskLine(risk RiskReport) string {
	if risk.Status == StatusUnavailable {
		return "unavailable (" + risk.Error + ")"
	}
	return fmt.Sprintf("%s risk_level=%s score=%d audits=%d findings=%d", risk.Status, risk.RiskLevel, risk.Score, risk.AuditCount, risk.FindingCount)
}

func endpointLine(label string, check EndpointCheck) string {
	if check.Status == StatusOK {
		return fmt.Sprintf("%s: ok http=%d latency=%s", label, check.HTTPStatus, check.Latency.Round(time.Millisecond))
	}
	return fmt.Sprintf("%s: %s http=%d error=%s", label, check.Status, check.HTTPStatus, emptyDash(check.Error))
}

func metricsLine(label string, report MetricsReport) string {
	if report.Status == StatusOK {
		return fmt.Sprintf("%s: ok http=%d latency=%s", label, report.HTTPStatus, report.Latency.Round(time.Millisecond))
	}
	return fmt.Sprintf("%s: %s http=%d error=%s", label, report.Status, report.HTTPStatus, emptyDash(report.Error))
}

func jsonSummary(payload map[string]any) string {
	if status, ok := payload["status"]; ok {
		return "status=" + fmt.Sprint(status)
	}
	if project, ok := payload["project"]; ok {
		return "project=" + fmt.Sprint(project)
	}
	if id, ok := payload["id"]; ok {
		return "id=" + fmt.Sprint(id)
	}
	return "json ok"
}

func trimForSummary(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 160 {
		return value[:160] + "..."
	}
	return value
}

func emptyDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func startOfDay(t time.Time) time.Time {
	year, month, day := t.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, t.Location())
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

type credential struct {
	username string
	password string
}

func gameOpsCredentials() []credential {
	if user := os.Getenv("GSA_GAMEOPS_RISK_USER"); user != "" {
		return []credential{{
			username: user,
			password: os.Getenv("GSA_GAMEOPS_RISK_PASSWORD"),
		}}
	}
	return []credential{
		{
			username: getenv("GAMEOPS_AUDITOR_USER", "auditor"),
			password: getenv("GAMEOPS_AUDITOR_PASSWORD", "auditor_demo"),
		},
		{
			username: getenv("GAMEOPS_ADMIN_USER", "admin"),
			password: getenv("GAMEOPS_ADMIN_PASSWORD", "admin_demo"),
		},
	}
}

func getenv(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
