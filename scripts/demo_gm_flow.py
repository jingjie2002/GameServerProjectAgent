import json
import os
import subprocess
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
WORKSPACE = ROOT.parent
GAMEOPS = WORKSPACE / "GameOps"
ADDR = "127.0.0.1:18190"
BASE_URL = f"http://{ADDR}"


def request(method, path, token=None, payload=None, expected=(200, 201)):
    data = None
    headers = {}
    if payload is not None:
        data = json.dumps(payload).encode("utf-8")
        headers["Content-Type"] = "application/json"
    if token:
        headers["Authorization"] = "Bearer " + token
    req = urllib.request.Request(BASE_URL + path, data=data, method=method, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=3) as resp:
            body = resp.read().decode("utf-8")
            if resp.status not in expected:
                raise RuntimeError(f"{method} {path} expected {expected}, got {resp.status}: {body}")
            return json.loads(body) if body else None
    except urllib.error.HTTPError as exc:
        body = exc.read().decode("utf-8", errors="replace")
        if exc.code in expected:
            return json.loads(body) if body else None
        raise RuntimeError(f"{method} {path} failed {exc.code}: {body}") from exc


def wait_ready():
    for _ in range(40):
        try:
            request("GET", "/healthz")
            return
        except Exception:
            time.sleep(0.25)
    raise RuntimeError("GameOps did not become ready")


def run_gsa(*args: str, manifests: Path) -> str:
    env = os.environ.copy()
    env["GOCACHE"] = str(ROOT / ".gocache")
    env["GSA_WORKSPACE"] = str(WORKSPACE)
    env["GSA_PROJECT_MANIFESTS"] = str(manifests)
    env["GSA_AUDIT_LOG"] = str(ROOT / "tmp" / "demo-gm-audit.log")
    env["GSA_GAMEOPS_USER"] = "admin"
    env["GSA_GAMEOPS_PASSWORD"] = "admin_demo"
    completed = subprocess.run(
        ["go", "run", "./cmd/gsa", *args],
        cwd=ROOT,
        env=env,
        text=True,
        encoding="utf-8",
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        check=False,
    )
    print("$ gsa", " ".join(args))
    print(completed.stdout.strip())
    if completed.returncode != 0:
        raise SystemExit(completed.returncode)
    return completed.stdout


def write_manifest(path: Path):
    path.write_text(
        f"""id: gameops
name: GameOps
description: 游戏运营后台与 GM 管理服务
type: go-service
root: {GAMEOPS.as_posix()}

health:
  url: {BASE_URL}/healthz

metrics:
  url: {BASE_URL}/metrics
  format: prometheus

capabilities_endpoint:
  url: {BASE_URL}/api/agent/capabilities

commands:
  test:
    command: go test ./...
    mode: 自动审查

capabilities:
  - gm_risk_analyze
  - mail_preview
  - cdk_freeze
  - agent_audit_fields

agent_tools:
  gameops_preview_mail: POST /api/mails/preview
  gameops_send_mail: POST /api/mails
  gameops_ban_player: POST /api/players/{{player_id}}/ban
  gameops_unban_player: POST /api/players/{{player_id}}/unban
  gameops_freeze_cdk: POST /api/cdk/{{code}}/freeze
""",
        encoding="utf-8",
    )


def main():
    tmp = ROOT / "tmp"
    tmp.mkdir(exist_ok=True)
    exe = tmp / ("gameops-agent-demo.exe" if os.name == "nt" else "gameops-agent-demo")
    manifest = tmp / "gameops-agent-demo.yaml"
    write_manifest(manifest)

    env = os.environ.copy()
    env["GOCACHE"] = env.get("GOCACHE", str(GAMEOPS / ".gocache"))
    env["GAMEOPS_ADDR"] = ADDR

    subprocess.run(["go", "build", "-o", str(exe), "./cmd/server"], cwd=GAMEOPS, env=env, check=True)
    proc = subprocess.Popen([str(exe)], cwd=GAMEOPS, env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        wait_ready()

        login = request("POST", "/api/admin/login", payload={"username": "admin", "password": "admin_demo"})
        token = login["token"]
        print(f"admin_login role={login['role']}")
        players = request("POST", "/api/players/seed", token=token, payload={})
        print(f"seed_players count={len(players)}")

        batch = request("POST", "/api/cdk/batches", token=token, payload={
            "name": "agent freeze demo",
            "gold": 100,
            "items": ["ticket"],
            "count": 2,
            "max_uses_per_code": 1,
            "expires_in_seconds": 3600,
        })
        code = batch["codes"][0]
        print(f"create_cdk_batch {batch['batch_id']} code={code}")

        run_gsa("gm", "preview-mail", "--player", "player_1001", "--title", "Stage5Mail", "--body", "demo", "--gold", "100", "--expires-days", "7", manifests=manifest)
        card = run_gsa("--mode", "完全访问权限", "gm", "send-mail", "--player", "player_1001", "--title", "Stage5Mail", "--body", "demo", "--gold", "100", "--expires-days", "7", manifests=manifest)
        if "确认卡片" not in card:
            raise RuntimeError("send mail did not require confirmation")
        run_gsa("--mode", "完全访问权限", "gm", "send-mail", "--player", "player_1001", "--title", "Stage5Mail", "--body", "demo", "--gold", "100", "--expires-days", "7", "--confirm", "confirm_demo_mail", "--confirmed-by", "demo_user", manifests=manifest)
        run_gsa("--mode", "完全访问权限", "gm", "ban-player", "--player", "player_1003", "--seconds", "259200", "--reason", "abuse", "--confirm", "confirm_demo_ban", "--confirmed-by", "demo_user", "--risk-ack", manifests=manifest)
        run_gsa("--mode", "完全访问权限", "gm", "unban-player", "--player", "player_1003", "--confirm", "confirm_demo_unban", "--confirmed-by", "demo_user", "--risk-ack", manifests=manifest)
        run_gsa("--mode", "完全访问权限", "gm", "freeze-cdk", "--code", code, "--confirm", "confirm_demo_freeze_code", "--confirmed-by", "demo_user", "--risk-ack", manifests=manifest)
        run_gsa("--mode", "完全访问权限", "gm", "freeze-cdk", "--batch", batch["batch_id"], "--confirm", "confirm_demo_freeze_batch", "--confirmed-by", "demo_user", "--risk-ack", manifests=manifest)
        nl = run_gsa("--mode", "完全访问权限", "ask", "给 player_1001 发 100 钻石补偿邮件，有效期 7 天", manifests=manifest)
        if "确认卡片" not in nl:
            raise RuntimeError("natural language mail request did not produce confirmation card")

        audits = request("GET", "/api/audit-logs", token=token)
        confirmations = {item.get("confirmation_id") for item in audits}
        required = {
            "confirm_demo_mail",
            "confirm_demo_ban",
            "confirm_demo_unban",
            "confirm_demo_freeze_code",
            "confirm_demo_freeze_batch",
        }
        missing = sorted(required - confirmations)
        if missing:
            raise RuntimeError("missing GameOps confirmation audit fields: " + ", ".join(missing))
        if not any(item.get("agent_session_id", "").startswith("gsa-") for item in audits):
            raise RuntimeError("missing gsa agent_session_id in GameOps audits")
        print("agent_and_gameops_audit linked")
        print("GameServerProjectAgent GM demo completed")
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:
        print(str(exc), file=sys.stderr)
        raise
