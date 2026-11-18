"""验证 PTY 回执乱序与消费边界，不启动终端或 Provider。"""
import importlib.util
import builtins
from pathlib import Path
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location("file_mentions_smoke", Path(__file__).with_name("file-mentions-smoke.py"))
smoke = importlib.util.module_from_spec(spec)
spec.loader.exec_module(smoke)


class AckInboxTest(unittest.TestCase):
    def test_ack_import_does_not_require_posix_terminal_modules(self):
        original_import = builtins.__import__

        def portable_import(name, *args, **kwargs):
            if name in {"fcntl", "pty", "termios"}:
                raise ImportError("POSIX terminal module is unavailable")
            return original_import(name, *args, **kwargs)

        portable = importlib.util.module_from_spec(spec)
        with patch("builtins.__import__", side_effect=portable_import):
            spec.loader.exec_module(portable)
        inbox = portable.AckInbox()
        inbox.append({"kind": "key"})
        self.assertEqual(inbox.take(lambda _: True), {"kind": "key"})

    def test_probe_before_key_and_key_before_probe_are_both_observable_once(self):
        for kinds in (("scan_blocked", "key"), ("key", "scan_blocked")):
            with self.subTest(kinds=kinds):
                inbox = smoke.AckInbox()
                for kind in kinds:
                    inbox.append({"kind": kind})
                self.assertEqual(inbox.take(lambda m: m["kind"] == "key"), {"kind": "key"})
                self.assertEqual(inbox.take(lambda m: m["kind"] == "scan_blocked"), {"kind": "scan_blocked"})
                self.assertIsNone(inbox.take(lambda _: True))

    def test_successive_turns_and_new_key_never_reuse_an_old_receipt(self):
        inbox = smoke.AckInbox()
        inbox.append({"kind": "key", "view": "old"})
        inbox.append({"kind": "terminal", "turn": 1})
        after = len(inbox.messages)
        inbox.append({"kind": "terminal", "turn": 2})
        inbox.append({"kind": "key", "view": "new"})
        self.assertEqual(inbox.take(lambda m: m["kind"] == "key", after), {"kind": "key", "view": "new"})
        self.assertEqual(inbox.take(lambda m: m["kind"] == "terminal")["turn"], 1)
        self.assertEqual(inbox.take(lambda m: m["kind"] == "terminal")["turn"], 2)
        self.assertIsNone(inbox.take(lambda m: m["kind"] == "terminal"))

    def test_new_view_does_not_accept_a_stale_buffered_view(self):
        inbox = smoke.AckInbox()
        inbox.append({"kind": "refresh", "view": "old rows"})
        after = len(inbox.messages)
        self.assertIsNone(inbox.take(lambda m: "rows" in m["view"], after))
        inbox.append({"kind": "refresh", "view": "new rows"})
        self.assertEqual(inbox.take(lambda m: "rows" in m["view"], after)["view"], "new rows")

    def test_new_submission_does_not_consume_an_old_admission(self):
        inbox = smoke.AckInbox()
        inbox.append({"kind": "input_accepted", "reads": 1})
        after = len(inbox.messages)
        inbox.append({"kind": "input_accepted", "reads": 2})
        self.assertEqual(inbox.take(lambda m: m["kind"] == "input_accepted", after)["reads"], 2)


if __name__ == "__main__":
    unittest.main()
