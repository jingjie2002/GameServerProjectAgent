package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jingjie2002/GameServerProjectAgent/internal/audit"
	"github.com/jingjie2002/GameServerProjectAgent/internal/permissions"
	"github.com/jingjie2002/GameServerProjectAgent/internal/projects"
)

func TestGMPreviewAndSendMailFlow(t *testing.T) {
	var receivedConfirmation string
	var receivedSession string
	server := newGameOpsGMTestServer(t, func(r *http.Request) {
		receivedConfirmation = r.Header.Get("X-Agent-Confirmation-ID")
		receivedSession = r.Header.Get("X-Agent-Session-ID")
	})
	defer server.Close()

	session := newGMTestSession(server.URL, permissions.DefaultMode)
	got := session.Handle(context.Background(), `/gm preview-mail --player player_1001 --title "Test Mail" --body demo --gold 100 --expires-days 7`)
	if !strings.Contains(got, "邮件预检结果") || !strings.Contains(got, "allowed=true") {
		t.Fatalf("expected preview result, got:\n%s", got)
	}

	got = session.Handle(context.Background(), `/gm send-mail --player player_1001 --title "Test Mail" --body demo --gold 100 --expires-days 7`)
	if !strings.Contains(got, "需要切换到 完全访问权限") {
		t.Fatalf("expected full access block, got:\n%s", got)
	}

	session.Mode = permissions.FullAccessMode
	got = session.Handle(context.Background(), `/gm send-mail --player player_1001 --title "Test Mail" --body demo --gold 100 --expires-days 7`)
	if !strings.Contains(got, "确认卡片") || !strings.Contains(got, "未执行") {
		t.Fatalf("expected confirmation card, got:\n%s", got)
	}

	got = session.Handle(context.Background(), `/gm send-mail --player player_1001 --title "Test Mail" --body demo --gold 100 --expires-days 7 --confirm confirm_test_mail --confirmed-by tester`)
	if !strings.Contains(got, "已发送邮件") || !strings.Contains(got, "confirm_test_mail") {
		t.Fatalf("expected sent mail, got:\n%s", got)
	}
	if receivedConfirmation != "confirm_test_mail" {
		t.Fatalf("expected confirmation header, got %q", receivedConfirmation)
	}
	if receivedSession != "sess_test" {
		t.Fatalf("expected agent session header, got %q", receivedSession)
	}
}

func TestGMHighRiskRequiresRiskAck(t *testing.T) {
	server := newGameOpsGMTestServer(t, nil)
	defer server.Close()

	session := newGMTestSession(server.URL, permissions.FullAccessMode)
	got := session.Handle(context.Background(), `/gm ban-player --player player_1003 --seconds 259200 --reason abuse --confirm confirm_ban --confirmed-by tester`)
	if !strings.Contains(got, "高风险二次确认") {
		t.Fatalf("expected high risk ack card, got:\n%s", got)
	}

	got = session.Handle(context.Background(), `/gm ban-player --player player_1003 --seconds 259200 --reason abuse --confirm confirm_ban --confirmed-by tester --risk-ack`)
	if !strings.Contains(got, "已封禁玩家") {
		t.Fatalf("expected ban result, got:\n%s", got)
	}
}

func TestGMFreezeBatch(t *testing.T) {
	server := newGameOpsGMTestServer(t, nil)
	defer server.Close()

	session := newGMTestSession(server.URL, permissions.FullAccessMode)
	got := session.Handle(context.Background(), `/gm freeze-cdk --batch batch_0001 --confirm confirm_freeze --confirmed-by tester --risk-ack`)
	if !strings.Contains(got, "已冻结 CDK 批次") || !strings.Contains(got, "batch_0001") {
		t.Fatalf("expected freeze batch result, got:\n%s", got)
	}
}

func TestGMSendMailDefaultModeSkipsPreviewCall(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	session := newGMTestSession(server.URL, permissions.DefaultMode)
	got := session.Handle(context.Background(), `/gm send-mail --player player_1001 --title "Test Mail" --body demo --gold 100 --expires-days 7`)
	if called {
		t.Fatalf("default mode should block send-mail before calling GameOps")
	}
	if !strings.Contains(got, "完全访问权限") {
		t.Fatalf("expected full access block, got:\n%s", got)
	}
}

