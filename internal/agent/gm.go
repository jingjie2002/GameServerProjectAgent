package agent

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jingjie2002/GameServerProjectAgent/internal/audit"
	"github.com/jingjie2002/GameServerProjectAgent/internal/permissions"
	"github.com/jingjie2002/GameServerProjectAgent/internal/tools"
)

type gmFlags map[string]string

func (s *Session) handleGM(ctx context.Context, args []string) string {
	if len(args) == 0 {
		return s.gmHelp()
	}
	action := normalizeGMAction(args[0])
	flags, err := parseGMFlags(args[1:])
	if err != nil {
		return "GM 命令参数错误：" + err.Error()
	}
	gameops, ok := s.findProject("gameops")
	if !ok {
		return "找不到 GameOps 项目。"
	}
	client, err := tools.NewGameOpsClient(gameops)
	if err != nil {
		return "GameOps GM 工具初始化失败：" + err.Error()
	}

	switch action {
	case "preview-mail":
		return s.previewMail(ctx, client, flags)
	case "send-mail":
		return s.sendMail(ctx, client, flags)
	case "ban-player":
		return s.banPlayer(ctx, client, flags)
	case "unban-player":
		return s.unbanPlayer(ctx, client, flags)
	case "freeze-cdk":
		return s.freezeCDK(ctx, client, flags)
	default:
		return "未知 GM 操作：" + args[0] + "\n" + s.gmHelp()
	}
}

func (s *Session) previewMail(ctx context.Context, client tools.GameOpsClient, flags gmFlags) string {
	draft, err := mailDraftFromFlags(flags)
	if err != nil {
		return "邮件预检参数错误：" + err.Error()
	}
	preview, err := client.PreviewMail(ctx, draft)
	if err != nil {
		s.appendAudit("gameops_preview_mail", "gameops", "failed", err.Error())
		return "邮件预检失败：" + err.Error()
	}
	s.appendAudit("gameops_preview_mail", "gameops", "ok", draft.PlayerID)
	return formatMailPreview(preview)
}

func (s *Session) sendMail(ctx context.Context, client tools.GameOpsClient, flags gmFlags) string {
	draft, err := mailDraftFromFlags(flags)
	if err != nil {
		return "发邮件参数错误：" + err.Error()
	}
	if !s.Mode.Allows(permissions.FullAccessMode) {
		s.appendAudit("gameops_send_mail", "gameops", "blocked", "requires full access")
		return "当前模式不能执行 GM 写操作：需要切换到 完全访问权限。"
	}
	preview, err := client.PreviewMail(ctx, draft)
	if err != nil {
		s.appendAudit("gameops_send_mail", "gameops", "failed", "preview failed: "+err.Error())
		return "发邮件前预检失败：" + err.Error()
	}
	if !preview.Allowed {
		s.appendAudit("gameops_send_mail", "gameops", "blocked", strings.Join(preview.Violations, "; "))
		return "发邮件被预检阻断：\n" + formatMailPreview(preview)
	}
	highRisk := isHighRiskMail(preview)
	if card, ok := s.confirmationCard("gameops_send_mail", "发送奖励邮件", draft.PlayerID, formatDraftDetail(draft), highRisk, flags); !ok {
		return card
	}
	result, err := client.SendMail(ctx, draft, s.agentAudit(flags))
	if err != nil {
		s.appendAudit("gameops_send_mail", "gameops", "failed", err.Error())
		return "发送邮件失败：" + err.Error()
	}
	s.appendAudit("gameops_send_mail", "gameops", "ok", "target="+draft.PlayerID+" confirmation_id="+flags.confirmationID())
	return fmt.Sprintf("已发送邮件：mail_id=%s player_id=%s expires_at=%s confirmation_id=%s",
		mapString(result, "mail_id"), mapString(result, "player_id"), mapNumberString(result, "expires_at"), flags.confirmationID())
}

