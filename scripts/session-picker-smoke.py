#!/usr/bin/env python3
"""通过真实 PTY 验证启动选择器和聊天内恢复，不使用真实凭据或外网。"""
import fcntl
import json
import os
from pathlib import Path
import pty
import re
import select
import signal
import struct
import subprocess
import tempfile
import termios
import time


def run_case(binary, root, name, steps, expected, no_color=False, entrypoint=None, skill=False):
    master, slave = pty.openpty()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 28, 100, 0, 0))
    before = termios.tcgetattr(slave)
    env = os.environ.copy()
    env.update(EASYCODE_PICKER_TERMINAL_SMOKE="1", TERM="xterm-256color")
    if no_color:
        env["NO_COLOR"] = "1"
    else:
        env.pop("NO_COLOR", None)
    fixture = None
    if entrypoint is not None:
        fixture = binary.parent / name
        fixture.mkdir(mode=0o700)
        env["EASYCODE_PICKER_CHAT_FIXTURE"] = str(fixture)
        env["EASYCODE_PICKER_FROM_STARTUP"] = str(int(entrypoint == "startup"))
        env["EASYCODE_PICKER_EXPECT_SKILL"] = str(int(skill))
    process = subprocess.Popen(
        [str(binary), "-test.run=^TestPickerTerminalHelper$", "-test.timeout=30s"],
        stdin=slave, stdout=slave, stderr=slave, cwd=root / "internal/app", env=env,
    )
    output = bytearray()
    cursor = 0
    threads = {}
    accounting = None

    def read_terminal(delay=0.2):
        ready, _, _ = select.select([master], [], [], delay)
        if ready:
            try:
                output.extend(os.read(master, 65536))
            except OSError:
                return False
        return True

    def wait_for(text, wrapped=False):
        nonlocal cursor
        needle = text.format(**threads).encode()
        deadline = time.monotonic() + 10
        while True:
            visible = output[cursor:]
            if wrapped:
                visible = re.sub(rb"\x1b\[[0-?]*[ -/]*[@-~]|[\r\n]", b"", visible)
            offset = visible.find(needle)
            if offset >= 0:
                cursor = len(output) if wrapped else cursor + offset + len(needle)
                return
            if time.monotonic() > deadline or not read_terminal():
                break
        raise RuntimeError(f"missing terminal output: {text}; tail={output[-2200:]!r}")

    def target_artifacts():
        return fixture / "sessions" / "artifacts" / threads["target"]

    def wait_for_target_recovery():
        deadline = time.monotonic() + 5
        # 目标在取得 lease 后创建产物目录，之后才等待已经被测试占用的会计锁。
        while time.monotonic() < deadline:
            if target_artifacts().is_dir():
                return
            if process.poll() is not None or not read_terminal(0.03):
                break
        raise RuntimeError("target did not reach restoration under its journal lease")

    try:
        if entrypoint is not None:
            wait_for("PICKER_THREADS=")
            start = cursor
            wait_for("\n")
            threads = json.loads(output[start:cursor])
            wait_for("gpt-test" if entrypoint == "startup" else "source prompt")
        else:
            wait_for("fixture-model")
        for action, value in steps:
            if action == "send":
                if isinstance(value, str):
                    value = value.format(**threads).encode()
                os.write(master, value)
            elif action == "wait":
                wait_for(value)
            elif action == "wrapped":
                wait_for(value, wrapped=True)
            elif action == "resize":
                height, width = value or (12, 30)
                fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", height, width, 0, 0))
                os.kill(process.pid, signal.SIGWINCH)
            elif action == "lock":
                accounting = (fixture / "sessions" / "artifacts" / "accounting").open("rb+")
                fcntl.flock(accounting, fcntl.LOCK_EX | fcntl.LOCK_NB)
            elif action == "reset_target_artifacts":
                target_artifacts().rmdir()
            elif action == "target_recovery":
                wait_for_target_recovery()
            elif action == "signal":
                os.kill(process.pid, value)
        wait_for(f"PICKER_EXIT={expected}\r")
        process.wait(timeout=5)
        if process.returncode != 0:
            raise RuntimeError(f"unexpected exit: {output[-500:]!r}")
        if b"blocked-handoff-paste" in output:
            raise RuntimeError("paste escaped the blocked handoff into the composer")
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
        if accounting is not None:
            accounting.close()
        os.close(master)
        os.close(slave)


def main():
    root = Path(__file__).resolve().parent.parent
    with tempfile.TemporaryDirectory(prefix="easycode-picker-smoke-") as directory:
        binary = Path(directory).resolve() / "app.test"
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
            run_case(binary, root, "in_chat_preview_cancel_and_switch", [
                ("send", "/resume\x02\r"), ("wait", "Current directory"),
                ("send", b"\x01"), ("wait", "All directories"),
                ("send", "{foreign}"), ("wait", "> {foreign}"),
                ("send", b"\x16"), ("wait", "foreign prompt"),
                ("resize", (12, 30)),
                ("send", b"\x1b"), ("wait", "All directories"),
                ("send", b"\r"), ("wait", "Created in:"),
                ("send", b"\x1b[6~"), ("wrapped", "easycode --resume {foreign}"),
                ("send", b"\x03"), ("wait", "Resume cancelled"), ("wait", "> /resum|e"),
                ("send", b"\x05"), ("send", " {foreign}\r"),
                ("wrapped", "easycode --resume {foreign}"), ("send", b"\x1b"),
                ("resize", (28, 100)),
                ("wait", "> /resume {foreign}|"),
                ("send", b"\x7f" * len(" 00000000-0000-0000-0000-000000000000") + b"\r"),
                ("wait", "Current directory"),
                ("send", "{target}"), ("wait", "> {target}"), ("send", b"\r"),
                ("wait", "Resumed session {target}"), ("wait", "target prompt"),
                ("send", "/resume {source}\r"),
                ("wait", "Resumed session {source}"), ("wait", "source prompt"),
                ("send", "$resume exact args\r"), ("wait", "answer-4"),
                ("signal", signal.SIGTERM),
            ], 130, no_color=True, entrypoint="chat", skill=True),
            run_case(binary, root, "startup_picker_then_in_chat_resume", [
                ("send", "{source}"), ("wait", "> {source}"), ("send", b"\r"),
                ("wait", "source prompt"), ("send", "/resume {target}\r"),
                ("wait", "Resumed session {target}"), ("wait", "target prompt"),
                ("send", b"\x03"),
            ], 0, entrypoint="startup"),
            run_case(binary, root, "blocked_target_esc_retry_and_signal", [
                ("lock", None), ("reset_target_artifacts", None),
                ("send", "/resume {target}\r"), ("wait", "Resuming session"), ("target_recovery", None),
                ("send", b"\r\r\x1b[200~blocked-handoff-paste\n\x1b[201~"),
                ("send", b"\x1b"), ("wait", "Resume cancelled"), ("wait", "> /resume {target}|"),
                ("reset_target_artifacts", None),
                ("send", b"\r"), ("wait", "Resuming session"), ("target_recovery", None),
                ("signal", signal.SIGTERM),
            ], 130, no_color=True, entrypoint="chat"),
        ]
        print(json.dumps(cases, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
