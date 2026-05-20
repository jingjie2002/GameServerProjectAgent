# GameServerProjectAgent

`GameServerProjectAgent` 是一个面向游戏服务端项目的本地管理 Agent 骨架，命令名为 `gsa`。

当前阶段是 V2.5：它可以读取 CoreRank、ArenaGate、GameOps 的 `agent.yaml`，列出项目，查看能力，切换权限模式，执行自动审查命令，读取 health / metrics / capabilities，调用 GameOps `/api/risk/analyze` 生成只读诊断报告，并在完全访问权限 + 人工确认后调用 GameOps 白名单接口执行 GM 写操作。

## 当前已实现

- `gsa` CLI 入口。
- 三种权限模式：`默认权限`、`自动审查`、`完全访问权限`。
- 读取三个项目的 `agent.yaml`。
- `/项目`：列出可管理项目。
- `/能力 [project_id]`：读取项目能力。
- `/模式 ...`：切换当前会话模式。
- `/检查 [all|project_id] [test|vet|smoke|all]`：按权限运行项目声明的命令。
- `/健康`：读取三项目 health、metrics、capabilities，服务未启动时返回 `unavailable`。
- `/诊断`：生成多服务统一诊断报告，覆盖匹配超时、长连接异常和 GameOps GM 风险。
- `/风险`：调用 GameOps `/api/risk/analyze`，只输出 GM 风险诊断摘要。
- `/gm preview-mail ...`：生成 GameOps 邮件预检。
- `/gm send-mail ...`：确认后发送带有效期奖励邮件。
- `/gm ban-player ...`：确认后封禁玩家。
- `/gm unban-player ...`：确认后解封玩家。
- `/gm freeze-cdk ...`：确认后冻结单个 CDK 或 CDK 批次。
- `/审计`：查看本地 JSONL 审计日志。
- `mock-llm`：把少量自然语言映射到斜杠命令。

## 本阶段明确不做

- 不接真实 DeepSeek API。
- 不绕过 GameOps 直接改数据库。
- 不做无确认批量发奖、封禁或冻结。
- 不做 V3 的 SSH、自动部署、自动重启、通用平台和多租户 RBAC。

## 快速验证

```powershell
$env:GSA_WORKSPACE = (Resolve-Path ..).Path
$env:GOCACHE = Join-Path (Get-Location) ".gocache"
go test ./...
go vet ./...
go run ./cmd/gsa projects
go run ./cmd/gsa capabilities gameops
go run ./cmd/gsa health
go run ./cmd/gsa diagnose
go run ./cmd/gsa risk
go run ./cmd/gsa --mode 自动审查 check all vet
go run ./cmd/gsa gm preview-mail --player player_1001 --title Stage5Mail --body demo --gold 100 --expires-days 7
go run ./cmd/gsa --mode 完全访问权限 gm send-mail --player player_1001 --title Stage5Mail --body demo --gold 100 --expires-days 7
python scripts\demo_gm_flow.py
```

如果三项目服务没有启动，`health`、`diagnose`、`risk` 会正常结束并在报告中标记 `unavailable`，不会把本地诊断流程直接打断。

`gm send-mail`、`gm ban-player`、`gm unban-player`、`gm freeze-cdk` 在缺少确认参数时只输出确认卡片，不会执行写操作。真正执行需要：

```powershell
--mode 完全访问权限 --confirm confirm_xxx --confirmed-by demo_user
```

封禁、解封、冻结等高风险操作还需要：

```powershell
--risk-ack
```

## 交互模式

```powershell
go run ./cmd/gsa
```

示例：

```text
/项目
/能力 corerank
/模式 自动审查
/检查 all vet
/诊断
/风险
/gm preview-mail --player player_1001 --title Stage5Mail --body demo --gold 100 --expires-days 7
/gm send-mail --player player_1001 --title Stage5Mail --body demo --gold 100 --expires-days 7
/审计
/退出
```

## 环境变量

| 变量 | 说明 |
|---|---|
| `GSA_WORKSPACE` | 工作区根目录，默认自动从当前目录或父目录寻找三项目 |
| `GSA_PROJECT_MANIFESTS` | 覆盖项目 manifest 列表，Windows 使用分号分隔 |
| `GSA_AUDIT_LOG` | 覆盖审计日志路径 |
| `GSA_GAMEOPS_USER` | 覆盖 GM 写操作使用的 GameOps 登录用户 |
| `GSA_GAMEOPS_PASSWORD` | 覆盖 GM 写操作使用的 GameOps 登录密码 |
| `GSA_GAMEOPS_ADMIN_USER` | 覆盖 GM 写操作管理员用户 |
| `GSA_GAMEOPS_ADMIN_PASSWORD` | 覆盖 GM 写操作管理员密码 |
| `GSA_GAMEOPS_RISK_USER` | 覆盖 GameOps 风险分析登录用户 |
| `GSA_GAMEOPS_RISK_PASSWORD` | 覆盖 GameOps 风险分析登录密码 |

GM 写操作默认使用管理员账号 `admin/admin_demo`；风险分析默认优先尝试审计员账号 `auditor/auditor_demo`，再尝试管理员账号 `admin/admin_demo`。

## 文档

- [设计说明](docs/design.md)
- [演示指南](docs/demo-guide.md)
## Setup 向导

从阶段 6 开始，`gsa` 支持安装后的本机初始化向导：

```powershell
.\bin\gsa.exe setup
```

如果直接运行 `.\bin\gsa.exe` 且尚未检测到 `.gsa/config.yaml`，也会自动进入初始化向导。

向导会引导配置：

- 服务端工作区目录。
- 模型供应商：DeepSeek / OpenAI / OpenAI-compatible / 暂不配置。
- 模型 API Base URL。
- 模型名称。
- API Key 环境变量名。
- 默认三项目 `agent.yaml` 注册。

生成文件：

```text
.gsa/config.yaml
.gsa/secrets.env    # 仅当向导中输入 API Key 时生成
```

`.gsa/` 已被 `.gitignore` 忽略，本机配置和密钥不会提交到 Git。

当前阶段只完成配置向导和本地保存，还没有接入真实模型请求；后续阶段会让 `gsa ask` 使用这里配置的模型。
