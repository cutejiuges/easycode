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
    ack_read, ack_write = os.pipe() if event == "Stop" else (None, None)
    pid, fd = pty.fork()
    if pid == 0:
        os.chdir(root / "internal/app")
        os.environ.update(EASYCODE_HOOK_TERMINAL_SMOKE=provider,
                          TERM="xterm-256color", NO_COLOR="1", EASYCODE_HOOK_TERMINAL_EVENT=event)
        helper = "TestHookTerminalHelper"
        if event == "Stop":
            os.environ["EASYCODE_STOP_TERMINAL_SMOKE"] = provider
            os.close(ack_read)
            os.set_inheritable(ack_write, True)
            os.environ["EASYCODE_STOP_TERMINAL_ACK_FD"] = str(ack_write)
            helper = "TestStopTerminalHelper"
        os.execv(binary, [binary, "-test.run=^" + helper + "$", "-test.timeout=90s"])
    if ack_write is not None:
        os.close(ack_write)
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 32, 100, 0, 0))
    raw = bytearray()
    cursor = 0
    stages = []
    acknowledgements, pending_ack = [], bytearray()
    ack_open = ack_read is not None

    def send(value):
        os.write(fd, value)

    def pump(timeout):
        nonlocal ack_open
        ready, _, _ = select.select([fd] + ([ack_read] if ack_open else []), [], [], timeout)
        for source in ready:
            try:
                chunk = os.read(source, 65536)
            except OSError:
                return False
            if not chunk:
                if source == ack_read:
                    ack_open = False
                    continue
                return False
            if source == fd:
                raw.extend(chunk)
            else:
                pending_ack.extend(chunk)
                while b"\n" in pending_ack:
                    line, _, rest = pending_ack.partition(b"\n")
                    pending_ack[:] = rest
                    acknowledgements.append(json.loads(line))
        return True

    def wait_ack(predicate, label):
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            while acknowledgements:
                acknowledgement = acknowledgements.pop(0)
                if predicate(acknowledgement):
                    stages.append(label)
                    return acknowledgement
            if not pump(0.25):
                break
        raise RuntimeError(f"{provider}: missing {label}: terminal={bytes(raw)[-1600:]!r}")

    def wait(text, label):
        nonlocal cursor
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            plain = re.sub(rb"\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\))", b"", bytes(raw)).decode("utf-8", "replace")
            if text in plain[cursor:]:
                cursor = plain.index(text, cursor) + len(text)
                stages.append(label)
                return
            if not pump(0.25):
                break
        raise RuntimeError(f"{provider}: missing {label}: {bytes(raw)[-2500:]!r}")

    try:
        if event == "Stop":
            wait("EasyCode", "startup")
            send(b"first\r")
            wait("Trust this hook?", "default-deny-review")
            wait("saved answer", "completion-scope-disclosure")
            send(b"\r")
            wait("hook_trust_denied", "default-deny-terminal")
            wait_ack(lambda ack: ack["event"] == "turn_failed" and ack.get("code") == "hook_trust_denied", "durable-default-denial")
            send(b"/hooks\r")
            wait("Hooks", "browser")
            send(b"\r")
            wait("Hook details", "details")
            wait("Scope: Turn completion", "scope-without-matcher")
            send(b"r")
            wait("[untrusted]", "reset")
            send(b"\x1b")
            wait("Hooks", "back-to-browser")
            send(b"\x1b")
            wait("EasyCode", "draft-return")
            send(b"\x7f" * 6 + b"second\r")
            wait("Trust this hook?", "review-after-reset")
            send(b"\t\r")
            wait("stop-fixed-answer", "same-turn-continuation")
            completed = wait_ack(lambda ack: ack["event"] == "turn_completed", "same-turn-completed")
            send(b"\x0f")
            wait("Transcript", "transcript-browser")
            wait("hook:", "independent-hook-row")
            send(b"\x1b[B\r")
            wait("PTY authorized completion feedback", "frozen-model-feedback")
            send(b"\x1b[C")
            wait("Completion checks", "completion-metadata")
            fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 24, 80, 0, 0))
            os.kill(pid, signal.SIGWINCH)
            stages.append("resize-80x24")
            send(b"\x1b")
            wait("Transcript", "details-back")
            send(b"\x0f")
            wait("EasyCode", "details-anchor-return")
            send(b"cancel\r")
            target = wait_ack(lambda ack: ack["event"] == "turn_started", "cancel-turn-started")
            assert target["turn_id"] != completed["turn_id"]
            wait_ack(lambda ack: ack["event"] == "stop_hook_committed" and ack.get("phase") == "started"
                     and ack["turn_id"] == target["turn_id"] and ack["sample_index"] == 0, "cancel-check-started")
            wait("Checking completion", "saved-answer-checking")
            send(b"\x03")
            cancelled = wait_ack(lambda ack: ack["event"] == "turn_failed"
                                 and ack["turn_id"] == target["turn_id"], "durable-check-cancellation")
            assert cancelled["code"] == "cancelled", cancelled
            wait("Completion check stopped: cancelled", "cancel-cleaned-up")
            send(b"\x03")
            wait("STOP_CANCEL_FACTS=cancelled/started", "durable-started-cancelled-facts")
            wait("STOP_TERMINAL_EXIT=0 REQUESTS=4", "clean-terminal-exit")
        else:
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
        if ack_read is not None:
            os.close(ack_read)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("binary", help="go test -c ./internal/app 生成的测试二进制绝对路径")
    parser.add_argument("--event", choices=("PreToolUse", "PostToolUse", "PostToolUseFailure", "Stop"), default="PreToolUse")
    parser.add_argument("--output-dir", type=pathlib.Path, help="保存实际 PTY 输出及退出状态")
    args = parser.parse_args()
    repo = pathlib.Path(__file__).resolve().parents[1]
    for family in ("openai", "anthropic"):
        smoke(os.path.abspath(args.binary), family, repo,args.event,args.output_dir)
