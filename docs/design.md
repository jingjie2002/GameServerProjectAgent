# GameServerProjectAgent V2.5 设计说明

## 定位

V2.5 在 V2 多服务只读诊断上补齐 GameOps GM 受控写操作。目标是让 `gsa` 能稳定读取三项目 health / metrics / capabilities，调用 GameOps `/api/risk/analyze` 生成统一诊断报告，并在完全访问权限和人工确认后调用 GameOps 白名单接口。

## 模块

| 模块 | 责任 |
|---|---|
| `cmd/gsa` | CLI 入口和交互循环 |
| `internal/projects` | 读取三项目 `agent.yaml` |
| `internal/permissions` | 三种权限模式和权限判断 |
| `internal/tools` | 执行项目声明的命令 |
| `internal/tools/gameops.go` | 调用 GameOps 白名单 GM 接口，注入 Agent 审计字段 |
| `internal/diagnostics` | 读取 health、metrics、capabilities 和 GameOps 风险分析，生成诊断报告 |
| `internal/audit` | JSONL 审计日志 |
| `internal/agent` | 会话命令、自然语言到命令的最小映射 |
| `internal/llm` | 当前只有 `mock-llm` |

## 权限边界

- `默认权限`：可以列项目、看能力、看审计、读取 health / metrics、生成诊断报告、调用 GameOps 风险分析、生成邮件预检；不能运行测试，不能执行 GM 写操作。
- `自动审查`：可以运行 `go test ./...`、`go vet ./...`、smoke 等低风险命令。
- `完全访问权限`：向下兼容自动审查能力；GM 写操作仍必须带确认 ID、确认人和 Agent 审计字段。

## GM 写操作确认

`gsa gm` 当前支持：

- `preview-mail`：调用 `POST /api/mails/preview`，不写入业务数据。
- `send-mail`：调用 `POST /api/mails`，执行前自动预检。
- `ban-player`：调用 `POST /api/players/{player_id}/ban`。
- `unban-player`：调用 `POST /api/players/{player_id}/unban`。
- `freeze-cdk`：调用 `POST /api/cdk/{code}/freeze` 或 `POST /api/cdk/batches/{batch_id}/freeze`。

写操作必须满足：

```text
当前模式 = 完全访问权限
--confirm <confirmation_id>
--confirmed-by <user>
```

封禁、解封、冻结和高风险邮件还必须额外带：

```text
--risk-ack
```

如果缺少确认参数，Agent 只输出确认卡片，不调用 GameOps 写接口。

## Agent 审计关联

写操作会同时写两类审计：

- Agent 本地 JSONL 审计：记录 `action`、`project_id`、`status`、`detail`。
- GameOps 审计：通过 HTTP Header / JSON 字段传入 `agent_session_id`、`agent_mode`、`confirmation_id`、`confirmed_by`、`confirmed_at`。

## 诊断模型

`internal/diagnostics` 生成统一 `Report`：

- `Services`：每个项目的 health、capabilities、metrics 状态。
- `Risk`：GameOps GM 风险分析结果。
- `Findings`：统一异常发现，例如匹配超时、长连接错误、GameOps 风险。
- `Recommendations`：面向排查的下一步建议。

服务未启动、端口不通或接口返回非 2xx 时，诊断报告会标记 `unavailable` 或 `degraded`，但命令本身仍正常返回，方便用户在本地逐项补启动。

## 指标解释

- CoreRank：读取匹配超时、排队票据、房间分配失败等指标，用于回答“为什么匹配超时变多”。
- ArenaGate：读取活跃连接、网关错误、CoreRank 调用失败等指标，用于回答“长连接是否异常”。
- GameOps：读取 HTTP 错误、审计写入、风险分析次数，并调用 `/api/risk/analyze` 用于回答“最近有没有异常 GM 操作”。

## 审计

审计日志默认写入：

```text
GameServerProjectAgent/.gsa/audit.log
```

格式是 JSONL，便于后续替换为 SQLite。
