#!/usr/bin/env python3
"""使用显式 ACK、真实 Darwin PTY 和本地双 Provider 验收会话内 effort。"""
import argparse
import fcntl
import json
import os
from pathlib import Path
import platform
import pty
import re
import select
import signal
import struct
import subprocess
import tempfile
import termios
import time
import unicodedata


def assert_viewport(view, width, height):
    plain = re.sub(r"\x1b\[[0-?]*[ -/]*[@-~]", "", view)
    lines = plain.splitlines()
    if len(lines) > height:
        raise RuntimeError(f"viewport exceeded height {height}: {plain!r}")
    for line in lines:
        cells = sum(0 if unicodedata.combining(c) else 2 if unicodedata.east_asian_width(c) in "WF" else 1 for c in line)
        if cells > width:
            raise RuntimeError(f"viewport exceeded width {width}: {line!r}")


def run_case(binary, root, family, no_color, cancel):
    master, slave = pty.openpty()
    ack_read, ack_write = os.pipe()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 24, 80, 0, 0))
    before = termios.tcgetattr(slave)
    env = os.environ.copy()
    env.update(EASYCODE_EFFORT_TERMINAL_SMOKE="1", EASYCODE_EFFORT_FAMILY=family,
               EASYCODE_EFFORT_ACK_FD=str(ack_write), TERM="xterm-256color")
    if no_color:
        env["NO_COLOR"] = "1"
    else:
        env.pop("NO_COLOR", None)
    process = subprocess.Popen([str(binary), "-test.run=^TestEffortTerminalHelper$", "-test.timeout=55s"],
                               stdin=slave, stdout=slave, stderr=slave, pass_fds=(ack_write,), cwd=root, env=env)
    os.close(ack_write)
    pending, output, messages, bodies = bytearray(), bytearray(), [], []

    def receive(kind, predicate=lambda _: True):
        deadline = time.monotonic() + 12
        while True:
            for i, message in enumerate(messages):
                if message["kind"] == kind and predicate(message):
                    return messages.pop(i)
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise RuntimeError(f"missing {kind}: {messages[-4:]} terminal={output[-1800:]!r}")
            ready, _, _ = select.select([master, ack_read], [], [], remaining)
            for fd in ready:
                data = os.read(fd, 65536)
                if not data:
                    raise RuntimeError(f"ended before {kind}: {output[-2200:]!r}")
                if fd == master:
                    output.extend(data)
                else:
                    pending.extend(data)
                    while b"\n" in pending:
                        line, _, rest = pending.partition(b"\n")
                        pending[:] = rest
                        message = json.loads(line)
                        if message["kind"] == "body":
                            bodies.append(message)
                        messages.append(message)

    def key(data, predicate=lambda _: True):
        os.write(master, data)
        return receive("key", predicate)

    def text(value):
        return key(b"\x1b[200~" + value.encode() + b"\x1b[201~")

    def command(value, kind="effort"):
        text(value)
        key(b"\r")
        return receive(kind)

    try:
        receive("ready")
        initial = command("/effort current")
        if initial["selected"] != "high" or initial["revision"] != 0:
            raise RuntimeError(f"wrong startup: {initial}")
        text("Read README.md then answer")
        key(b"\r")
        receive("input_accepted")
        receive("live")
        text("queued review input")
        key(b"\r")
        receive("input_accepted")
        low = command("/effort LOW")
        if low["selected"] != "low" or low["captured"] != "high" or not low["pending"]:
            raise RuntimeError(f"capture changed mid turn: {low}")
        context = command("/context", "context")
        if context["selected"] != "low" or context["captured"] != "high" or not context["pending"]:
            raise RuntimeError(f"context disagrees: {context}")
        key(b"\x1b")
        command("/effort status")
        for width, height in [(28, 10), (20, 5), (1, 1), (80, 24)]:
            fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", height, width, 0, 0))
            os.kill(process.pid, signal.SIGWINCH)
            resized = receive("size", lambda m: m["width"] == width and m["height"] == height)
            assert_viewport(resized["view"], width, height)
            if width == 28 and "[queued]" not in resized["view"]:
                raise RuntimeError("effort notice displaced the queued input preview")
        if cancel:
            key(b"\x03")
            receive("failed")
            current = command("/effort status")
            if current.get("selected") != "low":
                raise RuntimeError("cancel reverted the accepted choice")
        else:
            key(b"\x1b[15~")
            receive("complete")
            text("next task")
            key(b"\r")
            receive("complete")
            auto = command("/effort unset")
            if auto.get("selected", "") != "":
                raise RuntimeError("unset inferred an effort")
            text("automatic task")
            key(b"\r")
            receive("complete")
            reset = command("/effort reset")
            if reset["selected"] != "high":
                raise RuntimeError("reset lost startup")
            text("reset task")
            key(b"\r")
            receive("complete")
            text("/effort help")
            help_view = key(b"\r")["view"]
            if "Process only" not in help_view:
                raise RuntimeError("local help missing")
            text("/effort unknown")
            invalid = key(b"\r")["view"]
            if "unknown effort argument" not in invalid or "/effort unknown" not in invalid:
                raise RuntimeError("invalid argument damaged draft")
            key(b"\x01")
            for _ in range(len("/effort unknown")):
                key(b"\x1b[3~")
            key(b"/eff", lambda m: "/effort [command]" in m["view"])
            key(b"\t")
            key(b"\r")
            receive("effort")
            text("$effort")
            key(b"\r")
            receive("complete")
            receive("skill")
        os.write(master, b"\x1b[21~")
        total = receive("total")["number"]
        process.wait(timeout=8)
        if process.returncode:
            raise RuntimeError(f"helper failed: {output[-2200:]!r}")
        expected = ["high"] if cancel else ["high", "high", "low", "", "high", "high"]
        if [m.get("selected", "") for m in bodies] != expected or total != len(expected):
            raise RuntimeError(f"actual bodies differ: {bodies}, total={total}")
        after = termios.tcgetattr(slave)
        pending_flag = getattr(termios, "PENDIN", 0)
        before[3] &= ~pending_flag
        after[3] &= ~pending_flag
        if before != after:
            raise RuntimeError("terminal attributes not restored")
        for private in [b"opaque-reasoning-sentinel", b"raw-reasoning-sentinel", b"fixture-key"]:
            if private in output:
                raise RuntimeError("private field reached terminal")
        return {"provider": family, "no_color": no_color, "cancel": cancel,
                "bodies": expected, "narrow_sizes": [[28, 10], [20, 5], [1, 1]],
                "same_name_skill": not cancel, "stacked_queue_viewport": True,
                "terminal_restored": True, "passed": True}
    finally:
        if process.poll() is None:
            process.kill()
            process.wait()
        os.close(ack_read)
        os.close(master)
        os.close(slave)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    if platform.system() != "Darwin":
        raise SystemExit("effort PTY acceptance requires Darwin")
    root = Path(__file__).resolve().parent.parent
    with tempfile.TemporaryDirectory(prefix="easycode-effort-") as temporary:
        binary = Path(temporary) / "app.test"
        subprocess.run(["go", "test", "-c", "-o", str(binary), "./internal/app"], cwd=root, check=True)
        cases = [run_case(binary, root, family, no_color, cancel)
                 for family in ["openai", "anthropic"] for no_color in [False, True] for cancel in [False, True]]
    result = json.dumps({"platform": platform.platform(), "cases": cases}, ensure_ascii=False, indent=2)
    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(result + "\n")
    print(result)


if __name__ == "__main__":
    main()
