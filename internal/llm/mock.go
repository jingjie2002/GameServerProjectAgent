package llm

import (
	"regexp"
	"strconv"
	"strings"
)

type Mock struct{}

func (Mock) Plan(input string) string {
	text := strings.TrimSpace(input)
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(text, "发") && strings.Contains(text, "邮件") && extractPlayerID(text) != "":
		playerID := extractPlayerID(text)
		gold := extractRewardAmount(text, 100)
		days := extractDays(text, 7)
		return "/gm send-mail --player " + playerID + " --title 补偿邮件 --body compensation --gold " + strconv.Itoa(gold) + " --expires-days " + strconv.Itoa(days)
	case strings.Contains(text, "封禁") && extractPlayerID(text) != "":
		playerID := extractPlayerID(text)
		days := extractDays(text, 1)
		reason := afterReason(text)
		if reason == "" {
			reason = "gm_action"
		}
		return "/gm ban-player --player " + playerID + " --seconds " + strconv.Itoa(days*24*60*60) + " --reason " + reason
	case strings.Contains(text, "解封") && extractPlayerID(text) != "":
		return "/gm unban-player --player " + extractPlayerID(text)
	case strings.Contains(text, "冻结") && (strings.Contains(lower, "cdk") || strings.Contains(text, "批次")):
		if batchID := extractBatchID(text); batchID != "" {
			return "/gm freeze-cdk --batch " + batchID
		}
		if code := extractCDKCode(text); code != "" {
			return "/gm freeze-cdk --code " + code
		}
		return "/gm freeze-cdk"
	case strings.Contains(text, "GM") || strings.Contains(lower, "gm") || strings.Contains(text, "风险") || strings.Contains(text, "异常 GM"):
		return "/风险"
	case strings.Contains(text, "匹配超时") || strings.Contains(text, "长连接") || strings.Contains(text, "状态报告"):
		return "/诊断"
	case strings.Contains(text, "服务") && (strings.Contains(text, "正常") || strings.Contains(text, "健康") || strings.Contains(text, "状态")):
		return "/诊断"
	case strings.Contains(text, "列出") && strings.Contains(text, "项目"):
		return "/项目"
	case strings.Contains(text, "能力"):
		return "/能力"
	case strings.Contains(text, "自动审查"):
		return "/模式 自动审查"
	case strings.Contains(text, "测试") || strings.Contains(text, "审查"):
		return "/检查 all all"
	default:
		return "/帮助"
	}
}

var (
	playerPattern = regexp.MustCompile(`player_[0-9A-Za-z]+`)
	batchPattern  = regexp.MustCompile(`batch_[0-9A-Za-z]+`)
	cdkPattern    = regexp.MustCompile(`[A-Za-z]+-batch_[0-9A-Za-z]+-[0-9A-Za-z]+`)
	numberPattern = regexp.MustCompile(`\d+`)
)

func extractPlayerID(text string) string {
	return playerPattern.FindString(text)
}

func extractBatchID(text string) string {
	return batchPattern.FindString(text)
}

func extractCDKCode(text string) string {
	return cdkPattern.FindString(text)
}

func extractFirstNumber(text string, fallback int) int {
	raw := numberPattern.FindString(text)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func extractRewardAmount(text string, fallback int) int {
	for _, pattern := range []*regexp.Regexp{
		regexp.MustCompile(`(\d+)\s*(钻石|金币|gold|coin)`),
		regexp.MustCompile(`发\s*(\d+)`),
	} {
		matches := pattern.FindStringSubmatch(text)
		if len(matches) >= 2 {
			value, err := strconv.Atoi(matches[1])
			if err == nil {
				return value
			}
		}
	}
	return extractFirstNumber(strings.ReplaceAll(text, extractPlayerID(text), ""), fallback)
}

func extractDays(text string, fallback int) int {
	matches := regexp.MustCompile(`(\d+)\s*天`).FindStringSubmatch(text)
	if len(matches) != 2 {
		return fallback
	}
	value, err := strconv.Atoi(matches[1])
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func afterReason(text string) string {
	for _, marker := range []string{"原因是", "理由是", "原因：", "原因:"} {
		if _, value, ok := strings.Cut(text, marker); ok {
			value = strings.TrimSpace(value)
			value = strings.Trim(value, "，。,. ")
			return strings.ReplaceAll(value, " ", "_")
		}
	}
	return ""
}
