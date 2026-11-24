# SessionStart 上下文验收

2026-11-22 在 Darwin arm64、Go 1.24.13 上执行。Provider 端使用本地协议夹具，终端、EasyCode CLI、命令 Hook、sandbox 和 Session 文件均使用实际实现。

| 检查 | 结果 |
| --- | --- |
| `make verify` | 通过，60.634 秒。覆盖 gofmt、go vet、Staticcheck、架构边界、645 个普通测试入口和 63 个审计 race 入口。 |
| `openspec validate --all --strict` | 71/71 通过，包含当前 change 及全部主规格。 |
| `python3 scripts/session-start-smoke.py` | 双 Provider 的真实 PTY 验收通过。覆盖 startup、CLI resume、手动与自动 compact、默认拒绝、许可审查、Ctrl+C 和 Ctrl+O 已保存详情。每个 Provider 核对 6 次实际 SSE 请求及对应的 journal 顺序。 |
| 双 Provider native golden 与请求回归 | 通过。逐字节比对持续运行和离线恢复的 canonical 请求，校验原生项顺序、动态历史与稳定前缀指纹、真实输入预算和工具续写下标。OpenAI 同时验证恢复与压缩后的 WebSocket 全量、后续增量和 HTTP 回退。 |
| Session 固定 fixture 与定向 race | 通过。验证合法撕裂尾批、错误 singleton、复合提交和 writer 接纳两侧的取消；双 Provider 中断恢复先收口 Hook，再交付冻结的 Stop 反馈，整个恢复过程零脚本执行和零网络请求。 |
| `GOOS=linux GOARCH=amd64 go build ./...` | 交叉构建通过。命令 sandbox 当前支持 macOS，本次原生终端验收覆盖 Darwin。 |

- 会话交接回归验证预览、取消候选和同线程切换均保持原有 Hook 执行次数，采用新目标后的第一轮才消费 resume。手动压缩后取消会退回 auto 输入，已排队的 strict-next 在清理后按原序继续。
- Hook 信任按声明审查，命令沿用只读禁网 runner。验证只有已授权并持久化的 `additionalContext` 进入 Provider 历史和 Ctrl+O 独立详情。原始 stderr 不进入请求或展示，`stream-json` v1 保持现有事件结构及顺序。
- 原始 PTY 和诊断保存在已忽略的 `.test-results/session-start/`。

## 归档前复验

- `make verify` 复验通过，耗时 53.305 秒。全量 race 回归通过，645 个普通测试入口、645 个 race 入口，耗时 133.032 秒。真实终端复用现有脚本，双 Provider 的 SessionStart、相邻 Hook、Plan 审阅交接、恢复选择器、推理和文件附件链路通过。
- 另用临时工作区验证双 Provider 的组合场景。启动时授权的通配 `SessionStart` 命令在手动压缩后复用信任。独立 PostCompact 命令以退出码 7 结束，journal 依序记录 checkpoint、PostCompact 失败关闭、SessionStart 原生上下文和用户续写。下一请求按摘要、上下文、真实输入各一次交付。
- 修正原有 PTY 脚本的回合收尾等待，取消场景等到回合终态，旧 `/context` 查询被丢弃时重新查询。未增加仓库测试用例或夹具。