func (s *Session) banPlayer(ctx context.Context, client tools.GameOpsClient, flags gmFlags) string {
	playerID := flags.first("player", "player-id", "player_id")
	if playerID == "" {
		return "封禁参数错误：缺少 --player。"
	}
	seconds, err := flags.int64("seconds", "banned-seconds", "banned_seconds")
	if err != nil || seconds <= 0 {
		return "封禁参数错误：缺少有效的 --seconds。"
	}
	reason := flags.first("reason")
	if reason == "" {
		reason = "gm_action"
	}
	if !s.Mode.Allows(permissions.FullAccessMode) {
		s.appendAudit("gameops_ban_player", "gameops", "blocked", "requires full access")
		return "当前模式不能执行 GM 写操作：需要切换到 完全访问权限。"
	}
	detail := fmt.Sprintf("player_id=%s seconds=%d reason=%s", playerID, seconds, reason)
	if card, ok := s.confirmationCard("gameops_ban_player", "封禁玩家", playerID, detail, true, flags); !ok {
		return card
	}
	result, err := client.BanPlayer(ctx, playerID, reason, seconds, s.agentAudit(flags))
	if err != nil {
		s.appendAudit("gameops_ban_player", "gameops", "failed", err.Error())
		return "封禁玩家失败：" + err.Error()
	}
	s.appendAudit("gameops_ban_player", "gameops", "ok", "target="+playerID+" confirmation_id="+flags.confirmationID())
	return fmt.Sprintf("已封禁玩家：player_id=%s status=%s banned_until=%s confirmation_id=%s",
		mapString(result, "player_id"), mapString(result, "status"), mapNumberString(result, "banned_until"), flags.confirmationID())
}

func (s *Session) unbanPlayer(ctx context.Context, client tools.GameOpsClient, flags gmFlags) string {
	playerID := flags.first("player", "player-id", "player_id")
	if playerID == "" {
		return "解封参数错误：缺少 --player。"
	}
	if !s.Mode.Allows(permissions.FullAccessMode) {
		s.appendAudit("gameops_unban_player", "gameops", "blocked", "requires full access")
		return "当前模式不能执行 GM 写操作：需要切换到 完全访问权限。"
	}
	if card, ok := s.confirmationCard("gameops_unban_player", "解封玩家", playerID, "player_id="+playerID, true, flags); !ok {
		return card
	}
	result, err := client.UnbanPlayer(ctx, playerID, s.agentAudit(flags))
	if err != nil {
		s.appendAudit("gameops_unban_player", "gameops", "failed", err.Error())
		return "解封玩家失败：" + err.Error()
	}
	s.appendAudit("gameops_unban_player", "gameops", "ok", "target="+playerID+" confirmation_id="+flags.confirmationID())
	return fmt.Sprintf("已解封玩家：player_id=%s status=%s confirmation_id=%s",
		mapString(result, "player_id"), mapString(result, "status"), flags.confirmationID())
}

func (s *Session) freezeCDK(ctx context.Context, client tools.GameOpsClient, flags gmFlags) string {
	code := flags.first("code", "cdk")
	batchID := flags.first("batch", "batch-id", "batch_id")
	if code == "" && batchID == "" {
		return "冻结 CDK 参数错误：需要 --code 或 --batch。"
	}
	if code != "" && batchID != "" {
		return "冻结 CDK 参数错误：--code 和 --batch 只能选一个。"
	}
	if !s.Mode.Allows(permissions.FullAccessMode) {
		s.appendAudit("gameops_freeze_cdk", "gameops", "blocked", "requires full access")
		return "当前模式不能执行 GM 写操作：需要切换到 完全访问权限。"
	}
	target := code
	title := "冻结 CDK"
	detail := "code=" + code
	if batchID != "" {
		target = batchID
		title = "冻结 CDK 批次"
		detail = "batch_id=" + batchID
	}
	if card, ok := s.confirmationCard("gameops_freeze_cdk", title, target, detail, true, flags); !ok {
		return card
	}
	var result map[string]any
	var err error
	if batchID != "" {
		result, err = client.FreezeCDKBatch(ctx, batchID, s.agentAudit(flags))
	} else {
		result, err = client.FreezeCDK(ctx, code, s.agentAudit(flags))
	}
	if err != nil {
		s.appendAudit("gameops_freeze_cdk", "gameops", "failed", err.Error())
		return "冻结 CDK 失败：" + err.Error()
	}
	s.appendAudit("gameops_freeze_cdk", "gameops", "ok", "target="+target+" confirmation_id="+flags.confirmationID())
	if batchID != "" {
		return fmt.Sprintf("已冻结 CDK 批次：batch_id=%s status=%s confirmation_id=%s",
			mapString(result, "batch_id"), mapString(result, "status"), flags.confirmationID())
	}
	return fmt.Sprintf("已冻结 CDK：code=%s status=%s confirmation_id=%s",
		mapString(result, "code"), mapString(result, "status"), flags.confirmationID())
}

