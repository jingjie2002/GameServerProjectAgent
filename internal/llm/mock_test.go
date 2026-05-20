package llm

import "testing"

func TestMockPlansDiagnostics(t *testing.T) {
	mock := Mock{}
	if got := mock.Plan("检查三个服务是否正常"); got != "/诊断" {
		t.Fatalf("expected /诊断, got %q", got)
	}
	if got := mock.Plan("检查最近有没有异常 GM 操作"); got != "/风险" {
		t.Fatalf("expected /风险, got %q", got)
	}
	if got := mock.Plan("为什么匹配超时变多"); got != "/诊断" {
		t.Fatalf("expected /诊断, got %q", got)
	}
}

func TestMockPlansGMOperations(t *testing.T) {
	mock := Mock{}
	tests := map[string]string{
		"给 player_1001 发 100 钻石补偿邮件，有效期 7 天": "/gm send-mail --player player_1001 --title 补偿邮件 --body compensation --gold 100 --expires-days 7",
		"封禁 player_1003 3 天，原因是恶意刷榜":         "/gm ban-player --player player_1003 --seconds 259200 --reason 恶意刷榜",
		"解封 player_1003":         "/gm unban-player --player player_1003",
		"冻结异常 CDK 批次 batch_0001": "/gm freeze-cdk --batch batch_0001",
	}
	for input, want := range tests {
		if got := mock.Plan(input); got != want {
			t.Fatalf("Plan(%q)=%q, want %q", input, got, want)
		}
	}
}