func TestGMConfirmationCardWritesPendingAudit(t *testing.T) {
	server := newGameOpsGMTestServer(t, nil)
	defer server.Close()

	store := audit.NewStore(filepath.Join(t.TempDir(), "audit.jsonl"))
	session := newGMTestSession(server.URL, permissions.FullAccessMode)
	session.Audit = store
	got := session.Handle(context.Background(), `/gm send-mail --player player_1001 --title "Test Mail" --body demo --gold 100 --expires-days 7`)
	if !strings.Contains(got, "确认卡片") {
		t.Fatalf("expected confirmation card, got:\n%s", got)
	}

	events, err := store.List(10)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	for _, event := range events {
		if event.Action == "gameops_send_mail" && event.Status == "pending_confirmation" && strings.Contains(event.Detail, "confirmation_id=") {
			return
		}
	}
	t.Fatalf("expected pending confirmation audit, got %#v", events)
}

func TestSplitCommandLineKeepsQuotedValues(t *testing.T) {
	fields, err := splitCommandLine(`/gm send-mail --title "Test Mail" --body "hello world"`)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(fields, "|"); got != `/gm|send-mail|--title|Test Mail|--body|hello world` {
		t.Fatalf("unexpected fields: %s", got)
	}
}

func newGMTestSession(baseURL string, mode permissions.Mode) *Session {
	session := NewSession(mode, []projects.Manifest{{
		ID:   "gameops",
		Name: "GameOps",
		CapabilitiesEndpoint: projects.Endpoint{
			URL: baseURL + "/api/agent/capabilities",
		},
	}}, nil, nil)
	session.AgentSessionID = "sess_test"
	session.Now = func() time.Time {
		return time.Date(2026, 5, 20, 16, 0, 0, 0, time.UTC)
	}
	return session
}

func newGameOpsGMTestServer(t *testing.T, onWrite func(*http.Request)) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/admin/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected login method: %s", r.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "token"})
	})
	mux.HandleFunc("/api/mails/preview", func(w http.ResponseWriter, r *http.Request) {
		requireBearer(t, r)
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"allowed":            true,
			"risk_level":         "low",
			"target_count":       1,
			"gold":               req["gold"],
			"items":              req["items"],
			"expires_in_seconds": req["expires_in_seconds"],
			"expires_at":         1770000000000,
			"violations":         []string{},
			"warnings":           []string{},
		})
	})
	mux.HandleFunc("/api/mails", func(w http.ResponseWriter, r *http.Request) {
		requireBearer(t, r)
		if onWrite != nil {
			onWrite(r)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"mail_id":    "mail_0001",
			"player_id":  "player_1001",
			"expires_at": 1770000000000,
		})
	})
	mux.HandleFunc("/api/players/player_1003/ban", func(w http.ResponseWriter, r *http.Request) {
		requireBearer(t, r)
		if onWrite != nil {
			onWrite(r)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"player_id":    "player_1003",
			"status":       "banned",
			"banned_until": 1770000000000,
		})
	})
	mux.HandleFunc("/api/players/player_1003/unban", func(w http.ResponseWriter, r *http.Request) {
		requireBearer(t, r)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"player_id": "player_1003",
			"status":    "normal",
		})
	})
	mux.HandleFunc("/api/cdk/batches/batch_0001/freeze", func(w http.ResponseWriter, r *http.Request) {
		requireBearer(t, r)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"batch_id": "batch_0001",
			"status":   "frozen",
		})
	})
	mux.HandleFunc("/api/cdk/GO-batch_0001-0001/freeze", func(w http.ResponseWriter, r *http.Request) {
		requireBearer(t, r)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":   "GO-batch_0001-0001",
			"status": "frozen",
		})
	})
	return httptest.NewServer(mux)
}

func requireBearer(t *testing.T, r *http.Request) {
	t.Helper()
	if got := r.Header.Get("Authorization"); got != "Bearer token" {
		t.Fatalf("unexpected authorization: %s", got)
	}
}
