#!/usr/bin/env python3
"""复用 Hook PTY owner，验收两家 Provider 的 Stop 完成检查交互。"""
import argparse
import importlib.util
import pathlib


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("binary", help="go test -c ./internal/app 生成的测试二进制绝对路径")
    parser.add_argument("--output-dir", type=pathlib.Path, required=True, help="保存实际 PTY 日志与阶段结果")
    args = parser.parse_args()
    root = pathlib.Path(__file__).resolve().parents[1]
    spec = importlib.util.spec_from_file_location("hook_smoke", root / "scripts/smoke-hooks.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    for family in ("openai", "anthropic"):
        module.smoke(str(pathlib.Path(args.binary).resolve()), family, root, "Stop", args.output_dir)
