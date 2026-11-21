# PostCompact 验收记录

验收于 2026-11-22 在 Darwin arm64、Go 1.24.13 进行。Provider 访问均指向 localhost 模拟服务。

| 验收 | 结果 |
| --- | --- |
| 双 Provider 手动／自动压缩 | Anthropic Messages 与 OpenAI Responses 均通过。无匹配、执行检查及从孤立 started 恢复后的首个普通请求，canonical 字节、原生 item 顺序及 fingerprint 等价。OpenAI WebSocket 握手回退只派发一次 PostCompact。 |
| 取消、故障与恢复 | 确认 checkpoint Sync 后取消、started 已接收后的取消、末项 finished 与取消交错、预算耗尽及关闭 Sync／清理不确定。自动初始输入、工具续轮和 Stop 路径均在不确定结果后停止提交与采样。恢复按原序号使用 checkpoint，先关闭检查再收口旧任务。复用固定前缀和注入的补偿写入失败覆盖零重跑、重复恢复和只读预览。 |
| Darwin PTY 与沙箱 | `scripts/pre-compact-smoke.py` 的 PostCompact 模式通过双 Provider，用时 1.06 秒。覆盖默认拒绝、重置信任、实际读取摘要、工作区禁写、私有文件和 localhost 访问受限、退出 2 以及后续正常采样。原 PreCompact 的 4 组 PTY 场景也通过。 |
| Headless | 双 Provider 真实 CLI 使用匹配配置仍只输出原 v1 压缩帧。严格回放确认 `skipped / hook_trust_required`，零命令执行。 |
| 构建矩阵 | Linux amd64 与 Windows amd64 CLI 及 app、runtime、session、builtin 测试二进制均交叉编译通过。真实进程沙箱和终端行为在 Darwin 验收，Linux／Windows 测试二进制未原生运行。 |
| 全仓质量门 | `make verify` 通过，63.729 秒，包含 gofmt、go vet、Staticcheck、架构边界、全量普通测试及策略选取的 race 测试。同步后的 OpenSpec 严格校验 71/71 通过。 |

单条记录大小回归独立检出计划使信封超限时丢失原 checkpoint 的风险，普通测试实测 1.10 秒。PostCompact 清理接入既有 race 用例，新增子场景实测 0.04 秒，race 清单继续沿用原选择。

整体验收期间修正了共享进程测试替身的 stdin 接线，以及 Windows 测试引用仅在 Unix 编译的辅助函数。两处修正均保留原场景与断言。

## 归档前全量复验

- `make verify TEST_OPTIONS=--race-scope=full` 通过，耗时 131.636 秒。普通和全量 race 各执行 33 个含测试入口的包、615 个入口。24 个测试调度器用例、分支规则及其测试也通过。
- 复用仓库 16 个端到端 smoke 脚本，79 个场景全部通过。包括双 Provider PostCompact/PreCompact、工具和 Stop Hooks、参数改写、真实 CLI 与 Headless、会话、Plan Mode、任务和终端交互。
- 使用本地编译的正式 CLI 对双 Provider 各执行一次实际崩溃恢复。在已确认沙箱子进程启动后向测试 CLI 发送 SIGKILL；同一线程恢复只追加一条结果未知的后置关闭记录，保留原 journal 前缀，恢复阶段零旧 Hook 或 Provider 调用。普通续写复用已保存摘要，二次空闲恢复保持 journal 逐字不变。验收桩收尾被强杀进程的测试子进程。
- 本轮在 Darwin arm64 使用回环 Provider 模拟端点，复用现有测试，额外 CLI 崩溃验收桩放在临时目录。
