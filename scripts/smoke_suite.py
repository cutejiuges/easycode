#!/usr/bin/env python3
"""顺序执行原生 macOS 关键交互，构建一次并核对每个脚本的完成回执。"""
import argparse
import json
import os
from pathlib import Path
import platform
import signal
import subprocess
import sys
import tempfile
import time


def run_process(command, root, env, log, timeout):
    with log.open("w") as output:
        process = subprocess.Popen(command, cwd=root, env=env, stdout=output, stderr=subprocess.STDOUT,
                                   start_new_session=True)
        try:
            return process.wait(timeout=timeout)
        finally:
            if process.poll() is None:
                # Python harness 的 finally 负责自身 PTY、假服务与 helper；先让它正常展开。
                process.send_signal(signal.SIGINT)
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    pass
            # 仅清理本次 start_new_session 创建的组；父进程退出不代表同组后代已退出。
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            process.wait()


def plan(binary, output):
    app = str(binary / "app.test")
    cases = [
        ("tasks", "tasks-smoke.py", ["--output-dir", str(output / "tasks")], 2),
        ("completion", "completion-smoke.py", [], 2),
        ("picker", "session-picker-smoke.py", [], 10),
        ("titles", "session-titles-smoke.py", ["--output", str(output / "titles")], 2),
        ("plugins", "plugins-smoke.py", [], 7),
        ("question", "user-question-smoke.py", [], 4),
        ("files", "file-mentions-smoke.py", [], 2),
        ("effort", "effort-smoke.py", [], 4),
        ("reasoning", "reasoning-smoke.py", [], 2),
        ("reasoning-controls", "reasoning-controls-smoke.py", [], 4),
        ("markdown", "markdown-transcript-smoke.py", [], 2),
        ("input-rewrite", "input-rewrite-smoke.py", [app, "--output-dir", str(output / "input-rewrite")], 2),
        ("pre-compact", "pre-compact-smoke.py", [app], 4),
        ("post-compact", "pre-compact-smoke.py", [app, "--post"], 2),
        ("stop", "stop-hook-smoke.py", [app, "--output-dir", str(output / "stop")], 2),
        ("plan", "plan-mode-smoke.py", ["--output", str(output / "plan")], 2),
        ("session-start", "session-start-smoke.py", ["--output", str(output / "session-start")], 2),
    ]
    for event in ("PreToolUse", "PostToolUse", "PostToolUseFailure"):
        cases.append((event, "smoke-hooks.py", [app, "--event", event, "--output-dir", str(output / event)], 2))
    return cases


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=Path(".test-results/smoke"))
    parser.add_argument("--go", default="go")
    args = parser.parse_args()
    if platform.system() != "Darwin":
        parser.error("native smoke requires macOS; no scenarios were verified")
    root = Path(__file__).resolve().parents[1]
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    env = {key: value for key, value in os.environ.items()
           if not key.startswith(("EASYCODE_", "OPENAI_", "ANTHROPIC_"))}
    env["PYTHONDONTWRITEBYTECODE"] = "1"
    started = time.monotonic()
    report = {"platform": platform.platform(), "builds": [], "scripts": [], "passed": False}
    try:
        with tempfile.TemporaryDirectory(prefix="easycode-smoke-suite-") as directory:
            binary = Path(directory)
            env["EASYCODE_SMOKE_BIN_DIR"] = directory
            for name, package in (("app.test", "./internal/app"), ("tui.test", "./internal/tui"), ("easycode", "./cmd/easycode")):
                command = [args.go, "build"] if name == "easycode" else [args.go, "test", "-c"]
                command += ["-o", str(binary / name), package]
                before = time.monotonic()
                code = run_process(command, root, env, output / (name + ".build.log"), 300)
                report["builds"].append({"binary": name, "seconds": time.monotonic() - before, "exit_code": code})
                if code:
                    raise RuntimeError(f"build failed: {name}; see {output}")
            for name, script, extra, expected in plan(binary, output):
                receipt = output / (name + ".cases.json")
                receipt.unlink(missing_ok=True)
                env["EASYCODE_SMOKE_RECEIPT"] = str(receipt)
                before = time.monotonic()
                print(f"Running native smoke: {name}", flush=True)
                code = run_process([sys.executable, "-B", str(root / "scripts" / script), *extra], root, env,
                                   output / (name + ".log"), 600)
                cases = json.loads(receipt.read_text()) if receipt.exists() else []
                result = {"name": name, "seconds": time.monotonic() - before, "exit_code": code,
                          "expected": expected, "completed": len(cases)}
                report["scripts"].append(result)
                if code or len(cases) != expected:
                    raise RuntimeError(f"smoke incomplete: {name}, exit={code}, cases={len(cases)}/{expected}; see {output}")
                print(f"Passed {name}: {len(cases)} cases, {result['seconds']:.3f}s", flush=True)
            report["passed"] = True
    except BaseException as error:
        report["error"] = str(error) or type(error).__name__
        raise
    finally:
        report["seconds"] = time.monotonic() - started
        (output / "summary.json").write_text(json.dumps(report, indent=2) + "\n")
    print(f"Native smoke passed: {sum(item['completed'] for item in report['scripts'])} cases, {report['seconds']:.3f}s")


if __name__ == "__main__":
    main()
