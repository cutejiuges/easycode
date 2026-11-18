#!/usr/bin/env python3
"""用真实 Darwin PTY、双 Provider 假服务与明确 ACK 验证任务清单。"""
import argparse
import fcntl
import importlib.util
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


def run_case(binary, root, family, no_color, output_dir):
    master, slave = pty.openpty()
    ack_read, ack_write = os.pipe()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 24, 80, 0, 0))
    before = termios.tcgetattr(slave)
    env = os.environ.copy()
    env.update(EASYCODE_TASK_TERMINAL_SMOKE="1", EASYCODE_TASK_FAMILY=family,
               EASYCODE_TASK_ACK_FD=str(ack_write), TERM="xterm-256color")
    if no_color:
        env["NO_COLOR"] = "1"
    else:
        env.pop("NO_COLOR", None)
    process = subprocess.Popen([str(binary), "-test.run=^TestTaskTerminalHelper$", "-test.timeout=90s"],
                               stdin=slave, stdout=slave, stderr=slave, cwd=root, env=env, pass_fds=(ack_write,))
    os.close(ack_write)
    pending, output, messages, received = bytearray(), bytearray(), [], []
    spec = importlib.util.spec_from_file_location("effort_smoke", root / "scripts/effort-smoke.py")
    harness = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(harness)

    def receive(kind, predicate=lambda _value: True):
        deadline = time.monotonic() + 15
        while True:
            for i, value in enumerate(messages):
                if value["kind"] == kind and predicate(value):
                    return messages.pop(i)
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise RuntimeError(f"missing {kind}: ACK={messages[-6:]} terminal={output[-1800:]!r}")
            ready, _, _ = select.select([master, ack_read], [], [], remaining)
            for fd in ready:
                try:
                    data = os.read(fd, 65536)
                except OSError:
                    data = b""
                if not data:
                    raise RuntimeError(f"ended before {kind}: {output[-2500:]!r}")
                if fd == master:
                    output.extend(data)
                else:
                    pending.extend(data)
                    while b"\n" in pending:
                        line, _, rest = pending.partition(b"\n")
                        pending[:] = rest
                        value = json.loads(line)
                        messages.append(value)
                        received.append(value)

    def key(data):
        os.write(master, data)
        return receive("key")

    def text(value):
        return key(b"\x1b[200~" + value.encode() + b"\x1b[201~")

    def clear_command():
        key(b"\x01")
        for _ in range(6):
            key(b"\x1b[3~")

    stages = []
    try:
        receive("ready")
        receive("tasks", lambda value: value["state"] == "none")
        text("work")
        key(b"\r")
        updated = receive("updated")
        if updated["state"] != "snapshot":
            raise RuntimeError("first task update did not hydrate")
        receive("live", lambda value: value["number"] == 2)
        first = key(b"\x14")["view"]
        second = key(b"\x14")["view"]
        if first == second or "20" not in second:
            raise RuntimeError("Ctrl+T failed to toggle the current task region")
        stages.append("20-task-current-and-toggle")
        key(b"\x0f")
        receive("view", lambda value: value["view"].startswith("Transcript\n") and "TaskProgress" in value["view"] and "Loading" not in value["view"])
        key(b"\r")
        detail = receive("detail")
        if "步骤20" not in detail["view"]:
            raise RuntimeError("Ctrl+O lost complete frozen task input")
        key(b"\x1b")
        key(b"\x0f")
        stages.append("frozen-task-detail")
        text("/ta")
        completed = key(b"\t")["view"]
        if "/tasks" not in completed:
            raise RuntimeError("tasks completion missing")
        key(b"\r")
        panel = receive("tasks")["view"]
        if "Tasks ·" not in panel:
            raise RuntimeError("/tasks did not open the complete panel")
        end = key(b"\x1b[F")["view"]
        if "步骤20" not in end:
            raise RuntimeError("20-task panel did not scroll to the last step")
        for width, height in [(120, 40), (40, 12), (13, 6), (80, 24)]:
            fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", height, width, 0, 0))
            os.kill(process.pid, signal.SIGWINCH)
            resized = receive("size", lambda value: value["width"] == width and value["height"] == height)
            harness.assert_viewport(resized["view"], width, height)
        stages.append("panel-scroll-and-resize")
        key(b"\x1b[15~")
        question = receive("question")
        if "Question" not in question["view"] or "Question" not in key(b"\x14")["view"]:
            raise RuntimeError("task key leaked through the question overlay")
        key(b"\r")
        key(b"\r")
        complete = receive("complete")
        if "Tasks ·" not in complete["view"]:
            raise RuntimeError("question did not restore the suspended task panel")
        escaped = key(b"\x1b")["view"]
        if "/tasks" not in escaped:
            raise RuntimeError("Esc did not restore the exact command draft")
        clear_command()
        text("$tasks")
        key(b"\r")
        receive("skill")
        receive("complete")
        stages.append("question-focus-and-same-name-skill")
        text("approve")
        key(b"\r")
        hook = receive("hook")
        if "Trust this hook?" not in hook["view"] or "Trust this hook?" not in key(b"\x14")["view"]:
            raise RuntimeError("task key bypassed hook trust")
        key(b"\t")
        key(b"\r")
        approval = receive("approval")
        if "Create file" not in approval["view"] or "Create file" not in key(b"\x14")["view"]:
            raise RuntimeError("task key bypassed tool approval")
        key(b"\x1b[A")
        key(b"\x1b[A")
        key(b"\r")
        receive("complete")
        stages.append("independent-hook-and-tool-approval")
        text("cancel")
        key(b"\r")
        receive("live", lambda value: value["number"] == 7)
        text("/tasks")
        key(b"\r")
        receive("tasks")
        key(b"\x03")
        receive("cancelled")
        receive("failed")
        clear_command()
        key(b"\x03")
        total = receive("total")
        process.wait(timeout=10)
        if process.returncode != 0 or total["number"] != 7:
            raise RuntimeError(f"task terminal failed: {process.returncode}, {total}")
        after = termios.tcgetattr(slave)
        pending_flag = getattr(termios, "PENDIN", 0)
        before[3] &= ~pending_flag
        after[3] &= ~pending_flag
        if before != after:
            raise RuntimeError("terminal modes were not restored")
        stages.append("active-panel-cancel-and-terminal-restore")
        return {"provider": family, "no_color": no_color, "requests": total["number"], "stages": stages,
                "terminal_restored": True}
    finally:
        if output_dir:
            output_dir.mkdir(parents=True, exist_ok=True)
            stem = family + ("-no-color" if no_color else "-color")
            (output_dir / (stem + ".pty.log")).write_bytes(output)
            (output_dir / (stem + ".ack.json")).write_text(json.dumps(received, ensure_ascii=False, indent=2) + "\n")
        if process.poll() is None:
            process.kill()
            process.wait()
        os.close(ack_read)
        os.close(master)
        os.close(slave)


def main():
    if platform.system() != "Darwin":
        raise SystemExit("tasks PTY helper requires Darwin")
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", type=Path)
    parser.add_argument("--output-dir", type=Path)
    args = parser.parse_args()
    root = Path(__file__).resolve().parent.parent
    with tempfile.TemporaryDirectory(prefix="easycode-task-smoke-") as directory:
        binary = args.binary or Path(directory) / "app.test"
        if not args.binary:
            env = os.environ.copy()
            env.update(GOPROXY="off", GOSUMDB="off")
            subprocess.run(["go", "test", "-c", "-o", str(binary), "./internal/app"], cwd=root, env=env, check=True)
        results = [run_case(binary, root, family, no_color, args.output_dir)
                   for family in ["openai", "anthropic"] for no_color in [False, True]]
        print(json.dumps({"platform": platform.platform(), "cases": results}, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
