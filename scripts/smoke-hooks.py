#!/usr/bin/env python3
"""使用 Go 本地服务 fixture 和真实 PTY 验证两家 Provider 的 Hook 交互。"""
import argparse
import fcntl
import json
import os
import pathlib
import pty
import re
import select
import signal
import struct
import termios
import time


def smoke(binary, provider, root, event="PreToolUse", output_dir=None):
    pid, fd = pty.fork()
    if pid == 0:
        os.chdir(root / "internal/app")
        os.environ.update(EASYCODE_HOOK_TERMINAL_SMOKE=provider,
                          TERM="xterm-256color", NO_COLOR="1", EASYCODE_HOOK_TERMINAL_EVENT=event)
        os.execv(binary, [binary, "-test.run=^TestHookTerminalHelper$", "-test.timeout=90s"])
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 32, 100, 0, 0))
    raw = bytearray()
    cursor = 0
    stages = []

    def send(value):
        os.write(fd, value)

    def wait(text, label):
        nonlocal cursor
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            plain = re.sub(rb"\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\))", b"", bytes(raw)).decode("utf-8", "replace")
            if text in plain[cursor:]:
                cursor = plain.index(text, cursor) + len(text)
                stages.append(label)
                return
            ready, _, _ = select.select([fd], [], [], 0.25)
            if ready:
                try:
                    chunk = os.read(fd, 65536)
                except OSError:
                    break
                if not chunk:
                    break
                raw.extend(chunk)
        raise RuntimeError(f"{provider}: missing {label}: {bytes(raw)[-2500:]!r}")

    try:
        wait("EasyCode", "startup")
        send(b"first\r")
        wait("Trust this hook?", "default-deny-review")
        send(b"\r")
        wait("answer-2", "denied-tool-paired")
        send(b"/hooks\r")
        wait("Hooks", "browser")
        send(b"\r")
        wait("Hook details", "details")
        send(b"r")
        wait("[untrusted]", "reset")
        send(b"\x1b")
        wait("Hooks", "back-to-browser")
        send(b"\x1b")
        wait("EasyCode", "draft-return")
        send(b"\x7f" * 6 + b"second\r")
        wait("Trust this hook?", "review-after-reset")
        send(b"\t\r")
        wait("Create file", "ordinary-tool-approval")
        if event == "PostToolUseFailure":
            match = re.search(rb"HOOK_TERMINAL_WORKSPACE=([^\r\n]+)", bytes(raw))
            if match is None:
                raise RuntimeError("missing workspace acknowledgement")
            workspace = pathlib.Path(os.fsdecode(match.group(1)))
            (workspace / "terminal-marker").write_text("external conflict\n")
        send(b"\x1b[A\x1b[A\r")
        wait("answer-4", "approved-tool-completed")
        if event in ("PostToolUse", "PostToolUseFailure"):
            row = "Write · conflict" if event == "PostToolUseFailure" else "Write · success"
            send(b"\x0f")
            wait("Transcript", "transcript-browser")
            wait(row, "tool-row-loaded")
            plain = re.sub(rb"\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\))", b"", bytes(raw)).decode("utf-8", "replace")
            frame = plain.rsplit("Transcript", 1)[-1]
            selected = re.search(r"^> ([^\r\n]*)", frame, re.MULTILINE)
            if selected is None:
                raise RuntimeError(f"{provider}: missing selected transcript row")
            send(b"\r" if row in selected.group(1) else b"\x1b[B\r")
            wait(row, "original-result-details")
            send(b"\x1b[C")
            wait("failure diagnosis:" if event == "PostToolUseFailure" else "Read-only check:", "explicit-post-feedback")
            send(b"\x1b")
            wait("Transcript", "details-back")
            send(b"\x0f")
            wait("EasyCode", "details-anchor-return")
        send(b"cancel\r")
        if event in ("PostToolUse", "PostToolUseFailure"):
            wait("Create file", "cancel-tool-approval")
            if event == "PostToolUseFailure":
                (workspace / "terminal-cancel").write_text("external conflict\n")
            send(b"\x1b[A\x1b[A\r")
        wait("checking", "checking-distinct-from-running")
        send(b"\x03")
        wait("user_cancelled", "cancel-cleaned-up")
        send(b"\x03")
        if event == "PostToolUseFailure":
            wait("HOOK_FAILURE_FACTS=conflict/failure/cancelled", "durable-original-conflict-and-cancelled-check")
        wait("HOOK_TERMINAL_EXIT=0", "clean-terminal-exit")
        _, status = os.waitpid(pid, 0)
        if os.waitstatus_to_exitcode(status) != 0:
            raise RuntimeError(f"{provider}: terminal helper failed")
        if output_dir:
            output_dir.mkdir(parents=True, exist_ok=True)
            stem = provider + "-" + event
            (output_dir / (stem + ".pty.log")).write_bytes(raw)
            (output_dir / (stem + ".json")).write_text(json.dumps({"provider": provider, "event": event, "exit_code": os.waitstatus_to_exitcode(status), "stages": stages}, indent=2) + "\n")
        print(provider + "/" + event + ": " + ", ".join(stages))
    except BaseException:
        os.kill(pid, signal.SIGKILL)
        os.waitpid(pid, 0)
        raise
    finally:
        os.close(fd)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("binary", help="go test -c ./internal/app 生成的测试二进制绝对路径")
    parser.add_argument("--event", choices=("PreToolUse", "PostToolUse", "PostToolUseFailure"), default="PreToolUse")
    parser.add_argument("--output-dir", type=pathlib.Path, help="保存实际 PTY 输出及退出状态")
    args = parser.parse_args()
    repo = pathlib.Path(__file__).resolve().parents[1]
    for family in ("openai", "anthropic"):
        smoke(os.path.abspath(args.binary), family, repo,args.event,args.output_dir)
