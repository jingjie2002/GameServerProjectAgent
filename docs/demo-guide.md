# GameServerProjectAgent V2.5 演示指南

## 1. 查看项目

```powershell
$env:GSA_WORKSPACE = (Resolve-Path ..).Path
go run ./cmd/gsa projects
```

预期能列出：

- `corerank`
- `arenagate`
- `gameops`

## 2. 查看能力

```powershell
go run ./cmd/gsa capabilities gameops
```

预期能看到 `gm_risk_analyze` 和 `gameops_analyze_gm_risk`。

## 3. 权限阻断

```powershell
go run ./cmd/gsa check all vet
```

默认权限下应被阻断，因为 `go vet` 需要 `自动审查`。

## 4. 自动审查

```powershell
go run ./cmd/gsa --mode 自动审查 check all vet
```

预期对 CoreRank、ArenaGate、GameOps 依次执行 `go vet ./...`。

## 5. 多服务健康诊断

```powershell
go run ./cmd/gsa health
```

预期输出三项目 health、capabilities、metrics 状态。三项目服务未启动时，报告中会显示 `unavailable`，命令本身仍应正常结束。

## 6. 多服务综合诊断

```powershell
go run ./cmd/gsa diagnose
```

预期生成统一诊断报告，覆盖 CoreRank 匹配超时、ArenaGate 长连接异常、GameOps 指标和 GM 风险分析。

## 7. GameOps GM 风险诊断

```powershell
go run ./cmd/gsa risk
```

预期调用 GameOps `/api/risk/analyze`。如 GameOps 未启动，应输出风险分析 `unavailable` 及连接失败原因。

## 8. 自然语言验收

```powershell
go run ./cmd/gsa ask "检查三个服务是否正常"
go run ./cmd/gsa ask "为什么匹配超时变多"
go run ./cmd/gsa ask "检查最近有没有异常 GM 操作"
go run ./cmd/gsa ask "生成一份今天的游戏服务端状态报告"
```

## 9. 交互模式

```powershell
go run ./cmd/gsa
```

然后输入：

```text
/项目
/模式 自动审查
/检查 all vet
/诊断
/风险
/审计
/退出
```

## 10. GM 邮件预检

需要先启动 GameOps，或运行第 14 节的一键 demo。

```powershell
go run ./cmd/gsa gm preview-mail --player player_1001 --title Stage5Mail --body demo --gold 100 --expires-days 7
```

预期输出 `邮件预检结果`，包含 `allowed`、`risk_level`、`expires_at`。

## 11. GM 写操作确认卡片

```powershell
go run ./cmd/gsa --mode 完全访问权限 gm send-mail --player player_1001 --title Stage5Mail --body demo --gold 100 --expires-days 7
```

未带 `--confirm` 和 `--confirmed-by` 时，预期只输出 `确认卡片`，不会执行写操作。

## 12. 确认后发送邮件

```powershell
go run ./cmd/gsa --mode 完全访问权限 gm send-mail --player player_1001 --title Stage5Mail --body demo --gold 100 --expires-days 7 --confirm confirm_demo_mail --confirmed-by demo_user
```

预期输出 `已发送邮件`，GameOps 审计日志中应记录 `agent_session_id` 和 `confirmation_id=confirm_demo_mail`。

## 13. 高风险二次确认

封禁、解封、冻结需要额外 `--risk-ack`：

```powershell
go run ./cmd/gsa --mode 完全访问权限 gm ban-player --player player_1003 --seconds 259200 --reason abuse --confirm confirm_demo_ban --confirmed-by demo_user --risk-ack
go run ./cmd/gsa --mode 完全访问权限 gm unban-player --player player_1003 --confirm confirm_demo_unban --confirmed-by demo_user --risk-ack
go run ./cmd/gsa --mode 完全访问权限 gm freeze-cdk --batch batch_0001 --confirm confirm_demo_freeze --confirmed-by demo_user --risk-ack
```

缺少 `--risk-ack` 时，预期继续输出确认卡片，不执行写操作。

## 14. 一键 GM 演示

```powershell
python scripts\demo_gm_flow.py
```

脚本会临时启动 GameOps、写入临时 manifest，然后验证邮件预检、确认卡片、确认后发邮件、封禁/解封、冻结 CDK、自然语言生成确认卡片，以及 Agent 审计与 GameOps 审计关联。
