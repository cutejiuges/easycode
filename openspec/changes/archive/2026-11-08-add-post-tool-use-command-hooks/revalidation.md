# 首次推送前完整功能与回归验证

本记录对应首次推送；后续合并 main 与创建 PR 的验证见 [merge-verification.md](merge-verification.md)。

日期：2026-11-08。平台：Darwin arm64 / Go 1.24.13。分支：`feat/add-post-tool-use-hooks`，实现基线：`d7af3c9`。本轮按用户要求重新执行完整功能与回归验证，通过后提交并推送该分支，不创建 PR。

本轮不修改产品逻辑、测试断言或固定输入。对原有 176 份实现、测试、fixture、golden 和终端脚本的差异内容建立快照，提交前再次比较；另检查完整变更集的凭据特征和生成文件，未发现应排除的文件。

## 完整检查

| 命令 | 结果 |
| --- | --- |
| `go test -count=1 ./...` | 退出码 0；28 个含测试包全部重新执行，未使用测试结果缓存 |
| `go test -race -count=1 ./...` | 退出码 0；28 个含测试包全部重新执行，未报告 data race |
| `make verify` | 退出码 0；分支策略、gofmt、vet、Staticcheck、架构边界、全量测试与 race 质量门通过 |
| `openspec validate --all --strict` | 退出码 0；52 个主规格和 1 个活动 change，共 53 项通过 |
| `GOOS=linux GOARCH=amd64 go build ./...` | 退出码 0；生产代码交叉构建通过 |
| Linux app/builtin `go test -c` | 退出码 0；非 Darwin 测试消费者可编译 |
| `git diff --check`、本地文档链接和最终变更清单核对 | 退出码 0；88 个本地链接有效，215 个待提交文件与审阅清单一致 |

全量测试重新覆盖了三项 mutation、双 Provider golden/原生配对/opaque、HTTP 与 OpenAI WS、显式反馈/none/泄漏哨兵、预算、共享期限、输入输出边界、每个 Sync 故障点、取消与 shutdown owner、跨进程 lease、各恢复窗口、重复补偿、压缩与缓存、headless、Skills、提问及 TUI 快照。具体需求与测试文件映射见 [verification.md](verification.md)，主规格及活动 Pre 增量协调见 [archive.md](archive.md)。

## 真实终端回归

全部使用本地 mock 服务与合成凭据，不调用真实模型服务。以下 16 个真实 PTY 场景均完成，原脚本断言未修改：

| 脚本 | 场景与结果 |
| --- | --- |
| `scripts/smoke-hooks.py`，分别传入 `--event PostToolUse`、`--event PreToolUse` | OpenAI/Anthropic × 两事件，共 4 场景通过；信任默认拒绝、reset、普通批准、检查状态、反馈详情、取消清理、工具锚点及 clean exit |
| `scripts/user-question-smoke.py` | 选择回答、缩放后其他文字、停止、picker 取消，共 4 场景通过 |
| `scripts/completion-smoke.py` | 有色/无色 2 场景通过；补全、审批隔离、极窄窗口及终端恢复 |
| `scripts/session-picker-smoke.py` | 搜索/预览/跨目录/缩放、Esc、无色中断、恢复后退出，共 4 场景通过 |
| `scripts/markdown-transcript-smoke.py` | 有色/无色 2 场景通过；冷扫描期间输入、详情、批准、缩放与终端恢复，原性能阈值保持 |

Markdown 有色场景 P95 为 4.35 ms、最大 4.56 ms；无色场景 P95 为 3.28 ms、最大 3.43 ms，均低于脚本规定的 P95 100 ms / 最大 250 ms。

## 提交与推送边界

提交前审阅完整差异清单及英中双语 commit 文本。pre-commit 与 pre-push 保持启用，两者均调用仓库原有 `make verify`，不使用绕过选项。推送完成后核对本地 HEAD 与远端分支 SHA，并确认工作区状态；不会创建 PR。

本轮实际运行验收只覆盖 Darwin；Linux 为交叉编译，没有宣称 Linux/Windows 原生执行通过。完整测试和 mock/PTY 的通过表示本轮未发现新增回归，不替代未执行平台或真实服务的验收。
