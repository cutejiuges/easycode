#!/usr/bin/env python3
"""在 go test 枚举入口时保留当前包的二进制，同一阶段的分片复用该文件。"""

import errno
import hashlib
import os
from pathlib import Path
import shutil
import sys


def binary_path(directory, cwd):
    # 使用完整目录的摘要，避免不同路径下同名 Go 包覆盖二进制。
    name = hashlib.sha256(os.fsencode(Path(cwd).resolve())).hexdigest()
    return Path(directory) / (name + (".exe" if os.name == "nt" else ".test"))


def main():
    destination = binary_path(sys.argv[1], Path.cwd())
    destination.parent.mkdir(parents=True, exist_ok=True)
    retain_binary(sys.argv[2], destination)
    # 替换本进程，保持 go test 的参数、退出码、信号与 TestMain 生命周期。
    os.execv(str(destination), [str(destination), *sys.argv[3:]])


def retain_binary(source, destination):
    # 同一文件系统保留 inode，避免复制大二进制及其文件缓存；Go 删除原路径后仍可执行。
    try:
        os.link(source, destination)
    except OSError as error:
        if error.errno not in (errno.EXDEV, errno.EPERM, errno.EACCES, errno.ENOTSUP):
            raise
        shutil.copy2(source, destination)


if __name__ == "__main__":
    main()
