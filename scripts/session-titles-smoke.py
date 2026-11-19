#!/usr/bin/env python3
"""通过真实 Darwin PTY 验收会话命名、离线查找和双 Provider 恢复。"""

import argparse
import fcntl
import json
import os
from pathlib import Path
import platform
import pty
import select
import signal
import struct
import subprocess
import tempfile
import termios
import time


class Terminal:
    def __init__(self, binary, cwd, env, mode):
        self.master, self.slave = pty.openpty()
        self.output = bytearray()
        self.cursor = 0
        self.resize(88, 24)
        self.before = termios.tcgetattr(self.slave)
        self.process = subprocess.Popen(
            [binary, "-test.run=^TestTitleTerminalHelper$", "-test.timeout=60s"],
            stdin=self.slave, stdout=self.slave, stderr=self.slave,
            cwd=cwd, env=dict(env, EASYCODE_TITLE_SMOKE_MODE=mode),
        )

    def resize(self, width, height):
        fcntl.ioctl(self.slave, termios.TIOCSWINSZ, struct.pack("HHHH", height, width, 0, 0))
        if hasattr(self, "process"):
            os.kill(self.process.pid, signal.SIGWINCH)

    def wait(self, value, timeout=12, *, optional=False):
        needle = value.encode()
        deadline = time.monotonic() + timeout
        while (offset := self.output.find(needle, self.cursor)) < 0:
            remaining = deadline - time.monotonic()
            if remaining <= 0 or not select.select([self.master], [], [], remaining)[0]:
                if optional:
                    return False
                raise RuntimeError(f"Missing {value!r} in PTY output {self.output[-1600:]!r}")
            self.output.extend(os.read(self.master, 65536))
        self.cursor = offset + len(needle)
        return True

    def send(self, keys):
        os.write(self.master, keys)

    def paste(self, text):
        self.send(b"\x1b[200~" + text.encode() + b"\x1b[201~")

    def finished(self):
        self.wait("TITLE_SMOKE_DONE")
        assert self.process.wait(timeout=5) == 0, self.output[-1200:]
        # PENDIN 是内核的待重绘标志。
        pending = getattr(termios, "PENDIN", 0)
        after = termios.tcgetattr(self.slave)
        self.before[3] &= ~pending
        after[3] &= ~pending
        assert after == self.before, "PTY modes were not restored"

    def close(self):
        if self.process.poll() is None:
            self.process.kill()
            self.process.wait()
        os.close(self.master)
        os.close(self.slave)


def journal_records(root):
    paths = list((root / "sessions").rglob("*.jsonl"))
    assert len(paths) == 1, paths
    return [json.loads(line) for line in paths[0].read_text().splitlines()]


def run_case(binary, root, workspace, family, output):
    titled = "ReviewAlpha 审阅 👩‍💻"
    unused = "Working draft"
    env = dict(os.environ, EASYCODE_TITLE_SMOKE_ROOT=str(root),
               EASYCODE_TITLE_SMOKE_PROVIDER=family, TERM="xterm-256color")
    if family == "anthropic":
        env["NO_COLOR"] = "1"
    else:
        env.pop("NO_COLOR", None)
    terminal = Terminal(binary, workspace, env, "new")
    try:
        terminal.wait("EasyCode")
        terminal.paste("/rename\t")
        terminal.send(b"\r")
        terminal.wait("invalid_input")
        terminal.send(b"\x7f" * len("/rename\t"))
        terminal.paste("/rename " + unused)
        terminal.send(b"\r")
        terminal.wait("Session renamed to: " + unused)
        terminal.paste("/rename")
        terminal.send(b"\r")
        terminal.wait("Rename session")
        terminal.wait(unused)
        terminal.send(b"\x1b")
        terminal.wait("> /rename|")
        terminal.send(b"\r")
        terminal.wait("Rename session")
        terminal.send(b"\x7f" * len(unused))
        terminal.paste(titled)
        terminal.send(b"\r")
        terminal.wait("Session renamed to: " + titled)
        terminal.send(b"\x03")
        terminal.finished()
    finally:
        (output / f"{family}-new.pty.out").write_bytes(terminal.output)
        terminal.close()
    records = journal_records(root)
    assert [record["event_kind"] for record in records] == [
        "session_meta", "thread_meta", "session_title_updated", "session_title_updated"
    ], records
    thread_id = records[0]["thread_id"]
    assert records[-1]["payload"] == {"title": titled}

    # 离线列表不应依赖前次聊天使用的配置文件。
    (root / "config.json").unlink()
    listed = subprocess.run([binary, "-test.run=^TestTitleTerminalHelper$"], cwd=workspace,
                            env=dict(env, EASYCODE_TITLE_SMOKE_MODE="list"),
                            capture_output=True, timeout=10, text=True)
    assert listed.returncode == 0, (listed.stdout, listed.stderr)
    assert f"Title: {titled}" in listed.stdout and thread_id in listed.stdout, listed.stdout

    terminal = Terminal(binary, workspace, env, "picker")
    try:
        terminal.wait("Resume a session")
        terminal.wait("ReviewAlpha")
        terminal.paste("reviewalpha")
        terminal.wait("> reviewalpha")
        terminal.resize(52, 15)
        terminal.send(b"\x16")
        terminal.wait("Read-only preview")
        terminal.wait("Title: " + titled)
        terminal.wait("No visible text in this session")
        terminal.send(b"\x1b")
        terminal.wait("reviewalpha")
        terminal.send(b"\r")
        terminal.wait("Session: " + titled)
        terminal.paste("Continue selected session")
        terminal.send(b"\r")
        terminal.wait("answer-81")
        # 答复可能先于回合收尾可见。提交相同标题，等会话 owner 确认空闲。
        terminal.paste("/rename " + titled)
        for _ in range(12):
            terminal.send(b"\r")
            if terminal.wait("Session renamed to: " + titled, timeout=1, optional=True):
                break
        else:
            raise RuntimeError(f"Missing idle title confirmation in PTY output {terminal.output[-1600:]!r}")
        terminal.send(b"\x03")
        terminal.finished()
    finally:
        (output / f"{family}-picker.pty.out").write_bytes(terminal.output)
        terminal.close()
    amended = journal_records(root)
    assert amended[:len(records)] == records and amended[-1]["thread_id"] == thread_id
    assert amended[-1]["event_kind"] == "turn_completed"
    assert sum(record["event_kind"] == "session_title_updated" for record in amended) == 2
    return {"provider": family, "no_color": family == "anthropic", "terminal_restored": True,
            "offline_title": True, "same_thread_resumed": True, "model_requests": 1}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=Path(".test-results/session-titles"))
    args = parser.parse_args()
    if platform.system() != "Darwin":
        parser.error("this smoke test requires Darwin")
    args.output.mkdir(parents=True, exist_ok=True)
    repository = Path(__file__).resolve().parent.parent
    with tempfile.TemporaryDirectory(prefix="easycode-title-pty-") as directory:
        temporary = Path(directory).resolve()
        binary = temporary / "app.test"
        subprocess.run(["go", "test", "-c", "-o", binary, "./internal/app"], cwd=repository, check=True)
        results = []
        for family in ("openai", "anthropic"):
            data = temporary / family
            data.mkdir(mode=0o700)
            workspace = temporary / (family + "-workspace")
            workspace.mkdir()
            result = run_case(binary, data, workspace, family, args.output)
            results.append(result)
            print(json.dumps(result), flush=True)
        (args.output / "pty-summary.json").write_text(json.dumps(results, indent=2) + "\n")


if __name__ == "__main__":
    main()
