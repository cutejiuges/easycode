# UserPromptSubmit 验收

2026-11-24，Darwin arm64，Go 1.24.13。Provider 请求使用本地模拟的 Anthropic Messages 和 OpenAI Responses 端点。

| 验收 | 实测结果 |
| --- | --- |
| 双 Provider 真实沙箱集成 | 用同一混合追加场景验证输入顺序、已保存工具结果、全拒追加后的继续、Stop 单独反馈、附件隔离和无原始进程输出泄漏。通过项的完整逻辑请求和 cache fingerprint 在同线程不中断与恢复后相等。全仓验证中用时 2.32 秒。 |
| 真实 CLI 与 Darwin PTY | 实际默认拒绝审查、授权、工作区只读与禁网、阻断、`Ctrl+R` 取回并编辑重提。OpenAI 走干净 exit `2`，Anthropic 走结构化 `decision: block`。`Ctrl+C` 在真实子进程运行时触发，进程回收且终端恢复，两个场景合计 3.34 秒。 |
| 输入归属、取消与写入 | Runtime 定向测试验证拒绝与通过项独立结算、主请求保有唯一终态、待触发的自动压缩和 SessionStart 保持待执行、Plan 确认正文保真、预算跨安全点共用和不支持平台时零披露。注入原生 writer 接纳前取消、Sync 后取消及 Sync 结果未知。Runtime 定向 race 通过，约 1.92 秒。 |
| 严格回放与恢复 | 固定 current fixture 验证正常复合交付、非法关联、伪造记录 arity、完整可读的撕裂尾批关联及旧拒绝身份复用。合法撕裂尾批只截断未提交字节。真实 writer 的重复恢复保留原前缀、不补造原生输入，先结算中断检查再继续原有回合补偿。Session 定向测试约 0.62 秒。 |
| Headless | 一次性文本与 JSON 在匹配时返回 `hook_trust_required`。stream-json v1 的严格和自动输入沿用既有输出形状，保持同批独立回执、唯一回合终态和 EOF drain。无脚本、信任审查或模型请求。流式并发回归进入默认 race 清单，实测 0.19 秒。 |
| 全仓质量门 | `make verify` 通过，含格式、vet、Staticcheck、架构约束、全量普通测试和审计 race，用时 69.49 秒。`openspec validate --all --strict` 74/74 通过，八份变更规格均已同步。 |

Linux amd64 与 Windows amd64 做全仓交叉构建。非 Darwin 的执行能力失败由 Runtime 的注入测试及平台 adapter 核对，未在原生 Linux 或 Windows 上运行 CLI。提示词附加上下文属于后续能力。

## 2026-11-25 合入主线后的验收

- 当前分支快进到 `main` 的 `71f76b2`，保留上游的历史回合分支与 SSE 解压修复。两处文档冲突均合并保留两侧内容。定向回归覆盖输入门禁、完整和撕裂 Session 重放、历史切点及 TUI 恢复。
- Darwin arm64 使用现有 DeepSeek 配置和对应 API 凭据，通过 OpenAI Responses 与 Anthropic Messages 两条协议各验证三个真实回合，共六个。每条协议都在真实 CLI/PTY 中先本地拒绝输入，然后取回、编辑并首次提交。后续同会话和 `--resume` 测试均正确召回上下文，恢复后在同一线程重新审查信任。六轮均使用合成短文本，无验收脚本重试。检查确认每条协议只保存三条通过的真实输入，凭据、被拒正文和原始脚本输出未泄漏。真实验收耗时约 7.51 秒，原始输出及测试会话未纳入仓库。
- `python3 -B scripts/session-picker-smoke.py` 通过 10 个 Darwin PTY 场景，包含历史回合切点、回填确认、编辑重提、宿主切换及退出后的终端恢复。
- Windows 交叉编译检查发现，合入的两组历史回合测试依赖 Darwin/Linux 的 Session 仓库和测试辅助函数。将这两组测试及对应 race 条目限定到实际支持的平台，Darwin 的五项相关回归通过。Linux amd64 与 Windows amd64 的全仓构建和测试编译均通过，本次原生验收在 Darwin 完成。
- 修复后的 `make verify` 通过，耗时 61.66 秒。归档前 `openspec validate --all --strict` 75/75 通过，变更的八份 delta 均与各自主规格一致。
