package diagnostics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jingjie2002/GameServerProjectAgent/internal/projects"
)

func TestDiagnoseServicesAndRisk(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/corerank/healthz", jsonHandler(map[string]any{"status": "ok"}))
	mux.HandleFunc("/corerank/capabilities", jsonHandler(map[string]any{"project": "CoreRank"}))
	mux.HandleFunc("/corerank/metrics", textHandler(`
corerank_matcher_ticket_events_total{match_mode="duel",status="timeout"} 2
corerank_matcher_queued_tickets{match_mode="duel"} 5
corerank_room_assignment_failures_total{match_mode="duel",reason="full"} 0
`))
	mux.HandleFunc("/arenagate/healthz", jsonHandler(map[string]any{"status": "ok"}))
	mux.HandleFunc("/arenagate/capabilities", jsonHandler(map[string]any{"project": "ArenaGate"}))
	mux.HandleFunc("/arenagate/metrics", textHandler(`
arenagate_active_sessions 8
arenagate_errors_total 0
arenagate_core_errors_total 1
`))
	mux.HandleFunc("/gameops/healthz", jsonHandler(map[string]any{"status": "ok"}))
	mux.HandleFunc("/gameops/capabilities", jsonHandler(map[string]any{"project": "GameOps"}))
	mux.HandleFunc("/gameops/metrics", textHandler(`
gameops_errors_total 0
gameops_risk_analyses_total 3
gameops_audit_writes_total 7
`))
	mux.HandleFunc("/api/admin/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected login method: %s", r.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "test-token"})
	})
	mux.HandleFunc("/api/risk/analyze", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("missing token: %s", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"risk_level":  "normal",
			"score":       0,
			"summary":     "ok",
			"audit_count": 1,
			"findings":    []any{},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewClient(time.Second)
	client.Now = func() time.Time {
		return time.Date(2026, 5, 20, 10, 0, 0, 0, time.Local)
	}
	report := client.Diagnose(context.Background(), "默认权限", testManifests(server.URL), Options{IncludeRisk: true})

	if report.OverallStatus != StatusDegraded {
		t.Fatalf("expected degraded because metrics contain findings, got %s", report.OverallStatus)
	}
	if len(report.Services) != 3 {
		t.Fatalf("expected 3 services, got %d", len(report.Services))
	}
	if report.Risk == nil || report.Risk.Status != StatusOK {
		t.Fatalf("expected ok risk report, got %#v", report.Risk)
	}
	if !containsFinding(report.Findings, "match_timeout") {
		t.Fatalf("expected match_timeout finding: %#v", report.Findings)
	}
	if !containsFinding(report.Findings, "corerank_call_errors") {
		t.Fatalf("expected corerank_call_errors finding: %#v", report.Findings)
	}
}

func TestDiagnoseUnavailableDoesNotFailWholeRun(t *testing.T) {
	client := NewClient(10 * time.Millisecond)
	report := client.Diagnose(context.Background(), "默认权限", []projects.Manifest{{
		ID:   "corerank",
		Name: "CoreRank",
		Health: projects.Endpoint{
			URL: "http://127.0.0.1:1/healthz",
		},
		Metrics: projects.Endpoint{
			URL: "http://127.0.0.1:1/metrics",
		},
		CapabilitiesEndpoint: projects.Endpoint{
			URL: "http://127.0.0.1:1/api/agent/capabilities",
		},
	}}, Options{})
	if report.OverallStatus != StatusUnavailable {
		t.Fatalf("expected unavailable, got %s", report.OverallStatus)
	}
	if len(report.Findings) == 0 {
		t.Fatalf("expected unavailable findings")
	}
}

func TestParsePrometheusMetrics(t *testing.T) {
	samples := ParsePrometheusMetrics(`
# comment
corerank_matcher_ticket_events_total{match_mode="duel",status="timeout"} 3
corerank_matcher_ticket_events_total{match_mode="duel",status="matched"} 9
arenagate_active_sessions 4
`)
	if got := sumMetricWhere(samples, "corerank_matcher_ticket_events_total", "status", "timeout"); got != 3 {
		t.Fatalf("expected timeout sum 3, got %v", got)
	}
	if got := sumMetric(samples, "arenagate_active_sessions"); got != 4 {
		t.Fatalf("expected active sessions 4, got %v", got)
	}
}

func TestFormatReport(t *testing.T) {
	report := Report{
		GeneratedAt:   time.Date(2026, 5, 20, 10, 0, 0, 0, time.Local),
		Mode:          "默认权限",
		OverallStatus: StatusOK,
		Services: []ServiceReport{{
			ProjectID: "demo",
			Name:      "Demo",
			Status:    StatusOK,
			Health: EndpointCheck{
				Status:     StatusOK,
				HTTPStatus: http.StatusOK,
			},
			Capabilities: EndpointCheck{
				Status:     StatusOK,
				HTTPStatus: http.StatusOK,
			},
			Metrics: MetricsReport{
				Status:     StatusOK,
				HTTPStatus: http.StatusOK,
			},
		}},
		Recommendations: []string{"保持巡检。"},
	}
	output := FormatReport(report)
	if !strings.Contains(output, "多服务诊断报告") || !strings.Contains(output, "总体状态：ok") {
		t.Fatalf("unexpected output:\n%s", output)
	}
}

func testManifests(baseURL string) []projects.Manifest {
	return []projects.Manifest{
		{
			ID:   "corerank",
			Name: "CoreRank",
			Health: projects.Endpoint{
				URL: baseURL + "/corerank/healthz",
			},
			Metrics: projects.Endpoint{
				URL: baseURL + "/corerank/metrics",
			},
			CapabilitiesEndpoint: projects.Endpoint{
				URL: baseURL + "/corerank/capabilities",
			},
		},
		{
			ID:   "arenagate",
			Name: "ArenaGate",
			Health: projects.Endpoint{
				URL: baseURL + "/arenagate/healthz",
			},
			Metrics: projects.Endpoint{
				URL: baseURL + "/arenagate/metrics",
			},
			CapabilitiesEndpoint: projects.Endpoint{
				URL: baseURL + "/arenagate/capabilities",
			},
		},
		{
			ID:   "gameops",
			Name: "GameOps",
			Health: projects.Endpoint{
				URL: baseURL + "/gameops/healthz",
			},
			Metrics: projects.Endpoint{
				URL: baseURL + "/gameops/metrics",
			},
			CapabilitiesEndpoint: projects.Endpoint{
				URL: baseURL + "/gameops/capabilities",
			},
		},
	}
}

func jsonHandler(payload map[string]any) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}
}

func textHandler(payload string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(payload))
	}
}

func containsFinding(findings []Finding, typ string) bool {
	for _, finding := range findings {
		if finding.Type == typ {
			return true
		}
	}
	return false
}
