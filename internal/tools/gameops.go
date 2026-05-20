package tools

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
	"strings"
	"time"

	"github.com/jingjie2002/GameServerProjectAgent/internal/projects"
)

type AgentAudit struct {
	SessionID      string
	Mode           string
	ConfirmationID string
	ConfirmedBy    string
	ConfirmedAt    int64
}

type MailDraft struct {
	PlayerID         string
	Title            string
	Body             string
	Gold             int64
	Items            []string
	ExpiresInSeconds int64
}

type MailPreview struct {
	Allowed          bool     `json:"allowed"`
	RiskLevel        string   `json:"risk_level"`
	TargetCount      int      `json:"target_count"`
	Gold             int64    `json:"gold"`
	Items            []string `json:"items"`
	ExpiresInSeconds int64    `json:"expires_in_seconds"`
	ExpiresAt        int64    `json:"expires_at"`
	Violations       []string `json:"violations"`
	Warnings         []string `json:"warnings"`
}

type GameOpsClient struct {
	BaseURL            string
	Username           string
	Password           string
	HTTPClient         *http.Client
	PreviewMailPath    string
	SendMailPath       string
	BanPlayerPath      string
	UnbanPlayerPath    string
	FreezeCDKPath      string
	FreezeCDKBatchPath string
}

func NewGameOpsClient(manifest projects.Manifest) (GameOpsClient, error) {
	baseURL, err := gameOpsBaseURL(manifest)
	if err != nil {
		return GameOpsClient{}, err
	}
	username, password := gameOpsCredential()
	return GameOpsClient{
		BaseURL:            baseURL,
		Username:           username,
		Password:           password,
		HTTPClient:         &http.Client{Timeout: 5 * time.Second},
		PreviewMailPath:    toolPath(manifest.AgentTools, "gameops_preview_mail", "/api/mails/preview"),
		SendMailPath:       toolPath(manifest.AgentTools, "gameops_send_mail", "/api/mails"),
		BanPlayerPath:      toolPath(manifest.AgentTools, "gameops_ban_player", "/api/players/{player_id}/ban"),
		UnbanPlayerPath:    toolPath(manifest.AgentTools, "gameops_unban_player", "/api/players/{player_id}/unban"),
		FreezeCDKPath:      toolPath(manifest.AgentTools, "gameops_freeze_cdk", "/api/cdk/{code}/freeze"),
		FreezeCDKBatchPath: toolPath(manifest.AgentTools, "gameops_freeze_cdk_batch", "/api/cdk/batches/{batch_id}/freeze"),
	}, nil
}

func (c GameOpsClient) PreviewMail(ctx context.Context, draft MailDraft) (MailPreview, error) {
	var preview MailPreview
	err := c.doJSON(ctx, http.MethodPost, c.pathOrDefault(c.PreviewMailPath, "/api/mails/preview"), mailPayload(draft, AgentAudit{}), AgentAudit{}, &preview)
	return preview, err
}

func (c GameOpsClient) SendMail(ctx context.Context, draft MailDraft, agent AgentAudit) (map[string]any, error) {
	var payload map[string]any
	err := c.doJSON(ctx, http.MethodPost, c.pathOrDefault(c.SendMailPath, "/api/mails"), mailPayload(draft, agent), agent, &payload)
	return payload, err
}

func (c GameOpsClient) BanPlayer(ctx context.Context, playerID string, reason string, seconds int64, agent AgentAudit) (map[string]any, error) {
	var payload map[string]any
	body := map[string]any{
		"reason":         reason,
		"banned_seconds": seconds,
	}
	path := replacePathValue(c.pathOrDefault(c.BanPlayerPath, "/api/players/{player_id}/ban"), "player_id", playerID)
	err := c.doJSON(ctx, http.MethodPost, path, body, agent, &payload)
	return payload, err
}

func (c GameOpsClient) UnbanPlayer(ctx context.Context, playerID string, agent AgentAudit) (map[string]any, error) {
	var payload map[string]any
	path := replacePathValue(c.pathOrDefault(c.UnbanPlayerPath, "/api/players/{player_id}/unban"), "player_id", playerID)
	err := c.doJSON(ctx, http.MethodPost, path, map[string]any{}, agent, &payload)
	return payload, err
}

func (c GameOpsClient) FreezeCDK(ctx context.Context, code string, agent AgentAudit) (map[string]any, error) {
	var payload map[string]any
	path := replacePathValue(c.pathOrDefault(c.FreezeCDKPath, "/api/cdk/{code}/freeze"), "code", code)
	err := c.doJSON(ctx, http.MethodPost, path, map[string]any{}, agent, &payload)
	return payload, err
}

