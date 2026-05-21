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
- 模型供应商：默认可暂不配置，后续再选择 DeepSeek / OpenAI / OpenAI-compatible。
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
## 仓库导入

阶段 7 增加了安全的仓库导入命令：

```powershell
.\bin\gsa.exe import https://github.com/example/game-service.git
```

第一版导入行为：

- 如果目标目录不存在：执行 `git clone`。
- 如果目标目录已是 Git 仓库：执行 `git pull --ff-only`。
- 如果仓库根目录存在合法 `agent.yaml`：自动注册到 `.gsa/config.yaml`。
- 如果没有 `agent.yaml`：只生成导入报告，不自动注册、不改源码、不部署。

导入报告会写入：

```text
.gsa/imports/<repo-name>-import-report.md
```

可选指定目标目录：

```powershell
.\bin\gsa.exe import https://github.com/example/game-service.git --dest F:\server-workspace\game-service
```

## 导入向导

阶段 9 增加了不依赖大模型的模块导入向导：

```powershell
.\bin\gsa.exe onboard
```

也可以直接带上仓库地址：

```powershell
.\bin\gsa.exe onboard https://github.com/example/game-service.git
```

导入向导会按顺序执行：

- `git clone` 或 `git pull --ff-only`。
- 如果仓库已有合法 `agent.yaml`，直接注册到 `.gsa/config.yaml`。
- 如果没有合法 `agent.yaml`，询问是否执行 `scan`。
- 扫描后展示 `agent.generated.yaml` 预览。
- 用户确认后才注册 generated 配置。

自动确认模式可用于本地 smoke 或 CI：

```powershell
.\bin\gsa.exe onboard https://github.com/example/game-service.git --dest F:\server-workspace\game-service --yes
```

`--yes` 只会自动确认扫描和 generated 注册，不会部署、不会启动服务、不会执行数据库迁移，也不会改源码。

## 仓库扫描

阶段 8 增加了只读仓库扫描和配置草稿生成：

```powershell
.\bin\gsa.exe scan F:\server-workspace\game-service
```

第一版扫描行为：

- 优先识别 Go 项目：`go.mod`、`cmd/*/main.go`、根目录 `main.go`。
- 检测 `Dockerfile`、`docker-compose.yml` / `compose.yml`。
- 检测 `.env.example`。
- 从 README 中提取常见启动命令线索。
- 从源码和配置中初步识别端口、Redis/MySQL/Postgres 等依赖、`/healthz` / `/health` / `/metrics`。
- 生成 `agent.generated.yaml`。
- 生成 `deploy.generated.yaml`。
- 生成扫描报告。

生成文件只是草稿，需要人工检查后再决定是否改名为正式配置或进入部署流程。当前阶段不会自动改源码、不会自动启动服务、不会执行数据库迁移。

## Generated 配置注册

阶段 8.5 增加了人工确认注册命令，用于把已经审查过的 `agent.generated.yaml` 纳入 `gsa` 的本地管理配置：

```powershell
.\bin\gsa.exe register-generated F:\server-workspace\game-service
```

不带 `--confirm` 时只会预览：

- 解析 `agent.generated.yaml`。
- 展示项目 ID、名称、root、health 和 capabilities 地址。
- 不写入 `.gsa/config.yaml`。
- 提示确认命令。

确认无误后再执行：

```powershell
.\bin\gsa.exe register-generated F:\server-workspace\game-service --confirm
```

确认后只会把 manifest 路径写入 `.gsa/config.yaml`，不会修改被扫描仓库源码，不会自动部署，也不会启动服务。注册完成后可以运行：

```powershell
.\bin\gsa.exe projects
.\bin\gsa.exe capabilities sample-service
```

## 部署试运行

阶段 10 增加了本地部署试运行和服务生命周期管理的安全底座：

```powershell
.\bin\gsa.exe deploy plan sample-service
.\bin\gsa.exe deploy status sample-service
.\bin\gsa.exe deploy logs sample-service
```

`plan` 会读取项目目录中的 `deploy.generated.yaml`，展示 build/run 命令、端口、依赖和健康检查路径。它只预览，不会启动服务。

启动和停止属于高风险动作，必须同时满足：

- 使用 `--mode 完全访问权限`。
- 显式传入 `--confirm`。

示例：

```powershell
.\bin\gsa.exe --mode 完全访问权限 deploy start sample-service --confirm
.\bin\gsa.exe deploy status sample-service
.\bin\gsa.exe deploy logs sample-service --tail 80
.\bin\gsa.exe --mode 完全访问权限 deploy stop sample-service --confirm
```

运行状态会写入：

```text
.gsa/services/<project-id>.json
.gsa/services/<project-id>.log
```

当前阶段只做本地进程试运行，不会远程部署服务器，不会开放公网端口，不会执行数据库迁移。启动前应先人工检查 `deploy.generated.yaml` 的 run 命令。

### Windows live smoke

为验证 Windows 下长驻服务的进程树清理、端口释放和日志读取，可以运行：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\deploy_live_smoke.ps1
```

脚本会在 `tmp\deploy-live-smoke-*` 下创建临时 Go HTTP 服务，并使用临时 `GSA_HOME` / `GSA_CONFIG` 完成：

- `deploy plan`
- `deploy status`
- `deploy start`
- `/healthz` 轮询
- `deploy logs`
- `deploy stop`
- 进程退出检查
- health 不可访问检查
- 成功后自动删除临时目录

如果脚本失败，会保留临时目录并打印路径，便于排查。脚本清理前会校验路径必须位于仓库 `tmp\` 下，不会删除真实项目目录或正式 `.gsa/`。
