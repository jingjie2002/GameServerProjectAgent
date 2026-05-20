package permissions

import "testing"

func TestParseMode(t *testing.T) {
	tests := map[string]Mode{
		"默认权限":        DefaultMode,
		"默认":          DefaultMode,
		"自动审查":        AutoReviewMode,
		"auto-review": AutoReviewMode,
		"完全访问权限":      FullAccessMode,
		"full":        FullAccessMode,
	}
	for input, want := range tests {
		got, err := ParseMode(input)
		if err != nil {
			t.Fatalf("ParseMode(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("ParseMode(%q)=%s, want %s", input, got, want)
		}
	}
}

func TestModeAllows(t *testing.T) {
	if DefaultMode.Allows(AutoReviewMode) {
		t.Fatal("default mode should not allow auto review tools")
	}
	if !AutoReviewMode.Allows(DefaultMode) || !AutoReviewMode.Allows(AutoReviewMode) {
		t.Fatal("auto review mode should allow default and auto review tools")
	}
	if !FullAccessMode.Allows(AutoReviewMode) {
		t.Fatal("full access should allow auto review tools")
	}
}

func TestModeAgentHeaderValue(t *testing.T) {
	if got := FullAccessMode.AgentHeaderValue(); got != "full-access" {
		t.Fatalf("unexpected full access header value: %s", got)
	}
	if got := AutoReviewMode.AgentHeaderValue(); got != "auto-review" {
		t.Fatalf("unexpected auto review header value: %s", got)
	}
}
