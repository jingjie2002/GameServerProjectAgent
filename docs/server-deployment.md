# 轻量级服务器部署与管理约定

本文记录 GameServerProjectAgent 在轻量级 Linux 服务器上的推荐落地方式。当前阶段先完成目录约定和只读展示入口，不直接执行 systemd / Docker Compose 部署。

## 推荐目录

```text
/opt/gsa/                    # Agent 本体、配置、状态和日志
/opt/gsa/bin/gsa             # gsa 可执行文件
/opt/gsa/.gsa/config.yaml    # 本机配置
/opt/gsa/.gsa/services/      # 服务状态和日志
/srv/gsa/workspace/          # 被管理的服务端模块
/srv/gsa/workspace/CoreRank
/srv/gsa/workspace/ArenaGate
/srv/gsa/workspace/GameOps
```

推荐环境变量：

```bash
export GSA_HOME=/opt/gsa
export GSA_WORKSPACE=/srv/gsa/workspace
export GSA_CONFIG=/opt/gsa/.gsa/config.yaml
```

Windows 本地验证时不需要使用这些 Linux 路径，直接使用当前工作区和 `.gsa/config.yaml` 即可。

## 管理流程

第一轮服务器部署建议按这个顺序走：

```bash
gsa setup
gsa onboard <repo-url>
gsa server plan
gsa deploy plan <project_id>
gsa dashboard --host 127.0.0.1 --port 18088
```

确认 `deploy.generated.yaml` 没问题后，再手动执行：

```bash
gsa --mode 完全访问权限 deploy start <project_id> --confirm
gsa deploy status <project_id>
gsa deploy logs <project_id>
gsa --mode 完全访问权限 deploy stop <project_id> --confirm
```

## Web 状态面板

阶段 11 增加只读 Web 状态面板：

```bash
gsa dashboard --host 127.0.0.1 --port 18088
```

默认只监听 `127.0.0.1`，用于本机或服务器内网查看。页面提供：

- 已注册模块列表。
- deploy 运行状态、PID、端口和日志入口。
- health / capabilities / metrics 只读检查状态。
- `/api/status` JSON 状态接口。
- `/api/logs/<project_id>?tail=120` 日志尾部接口。

当前页面不提供启动、停止、重启按钮；这些动作仍必须通过 CLI，并满足 `--mode 完全访问权限` 和 `--confirm`。

## systemd 与 Docker Compose 的位置

当前阶段先不直接生成或执行 systemd / Docker Compose。

后续推荐分两层接入：

```text
gsa deploy plan       # 继续负责预览和解释
gsa dashboard         # 继续负责展示状态
systemd/compose       # 作为后续可选的长期运行后端
```

选择建议：

- 单个 Go 服务或少量服务：优先考虑 systemd。
- 同时需要数据库、Redis、Nginx 或多容器隔离：优先考虑 Docker Compose。

不管使用哪种方式，Agent 都应先展示计划，再要求用户确认，不自动开放公网端口，不自动执行数据库迁移。
