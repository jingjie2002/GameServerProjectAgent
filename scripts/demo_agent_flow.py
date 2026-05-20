import subprocess
import os
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
WORKSPACE = ROOT.parent


def run(*args: str) -> None:
    env = os.environ.copy()
    env["GOCACHE"] = str(ROOT / ".gocache")
    env["GSA_WORKSPACE"] = str(WORKSPACE)
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


def main() -> None:
    run("projects")
    run("capabilities", "gameops")
    run("health")
    run("diagnose")
    run("risk")
    run("ask", "检查三个服务是否正常")
    run("ask", "检查最近有没有异常 GM 操作")
    run("--mode", "自动审查", "check", "all", "vet")
    run("audit")


if __name__ == "__main__":
    main()
