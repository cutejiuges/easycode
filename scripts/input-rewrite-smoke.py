#!/usr/bin/env python3
"""使用真实 Darwin PTY 验证完整改写审查、窄屏停用、详情和取消。"""
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


def smoke(binary, family, repo, output_dir):
    pid, fd = pty.fork()
    if pid == 0:
        os.chdir(repo / "internal/app")
        os.environ.update(EASYCODE_REWRITE_TERMINAL_PROVIDER=family,
                          TERM="xterm-256color", NO_COLOR="1")
        os.execv(binary, [binary, "-test.run=^TestInputRewriteTerminalHelper$",
                         "-test.timeout=90s"])
    raw, stages = bytearray(), []
    cursor = 0

    def resize(width, height):
        fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", height, width, 0, 0))
        os.kill(pid, signal.SIGWINCH)
        stages.append(f"resize-{width}x{height}")

    def plain():
        return re.sub(rb"\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\))",
                      b"", bytes(raw)).decode("utf-8", "replace")

    def send(value):
        os.write(fd, value)

    def wait(text, label):
        nonlocal cursor
        deadline = time.monotonic() + 15
        while time.monotonic() < deadline:
            text_view = plain()
            if text in text_view[cursor:]:
                cursor = text_view.index(text, cursor) + len(text)
                stages.append(label)
                return
            ready, _, _ = select.select([fd], [], [], 0.2)
            if ready:
                try:
                    block = os.read(fd, 65536)
                except OSError:
                    break
                if not block:
                    break
                raw.extend(block)
        raise RuntimeError(f"{family}: missing {label}: {bytes(raw)[-2500:]!r}")

    def assert_absent(workspace):
        if (workspace / "terminal-effective").exists() or (workspace / "terminal-original").exists():
            raise RuntimeError(f"{family}: a default or disabled approval executed a target")

    status, error, reaped = None, None, False
    try:
        resize(80, 24)
        wait("EasyCode", "startup-no-color")
        match = re.search(r"REWRITE_WORKSPACE=([^\r\n]+)", plain())
        if match is None:
            raise RuntimeError("missing terminal fixture workspace")
        workspace = pathlib.Path(match.group(1))
        send(b"first\r")
        wait("Trust this hook?", "default-deny-review")
        wait("Changed candidates", "persistence-disclosure")
        send(b"\r")
        wait("answer-2", "default-deny-completed")
        assert_absent(workspace)
        send(b"/hooks\r")
        wait("Hooks", "hook-browser")
        send(b"\r")
        wait("Hook details", "full-hook-review")
        send(b"r")
        wait("[untrusted]", "reset-denied-trust")
        send(b"\x1b")
        wait("Hooks", "back-to-hook-browser")
        send(b"\x1b")
        wait("EasyCode", "return-to-draft")
        send(b"second\r")
        wait("Trust this hook?", "independent-hook-trust")
        send(b"\t\r")
        wait("Create file", "ordinary-effective-approval")
        wait("terminal-effective", "actual-target-reviewed")
        resize(28, 10)
        wait("Approval disabled", "narrow-approval-disabled")
        send(b"\x1b[A\x1b[A\r")
        resize(8, 2)
        wait("Approval", "tiny-approval-disabled")
        send(b"\x1b[A\r")
        assert_absent(workspace)
        resize(80, 24)
        wait("Create file", "resize-preserves-review")
        send(b"\r")
        wait("answer-4", "ordinary-default-deny-preserved")
        assert_absent(workspace)
        send(b"third\r")
        wait("Create file", "next-effective-candidate")
        send(b"\x1b[A\x1b[A\r")
        wait("answer-6", "approved-effective-write")
        expected = "界\nline\x1b[31m\n"
        if (workspace / "terminal-effective").read_text() != expected:
            raise RuntimeError("effective content or control bytes changed")
        if (workspace / "terminal-original").exists():
            raise RuntimeError("original target executed")
        send(b"\x0f")
        wait("Transcript", "transcript-browser")
        wait("Write · success", "effective-result-row")
        frame = plain().rsplit("Transcript", 1)[-1]
        selected = re.search(r"^> ([^\r\n]*)", frame, re.MULTILINE)
        if selected is None:
            raise RuntimeError("missing selected transcript row")
        send(b"\r" if "Write · success" in selected.group(1) else b"\x1b[B\r")
        wait("Write · success", "effective-result-detail")
        send(b"\x1b[D")
        wait("original request O", "separate-original-and-candidate-audit")
        send(b"\x1b[B" * 12)
        wait("effective input E", "selected-input-detail")
        send(b"\x1b")
        wait("Transcript", "details-back")
        send(b"\x0f")
        wait("EasyCode", "detail-anchor-return")
        send(b"cancel\r")
        wait("checking", "checking-distinct-from-running")
        send(b"\x03")
        wait("user_cancelled", "cancel-waits-for-cleanup")
        send(b"\x03")
        wait("REWRITE_TERMINAL_FACTS=denied/denied/success/cancelled", "durable-four-turn-facts")
        wait("REWRITE_TERMINAL_EXIT=0", "clean-exit")
        _, wait_status = os.waitpid(pid, 0)
        reaped = True
        status = os.waitstatus_to_exitcode(wait_status)
        if status != 0:
            raise RuntimeError(f"terminal helper exit {status}")
        if b"\x1b[31m" in raw:
            raise RuntimeError("untrusted control text escaped into the terminal")
        print(f"{family}: " + ", ".join(stages))
    except BaseException as exc:
        error = str(exc)
        if not reaped:
            try:
                os.kill(pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            _, wait_status = os.waitpid(pid, 0)
            status = os.waitstatus_to_exitcode(wait_status)
        raise
    finally:
        os.close(fd)
        output_dir.mkdir(parents=True, exist_ok=True)
        (output_dir / f"{family}-input-rewrite.pty.log").write_bytes(raw)
        (output_dir / f"{family}-input-rewrite.json").write_text(json.dumps({
            "provider": family, "exit_code": status, "error": error,
            "no_color": True, "stages": stages}, ensure_ascii=False, indent=2) + "\n")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("binary", help="go test -c ./internal/app 生成的测试二进制绝对路径")
    parser.add_argument("--output-dir", type=pathlib.Path, required=True)
    args = parser.parse_args()
    root = pathlib.Path(__file__).resolve().parents[1]
    for provider in ("openai", "anthropic"):
        smoke(os.path.abspath(args.binary), provider, root, args.output_dir)
