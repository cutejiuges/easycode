#!/usr/bin/env python3
"""通过真实 PTY 验证启动选择器，不使用真实凭据或外网。"""
import fcntl
import json
import os
from pathlib import Path
import pty
import select
import signal
import struct
import subprocess
import tempfile
import termios
import time


def run_case(binary, root, name, steps, expected, no_color=False):
    master, slave = pty.openpty()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 28, 100, 0, 0))
    before = termios.tcgetattr(slave)
    env = os.environ.copy()
    env.update(EASYCODE_PICKER_TERMINAL_SMOKE="1", TERM="xterm-256color")
    if no_color:
        env["NO_COLOR"] = "1"
    process = subprocess.Popen(
        [str(binary), "-test.run=^TestPickerTerminalHelper$", "-test.timeout=30s"],
        stdin=slave, stdout=slave, stderr=slave, cwd=root / "internal/app", env=env,
    )
    output = bytearray()
    cursor = 0

    def wait_for(text):
        nonlocal cursor
        needle = text.encode()
        deadline = time.monotonic() + 10
        while needle not in output[cursor:]:
            if time.monotonic() > deadline:
                raise RuntimeError(f"missing terminal output: {text}; tail={output[-1500:]!r}")
            ready, _, _ = select.select([master], [], [], 0.2)
            if ready:
                try:
                    output.extend(os.read(master, 65536))
                except OSError:
                    break
            if process.poll() is not None:
                break
        if needle not in output[cursor:]:
            raise RuntimeError(f"missing terminal output: {text}; tail={output[-1500:]!r}")
        cursor = len(output)

    try:
        wait_for("fixture-model")
        for action, value in steps:
            if action == "send":
                os.write(master, value)
            elif action == "wait":
                wait_for(value)
            elif action == "resize":
                fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 12, 30, 0, 0))
                os.kill(process.pid, signal.SIGWINCH)
        wait_for("PICKER_EXIT=")
        process.wait(timeout=5)
        if process.returncode != 0 or f"PICKER_EXIT={expected}\r".encode() not in output:
            raise RuntimeError(f"unexpected exit: {output[-500:]!r}")
        after = termios.tcgetattr(slave)
        delta = before[3] ^ after[3]
        # Darwin 的 PENDIN 是内核待重绘状态，不是可配置终端模式。
        pending = getattr(termios, "PENDIN", 0)
        before[3] &= ~pending
        after[3] &= ~pending
        if before != after:
            raise RuntimeError(f"terminal modes not restored: {before!r} -> {after!r}")
        return {"case": name, "exit_code": expected, "terminal_restored": True, "kernel_lflag_delta": delta}
    finally:
        if process.poll() is None:
            process.kill()
            process.wait()
        os.close(master)
        os.close(slave)


def main():
    root = Path(__file__).resolve().parent.parent
    with tempfile.TemporaryDirectory(prefix="easycode-picker-smoke-") as directory:
        binary = Path(directory) / "app.test"
        subprocess.run(["go", "test", "-c", "-o", str(binary), "./internal/app"], cwd=root, check=True)
        cases = [
            run_case(binary, root, "search_preview_cross_directory_resize", [
                ("send", b"fixture"), ("wait", "> fixture"),
                ("send", b"\x16"), ("wait", "Read-only preview"),
                ("send", b"\x1b"), ("wait", "All directories"),
                ("send", b"\r"), ("wait", "another directory"),
                ("send", b"\r"), ("wait", "All directories"),
                ("resize", None), ("wait", "ID: 00000000-0001-7000-8000-"), ("send", b"\x03"),
            ], 130),
            run_case(binary, root, "normal_escape", [("send", b"\x1b")], 0),
            run_case(binary, root, "no_color_interrupt", [("send", b"\x03")], 130, True),
            run_case(binary, root, "resume_then_chat_exit", [
                ("send", b"\r"), ("wait", "another directory"),
                ("send", b"\t\r"), ("wait", "EasyCode"), ("send", b"\x03"),
            ], 0),
        ]
        print(json.dumps(cases, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
