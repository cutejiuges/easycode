#!/usr/bin/env python3
"""以真实 Darwin PTY 和本地 Provider 检查手动及自动 PreCompact。"""
import argparse
import fcntl
import json
import os
from pathlib import Path
import pty
import select
import signal
import struct
import subprocess
import termios
import time


def run_case(binary, family, mode):
    master, slave = pty.openpty()
    ack_read, ack_write = os.pipe()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 32, 100, 0, 0))
    before = termios.tcgetattr(slave)
    environment = dict(os.environ, EASYCODE_PRE_COMPACT_TERMINAL=mode,
                       EASYCODE_PRE_COMPACT_FAMILY=family, EASYCODE_PRE_COMPACT_ACK_FD=str(ack_write),
                       TERM="xterm-256color", NO_COLOR="1")
    process = subprocess.Popen(
        [binary, "-test.run=^TestPreCompactTerminalHelper$", "-test.timeout=70s"],
        stdin=slave, stdout=slave, stderr=slave, pass_fds=(ack_write,), env=environment,
    )
    os.close(ack_write)
    pending, raw, messages, emitted = bytearray(), bytearray(), [], []

    def receive(kind, predicate=lambda _: True):
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            for index, message in enumerate(messages):
                if message["kind"] == kind and predicate(message):
                    return messages.pop(index)
            ready, _, _ = select.select([master, ack_read], [], [], max(0, deadline - time.monotonic()))
            for fd in ready:
                data = os.read(fd, 65536)
                if not data:
                    raise RuntimeError(f"helper closed before {kind}: {raw[-2600:]!r}")
                if fd == master:
                    raw.extend(data)
                    continue
                pending.extend(data)
                while b"\n" in pending:
                    line, _, rest = pending.partition(b"\n")
                    pending[:] = rest
                    message = json.loads(line)
                    messages.append(message)
                    emitted.append(message["kind"])
        raise RuntimeError(f"missing {kind}: {messages[-4:]} terminal={raw[-2600:]!r}")

    def key(data, view_text=""):
        os.write(master, data)
        return receive("key", lambda item: view_text in item.get("view", "")).get("view", "")

    def paste(value):
        return key(b"\x1b[200~" + value.encode() + b"\x1b[201~")

    def trigger(value):
        paste(value)
        key(b"\r")
        started = receive("compaction_started")["view"]
        assert "Preparing context compaction" in started and "Compacting conversation" not in started
        review = receive("hook_trust_requested")["view"]
        assert f"PreCompact: {mode}" in review and "> Deny" in review
        assert "go to the provider" in review and "summary may retain" in review
        assert ("original /compact instructions" in review) == (mode == "manual")

    def resize(width, height):
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", height, width, 0, 0))
        os.kill(process.pid, signal.SIGWINCH)
        return receive("size", lambda item: item["width"] == width and item["height"] == height)["view"]

    try:
        receive("ready")
        receive("size")
        command = "/compact terminal-user-focus"
        trigger(command if mode == "manual" else "next automatic input")
        assert "Hook approval" in resize(16, 5)
        assert "Hook approval" in key(b"\t")
        assert "Hook approval" in key(b"\r")
        resize(100, 32)
        assert "blocked-draft-paste" not in paste("blocked-draft-paste")
        key(b"\x03", "Cancelling compaction")
        cancelled = receive("compaction_failed")
        assert cancelled["state"] == "compaction_cancelled" and "Trust this hook?" not in cancelled["view"]
        assert "hook_trust_resolved" not in emitted and "compaction_summarizing" not in emitted

        if mode == "auto":
            receive("turn_failed")
            assert "draft after cleanup" in paste("draft after cleanup")
        else:
            trigger(command)
            key(b"\r")
            denied = receive("compaction_failed")
            assert denied["state"] == "hook_trust_denied" and "Compacting conversation" not in denied["view"]
            paste("/hooks")
            key(b"\r", "Hooks · Local checks")
            key(b"\r", "Hook details")
            key(b"r")
            receive("view", lambda item: "[untrusted]" in item["view"])
            key(b"\x1b", "Hooks · Local checks")
            key(b"\x1b", "/hooks")
            key(b"\x7f" * len("/hooks"), "EasyCode")
            trigger(command)
            key(b"\t", "> Trust for this session")
            key(b"\r")
            check = receive("checking")["view"]
            assert "Running PreCompact checks" in check and "Compacting conversation" not in check
            assert "Compacting conversation" in receive("compaction_summarizing")["view"]
            assert "Conversation compacted" in receive("compaction_completed")["view"]
            assert emitted.count("compaction_summarizing") == 1

        os.write(master, b"\x1b[21~")
        assert receive("total")["number"] == (2 if mode == "manual" else 1)
        process.wait(timeout=5)
        assert process.returncode == 0
        assert b"terminal-hook-focus" not in raw
        after = termios.tcgetattr(slave)
        flag = getattr(termios, "PENDIN", 0)
        before[3] &= ~flag
        after[3] &= ~flag
        assert before == after
        print(f"{family}/{mode}: terminal stages, directed cancellation and provider requests verified")
    finally:
        if process.poll() is None:
            process.kill()
            process.wait()
        os.close(ack_read)
        os.close(master)
        os.close(slave)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("binary", type=Path, help="go test -c ./internal/app 生成的二进制文件")
    binary = str(parser.parse_args().binary.resolve())
    for family in ("openai", "anthropic"):
        for mode in ("manual", "auto"):
            run_case(binary, family, mode)
