# PreCompact 验收记录

2026-11-19 在 Darwin arm64 和 Go 1.24.13 上验收。Provider 端到端使用 loopback 测试服务，终端和命令检查使用实际 PTY 与 Darwin 沙箱。

| 范围 | 结果 |
| --- | --- |
| 仓库质量门 | 修复后运行 `make verify TEST_OPTIONS='--race-scope=full'`，格式、vet、Staticcheck 和架构检查通过。普通测试与全量 race 各通过 533 个入口，无失败或跳过，耗时约 193 秒。 |
| 终端与受限执行 | 编译 app 测试二进制后运行 `scripts/pre-compact-smoke.py`，OpenAI 和 Anthropic 的手动与自动场景共 4/4 通过。手动覆盖窄屏、取消、拒绝及放行后的阶段顺序，自动覆盖定向取消、零摘要请求和恢复输入。`internal/tool/builtin/hook_process_darwin_test.go` 覆盖工作区写入、私有文件、连接凭据及网络限制。 |
| 正式 CLI 与退出 | 修复进程信号所有权后，重新通过 4 个 PreCompact PTY 场景和 2 个正式 CLI 补充场景。另有 7 个正式 CLI 信号场景通过，覆盖空闲 Ctrl+C、空闲或等待两家 Provider 的 PreCompact 审查时收到 SIGTERM/SIGINT。全部恢复终端并可重新打开会话，等待审查的 4 个场景写入取消终态，未启动脚本或发起摘要请求。 |
| 双 Provider 与传输 | `internal/app/compaction_e2e_test.go` 验证不匹配时摘要字节一致、显式重点只改变摘要请求、正常续写与恢复续写的 canonical bytes、原生顺序和 fingerprint 等价。OpenAI 的独立完整 WS 摘要、受限握手回退和阻止时零摘要连接通过。 |
| 自动门禁与非交互 | `internal/app/auto_compaction_test.go` 验证重点与待提交输入隔离、摘要 413 后每次只运行一次检查及请求、旧窗口通过预算可继续及超限拒绝。`internal/app/pre_compact_stream_test.go` 的手动与自动 v1 场景均以 `hook_trust_required` 收口，零命令与摘要请求。 |
| 提交、取消与回放 | `internal/runtime/pre_compact_hooks_test.go` 验证检查决定 Sync 前后与 checkpoint Sync 的取消边界、同次审查与脚本顺序、不确定时关闭接纳。`internal/session/pre_compact_replay_test.go` 验证错误 matcher、退出码、预算与非法批次在 repair 前拒绝。`internal/app/pre_compact_resume_test.go` 验证中断后先关闭压缩，再逐字交付已有 Stop 反馈，重复恢复零旧脚本与摘要请求。 |
| OpenSpec | 归档前严格校验 69 项通过，含当前变更与 66 份主规格。9 份规格增量已同步，17 项任务全部完成。 |

归档前的首轮全量端到端回归通过 14 个现有脚本的 70 个场景，以及 2 个正式 CLI 场景。Linux amd64 和 Windows amd64 交叉构建通过，原生终端和命令检查只在 Darwin 运行。原始测试日志保存在 Git 忽略的 `.test-results/pre-compact-command-hooks/`。

唯一当前 `compaction_started` 要求 `pre_compact`。受影响的固定 fixture 已更新。旧开发 journal 缺少该字段时在 repair 前拒绝。

## 合入会话标题和聊天内恢复后的复验

合入 `main` 的 `2db476a` 后，Darwin arm64 的统一质量门通过，普通和全量 race 各通过 577 个入口。OpenSpec 严格校验 69/69 通过，9 份 PreCompact 增量仍与主规格一致。Linux amd64 和 Windows amd64 交叉构建通过。

合并后复验通过 23 个 PTY 和正式 CLI 场景。覆盖双 Provider PreCompact、会话标题、聊天内 `/resume` 和进程信号。组合场景验证重命名、压缩、切出再切回和从已保存的 checkpoint 续写，切换期间两份 journal 字节一致且 Provider 请求为零。聊天和选择器复用单一信号 owner，空闲和信任等待中的 SIGTERM/SIGINT 恢复终端并返回 130。回归测试也验证活动压缩期间拒绝 `/rename` 和 `/resume` 时保留草稿，Ctrl+C 仍定向取消当前压缩。