func (s *Session) confirmationCard(action string, title string, target string, detail string, highRisk bool, flags gmFlags) (string, bool) {
	confirmationID := flags.confirmationID()
	confirmedBy := flags.first("confirmed-by", "confirmed_by", "by")
	riskAck := flags.bool("risk-ack", "risk_ack")
	if confirmationID != "" && confirmedBy != "" && (!highRisk || riskAck) {
		return "", true
	}
	if confirmationID == "" {
		confirmationID = s.newConfirmationID(action)
	}
	s.appendAudit(action, "gameops", "pending_confirmation",
		fmt.Sprintf("target=%s confirmation_id=%s high_risk=%t detail=%s", target, confirmationID, highRisk, detail))
	var lines []string
	lines = append(lines, "确认卡片")
	lines = append(lines, "操作："+title)
	lines = append(lines, "目标："+target)
	lines = append(lines, "参数："+detail)
	lines = append(lines, "当前模式："+s.Mode.String())
	lines = append(lines, "确认ID："+confirmationID)
	lines = append(lines, "要求：完全访问权限 + --confirm "+confirmationID+" + --confirmed-by <操作人>")
	if highRisk {
		lines = append(lines, "高风险二次确认：需要额外添加 --risk-ack")
	}
	lines = append(lines, "状态：未执行，等待人工确认。")
	return strings.Join(lines, "\n"), false
}

func (s *Session) agentAudit(flags gmFlags) tools.AgentAudit {
	confirmedAt, _ := flags.int64("confirmed-at", "confirmed_at")
	if confirmedAt == 0 {
		confirmedAt = s.now().UnixMilli()
	}
	return tools.AgentAudit{
		SessionID:      s.AgentSessionID,
		Mode:           s.Mode.AgentHeaderValue(),
		ConfirmationID: flags.confirmationID(),
		ConfirmedBy:    flags.first("confirmed-by", "confirmed_by", "by"),
		ConfirmedAt:    confirmedAt,
	}
}

func (s *Session) newConfirmationID(action string) string {
	clean := strings.TrimPrefix(action, "gameops_")
	return fmt.Sprintf("confirm_%s_%d", clean, s.now().UnixMilli())
}

func mailDraftFromFlags(flags gmFlags) (tools.MailDraft, error) {
	playerID := flags.first("player", "player-id", "player_id")
	if playerID == "" {
		return tools.MailDraft{}, fmt.Errorf("缺少 --player")
	}
	gold, err := flags.int64("gold", "coins")
	if err != nil {
		return tools.MailDraft{}, fmt.Errorf("--gold 必须是数字")
	}
	expires, err := flags.int64("expires", "expires-in", "expires_in_seconds")
	if err != nil {
		return tools.MailDraft{}, fmt.Errorf("--expires 必须是秒数")
	}
	if expires == 0 {
		days, err := flags.int64("expires-days", "days")
		if err != nil {
			return tools.MailDraft{}, fmt.Errorf("--expires-days 必须是数字")
		}
		if days > 0 {
			expires = days * 24 * 60 * 60
		}
	}
	title := flags.first("title")
	if title == "" {
		title = "GM补偿邮件"
	}
	body := flags.first("body")
	if body == "" {
		body = "GM compensation"
	}
	return tools.MailDraft{
		PlayerID:         playerID,
		Title:            title,
		Body:             body,
		Gold:             gold,
		Items:            splitItems(flags.first("items", "item")),
		ExpiresInSeconds: expires,
	}, nil
}

func parseGMFlags(args []string) (gmFlags, error) {
	flags := gmFlags{}
	for i := 0; i < len(args); i++ {
		raw := args[i]
		if !strings.HasPrefix(raw, "--") {
			return nil, fmt.Errorf("无法识别参数 %q", raw)
		}
		key := strings.TrimPrefix(raw, "--")
		value := "true"
		if strings.Contains(key, "=") {
			parts := strings.SplitN(key, "=", 2)
			key, value = parts[0], parts[1]
		} else if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
			value = args[i+1]
			i++
		}
		flags[normalizeFlagKey(key)] = value
	}
	return flags, nil
}

