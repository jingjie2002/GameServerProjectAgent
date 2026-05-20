package permissions

import (
	"fmt"
	"strings"
)

type Mode string

const (
	DefaultMode    Mode = "默认权限"
	AutoReviewMode Mode = "自动审查"
	FullAccessMode Mode = "完全访问权限"
)

func ParseMode(input string) (Mode, error) {
	normalized := strings.ToLower(strings.TrimSpace(input))
	normalized = strings.ReplaceAll(normalized, " ", "")
	switch normalized {
	case "", "默认", "默认权限", "default", "readonly", "read-only":
		return DefaultMode, nil
	case "自动审查", "自动审核", "auto", "autoreview", "auto-review":
		return AutoReviewMode, nil
	case "完全访问", "完全访问权限", "full", "fullaccess", "full-access":
		return FullAccessMode, nil
	default:
		return "", fmt.Errorf("unknown mode %q", input)
	}
}

func (m Mode) Allows(required Mode) bool {
	return m.rank() >= required.rank()
}

func (m Mode) String() string {
	if m == "" {
		return string(DefaultMode)
	}
	return string(m)
}

func (m Mode) AgentHeaderValue() string {
	switch m {
	case FullAccessMode:
		return "full-access"
	case AutoReviewMode:
		return "auto-review"
	default:
		return "default"
	}
}

func (m Mode) rank() int {
	switch m {
	case FullAccessMode:
		return 3
	case AutoReviewMode:
		return 2
	default:
		return 1
	}
}
