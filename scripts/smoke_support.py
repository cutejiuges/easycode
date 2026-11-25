"""冒烟脚本共用构建产物与完整场景回执，不承载交互行为。"""
import json
import os
from pathlib import Path
import subprocess


def smoke_binary(name, temporary, root):
    shared = os.environ.get("EASYCODE_SMOKE_BIN_DIR")
    binary = Path(shared or temporary).resolve() / name
    if shared:
        if not binary.is_file() or not os.access(binary, os.X_OK):
            raise RuntimeError(f"missing smoke binary: {binary}")
    else:
        command = ["go", "build", "-o", str(binary), "./cmd/easycode"] if name == "easycode" else [
            "go", "test", "-c", "-o", str(binary), "./internal/" + name.removesuffix(".test")]
        subprocess.run(command, cwd=root, check=True)
    return binary


def complete_cases(cases):
    if not cases or any(case.get("passed") is False for case in cases):
        raise RuntimeError("smoke cases did not all complete")
    receipt = os.environ.get("EASYCODE_SMOKE_RECEIPT")
    if receipt:
        Path(receipt).write_text(json.dumps(cases, ensure_ascii=False, indent=2) + "\n")