func normalizeGMAction(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "preview", "preview-mail", "mail-preview", "预检邮件", "邮件预检":
		return "preview-mail"
	case "send", "send-mail", "mail", "发邮件", "发送邮件":
		return "send-mail"
	case "ban", "ban-player", "封禁":
		return "ban-player"
	case "unban", "unban-player", "解封":
		return "unban-player"
	case "freeze", "freeze-cdk", "冻结":
		return "freeze-cdk"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func normalizeFlagKey(value string) string {
	return strings.ReplaceAll(strings.TrimSpace(strings.ToLower(value)), "_", "-")
}

func (f gmFlags) first(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(f[normalizeFlagKey(key)]); value != "" {
			return value
		}
	}
	return ""
}

func (f gmFlags) int64(keys ...string) (int64, error) {
	value := f.first(keys...)
	if value == "" {
		return 0, nil
	}
	return strconv.ParseInt(value, 10, 64)
}

func (f gmFlags) bool(keys ...string) bool {
	value := strings.ToLower(f.first(keys...))
	return value == "true" || value == "1" || value == "yes" || value == "y" || value == "确认"
}

func (f gmFlags) confirmationID() string {
	return f.first("confirm", "confirmation-id", "confirmation_id")
}

func splitItems(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		if item := strings.TrimSpace(part); item != "" {
			items = append(items, item)
		}
	}
	return items
}

func isHighRiskMail(preview tools.MailPreview) bool {
	return preview.RiskLevel != "" && preview.RiskLevel != "low" || preview.Gold >= 1000 || len(preview.Items) > 5
}

func formatMailPreview(preview tools.MailPreview) string {
	lines := []string{
		"邮件预检结果",
		fmt.Sprintf("allowed=%t risk_level=%s target_count=%d gold=%d expires_in_seconds=%d expires_at=%d",
			preview.Allowed, preview.RiskLevel, preview.TargetCount, preview.Gold, preview.ExpiresInSeconds, preview.ExpiresAt),
	}
	if len(preview.Items) > 0 {
		lines = append(lines, "items="+strings.Join(preview.Items, ","))
	}
	if len(preview.Violations) > 0 {
		lines = append(lines, "violations="+strings.Join(preview.Violations, "; "))
	}
	if len(preview.Warnings) > 0 {
		lines = append(lines, "warnings="+strings.Join(preview.Warnings, "; "))
	}
	return strings.Join(lines, "\n")
}

func formatDraftDetail(draft tools.MailDraft) string {
	return fmt.Sprintf("player_id=%s gold=%d items=%s expires_in_seconds=%d title=%s",
		draft.PlayerID, draft.Gold, strings.Join(draft.Items, ","), draft.ExpiresInSeconds, draft.Title)
}

func mapString(payload map[string]any, key string) string {
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func mapNumberString(payload map[string]any, key string) string {
	value, ok := payload[key]
	if !ok || value == nil {
		return "0"
	}
	switch typed := value.(type) {
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	default:
		return fmt.Sprint(value)
	}
}

func (s *Session) appendAudit(action string, projectID string, status string, detail string) {
	if s.Audit == nil {
		return
	}
	_ = s.Audit.Append(audit.Event{
		Mode:      s.Mode.String(),
		Action:    action,
		ProjectID: projectID,
		Status:    status,
		Detail:    detail,
	})
}

func (s *Session) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Session) gmHelp() string {
	return strings.Join([]string{
		"GM 命令：",
		"/gm preview-mail --player player_1001 --title 补偿 --body demo --gold 100 --expires-days 7",
		"/gm send-mail --player player_1001 --title 补偿 --body demo --gold 100 --expires-days 7 --confirm confirm_xxx --confirmed-by demo_user",
		"/gm ban-player --player player_1003 --seconds 259200 --reason 恶意刷榜 --confirm confirm_xxx --confirmed-by demo_user --risk-ack",
		"/gm unban-player --player player_1003 --confirm confirm_xxx --confirmed-by demo_user --risk-ack",
		"/gm freeze-cdk --code CODE --confirm confirm_xxx --confirmed-by demo_user --risk-ack",
		"/gm freeze-cdk --batch batch_0001 --confirm confirm_xxx --confirmed-by demo_user --risk-ack",
	}, "\n")
}