func (c GameOpsClient) FreezeCDKBatch(ctx context.Context, batchID string, agent AgentAudit) (map[string]any, error) {
	var payload map[string]any
	path := replacePathValue(c.pathOrDefault(c.FreezeCDKBatchPath, "/api/cdk/batches/{batch_id}/freeze"), "batch_id", batchID)
	err := c.doJSON(ctx, http.MethodPost, path, map[string]any{}, agent, &payload)
	return payload, err
}

func (c GameOpsClient) doJSON(ctx context.Context, method string, path string, body any, agent AgentAudit, target any) error {
	token, err := c.login(ctx)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	applyAgentAuditHeaders(req, agent)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("gameops %s %s failed: http %d: %s", method, path, resp.StatusCode, trimHTTPBody(string(responseBody)))
	}
	if target == nil {
		return nil
	}
	if err := json.Unmarshal(responseBody, target); err != nil {
		return fmt.Errorf("decode gameops response: %w", err)
	}
	return nil
}

func (c GameOpsClient) login(ctx context.Context) (string, error) {
	if c.Username == "" || c.Password == "" {
		return "", errors.New("GameOps admin credential is not configured")
	}
	body, _ := json.Marshal(map[string]string{
		"username": c.Username,
		"password": c.Password,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/admin/login", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("gameops login failed: http %d: %s", resp.StatusCode, trimHTTPBody(string(payload)))
	}
	var decoded struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return "", err
	}
	if decoded.Token == "" {
		return "", errors.New("gameops login response missing token")
	}
	return decoded.Token, nil
}

func (c GameOpsClient) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func mailPayload(draft MailDraft, agent AgentAudit) map[string]any {
	payload := map[string]any{
		"player_id":          draft.PlayerID,
		"title":              draft.Title,
		"body":               draft.Body,
		"gold":               draft.Gold,
		"items":              draft.Items,
		"expires_in_seconds": draft.ExpiresInSeconds,
	}
	if agent.SessionID != "" {
		payload["agent_session_id"] = agent.SessionID
	}
	if agent.Mode != "" {
		payload["agent_mode"] = agent.Mode
	}
	if agent.ConfirmationID != "" {
		payload["confirmation_id"] = agent.ConfirmationID
	}
	if agent.ConfirmedBy != "" {
		payload["confirmed_by"] = agent.ConfirmedBy
	}
	if agent.ConfirmedAt > 0 {
		payload["confirmed_at"] = agent.ConfirmedAt
	}
	return payload
}

func applyAgentAuditHeaders(req *http.Request, agent AgentAudit) {
	if agent.SessionID != "" {
		req.Header.Set("X-Agent-Session-ID", agent.SessionID)
	}
	if agent.Mode != "" {
		req.Header.Set("X-Agent-Mode", agent.Mode)
	}
	if agent.ConfirmationID != "" {
		req.Header.Set("X-Agent-Confirmation-ID", agent.ConfirmationID)
	}
	if agent.ConfirmedBy != "" {
		req.Header.Set("X-Agent-Confirmed-By", agent.ConfirmedBy)
	}
	if agent.ConfirmedAt > 0 {
		req.Header.Set("X-Agent-Confirmed-At", fmt.Sprint(agent.ConfirmedAt))
	}
}

func gameOpsBaseURL(manifest projects.Manifest) (string, error) {
	for _, raw := range []string{manifest.CapabilitiesEndpoint.URL, manifest.Health.URL, manifest.Health.LegacyURL} {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			continue
		}
		return parsed.Scheme + "://" + parsed.Host, nil
	}
	return "", errors.New("GameOps endpoint not configured")
}

func toolPath(agentTools map[string]string, name string, fallback string) string {
	raw := strings.TrimSpace(agentTools[name])
	if raw == "" {
		return fallback
	}
	parts := strings.Fields(raw)
	if len(parts) == 0 {
		return fallback
	}
	path := parts[len(parts)-1]
	if !strings.HasPrefix(path, "/") {
		return fallback
	}
	return path
}

func (c GameOpsClient) pathOrDefault(path string, fallback string) string {
	if strings.TrimSpace(path) == "" {
		return fallback
	}
	return path
}

func replacePathValue(path string, name string, value string) string {
	return strings.ReplaceAll(path, "{"+name+"}", url.PathEscape(value))
}

func gameOpsCredential() (string, string) {
	if user := os.Getenv("GSA_GAMEOPS_USER"); user != "" {
		return user, os.Getenv("GSA_GAMEOPS_PASSWORD")
	}
	if user := os.Getenv("GSA_GAMEOPS_ADMIN_USER"); user != "" {
		return user, os.Getenv("GSA_GAMEOPS_ADMIN_PASSWORD")
	}
	return getenv("GAMEOPS_ADMIN_USER", "admin"), getenv("GAMEOPS_ADMIN_PASSWORD", "admin_demo")
}

func getenv(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func trimHTTPBody(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 180 {
		return value[:180] + "..."
	}
	return value
}
