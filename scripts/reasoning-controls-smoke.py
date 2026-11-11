#!/usr/bin/env python3
"""复用推理阅读 PTY，验证启动设置、工具续写、独立摘要额度与计量。"""
import importlib.util
import json
from pathlib import Path
import platform
import subprocess
import tempfile


def main():
    if platform.system() != "Darwin":
        raise SystemExit("reasoning controls PTY helper requires Darwin")
    root = Path(__file__).resolve().parent.parent
    spec = importlib.util.spec_from_file_location("reasoning_smoke", root / "scripts/reasoning-smoke.py")
    harness = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(harness)
    with tempfile.TemporaryDirectory(prefix="easycode-reasoning-controls-") as temporary:
        binary = Path(temporary) / "app.test"
        subprocess.run(["go", "test", "-c", "-o", str(binary), "./internal/app"], cwd=root, check=True)
        cases = [harness.run_case(binary, root, family, no_color, cancel, controls=True)
                 for family in ["openai", "anthropic"]
                 for no_color in [False, True] for cancel in [False, True]]
        print(json.dumps({"platform": platform.platform(), "cases": cases}, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
