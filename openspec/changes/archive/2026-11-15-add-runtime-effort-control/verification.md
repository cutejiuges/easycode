# 会话内 effort 控制验收

> 历史日志、运行清单和诊断转储已清理，仅保留文字结论；当前验收以现存测试及质量门为准。

后续已整合 main 的 PreToolUse 参数改写；当前交付的整合基线和验证见 [main-sync-2026-11-15.md](main-sync-2026-11-15.md)。下文保留各轮原始执行记录，不将旧源码校验清单当作整合后的当前基线。

核对日期：2026-11-15。变更：`add-runtime-effort-control`；分支：`feat/runtime-effort-control`。首次实现 38/38 和 11 月 14 日完整复审 44/44 的结果保留如下；主规格已同步，change 已于 2026-11-15 归档，核对见 [archive.md](archive.md)。归档前再次回归、端到端及耗时排查的第 10 节三项任务也已完成，现为 47/47。完整未缓存质量门通过，24 组真实 PTY、10 组脚本化真实 CLI 和 OpenSpec 严格校验通过，覆盖范围内未发现新的功能问题。归档前的范围和证据见 [regression-2026-11-15.md](regression-2026-11-15.md)，此前记录不能替代本轮结果。

## 实现与选择理由

本次交付 `/effort` 和同源 stream-json 控制。现有双 Provider 已有完整推理字段、原生历史、摘要和缓存计划，缺口主要是运行中的产品控制及操作边界；复用这些基础，能提供直接可见的交互收益，并集中验证 owner、策略冻结和恢复，而不扩展模型发现、鉴权或整个 P5。

交互参考 Claude Code 的精确 slash、短反馈、auto/reset 和既有面板；内核参考 Codex 的候选校验、最新选择与活动策略分离。没有照搬 Claude 在循环内读取最新 appState、模型默认推断和摘要禁用 thinking，也没有引入 Codex 专属 configuration_update/pin 路径。固定参考与差异详见 [design.md](design.md) 第二次对照表；reference-readonly.json 记录两份源码 HEAD 与 tracked diff 清洁状态。参考工程未修改或作为构建依赖。

## 功能、故障与恢复矩阵

| 验收范围 | 结果与业务断言 | 主要证据 |
| --- | --- | --- |
| 1.1–1.3 不可变值与共享校验 | 通过；两家原生档位、auto/reset/显式 none、family 与 disabled 冲突、预算、隐式普通上限来源、同 wire profile 身份 | `internal/domain/effort_test.go`、`internal/context/inference_test.go`、原 config reasoning 矩阵 |
| 2.1–2.4 当前协议 | 通过；专属命令、updated、安全错误、三种 active 联合、排他身份、必需 uint64 与未来/溢出拒绝、null/重复/未知字段、16 KiB；旧内部 context shape 拒绝，外部数值 shape 独立 | `internal/protocol/effort_test.go`、`internal/headless/effort_test.go`、固定 `effort_snapshots.golden.jsonl` / `context_metrics.golden.jsonl` |
| 3.1–3.4 Provider 策略输入 | 通过；所有采样输入必须有有效策略，零值/跨 family 在发送前失败；两家 body/properties/native 证据指纹固定，旧默认 golden 保持，摘要使用独立额度 | 两家 `effort_test.go` / `reasoning_request_test.go` 及 `runtime_effort_requests.golden.jsonl`；全量 Provider 测试 |
| 4.1–4.6 owner 与操作冻结 | 通过；set/submit、FIFO、handoff 前后取消、重复、closing/draining/poisoned、来源变化/幂等/序号上限；零设置 journal；构造失败原子拒绝、context 失败不回滚；started Sync/失败收口/terminal cleanup 确定性屏障 | `internal/runtime/effort_test.go`、`effort_stream_test.go`、原 agent loop 与输入安全点测试 |
| 4.2–4.3 工具、Stop 与摘要 | 通过；活动 high 接收 low 后工具、Question、普通审批、Stop 续轮及自动摘要仍 high；下一操作 low；独立摘要预检/执行同源，失败来源抑制及三次熔断不重置 | `TestEffortManualAndAutomaticCompactionCapture`、`TestEffortStopContinuationUsesCapturedPolicy`、`TestRuntimeEffortQuestionAndApprovalKeepCapturedPolicy` |
| 5.1–5.6 TUI | 通过；current/status/unset/help 别名、ASCII 参数、精确命令、拒绝草稿/光标保真、方法成功不生成重复反馈；选择水位与请求/操作世代分离，迟到回复不清草稿/抢新 Question；第七项内置保留 256 Skill，`$effort` 可达 | `internal/tui/effort_test.go`、completion/context 测试；16 份 effort 固定尺寸/颜色快照；另有取消中且已有正文的极小窗口回归 |
| 6.1–6.4 stream-json 与资源 owner | 通过；4 KiB 分支解码、分片 UTF-8/无换行 EOF、正文 slash 不执行；response 在单个匹配 snapshot 前；拒绝无 snapshot；EOF drain、snapshot 短写、取消、满 buffer detach、dispatcher/worker 最终完成；旧一次性输出回归保持 | `internal/headless/effort_test.go`、`internal/runtime/effort_stream_test.go`、`TestRuntimeEffortBrokenSnapshotReleasesRealLeaseWithoutRetry` |
| 7.1–7.2 实际会话与故障 | 通过；本地双 Provider 工具续写/问卷/审批/后续输入；400、断流、取消、摘要失败不降档/重发；partial native/usage 不提交；真实子进程证明 shutdown 超时仍持有 lease，清理后才释放 | `internal/app/effort_e2e_test.go`、`TestEffortHostShutdownRetainsRealLeaseUntilTerminalCleanup`、既有权限与故障注入矩阵 |
| 7.3 原生恢复 | 通过；override 重启归启动配置/序号 0，读取与设置不改 journal 前缀、恢复零自动网络；有 opaque/工具 pairing/摘要 checkpoint 的 uninterrupted 与 restored 在选择序号 1/3 下请求 bytes、request/native/profile/source fingerprint 等价 | `TestRuntimeEffortRestoredRequestIgnoresProcessRevision`、两家固定 `reasoning_restore.current.jsonl` 及恢复测试、现有 Session 当前 fixture/Hook 恢复矩阵 |
| 7.4、7.6 缓存 | 通过；Anthropic disabled/5m/1h 与 disabled/adaptive/manual 的 body/native/properties golden；OpenAI 真实 WS 属性改变 full，成功后增量；无中间操作的等价 high 保持资格；low 实际失败/取消及摘要失败后选回 high 仍 full，不复活旧 response ID；原 HTTP/WS 安全回退保持 | 两家 `TestRuntimeEffortRequestPolicyGolden`、`TestRuntimeEffortWebsocketPropertiesNoopFailureAndSummaryReset`、原 websocket fallback/summary/reconnect 测试 |
| 7.5、7.7 真实终端与未发送边界 | 通过；8 组双 Provider × NO_COLOR × 取消/正常，真实 body 顺序 high/high/low/omitted/high/high；28×10、20×5、1×1、帮助、非法恢复、补全、同名 Skill、取消和终端属性恢复；本地预算拒绝时零网络，不把 capture 说成 sent | pty.json、`scripts/effort-smoke.py`、`TestRuntimeEffortCaptureCanFailBeforeFirstNetwork` |

## 命令与平台

- `go test ./...`：实现中全量测试通过；最终质量门再次覆盖。
- `go test -race ./internal/app ./internal/runtime ./internal/tui ./internal/headless ./internal/provider/... -run Effort -count=1 -timeout=90s`：退出 0，见 effort-race.log。
- `python3 scripts/effort-smoke.py --output openspec/changes/add-runtime-effort-control/evidence/pty.json`：8/8 通过；使用 ACK、channel 和可复核就绪信号，不使用 sleep 证明时序。
- `python3 scripts/reasoning-controls-smoke.py`：既有启动设置、推理阅读、工具/摘要与终端恢复的 8 组 PTY 回归通过，见 reasoning-controls-pty.json。
- `python3 scripts/completion-smoke.py`：既有补全/粘贴/普通审批隔离的 2 组 PTY 回归通过，见 completion-pty.json。
- 本 change `openspec validate add-runtime-effort-control --strict`：退出 0。
- `openspec validate --specs --strict`：60/60 主规格通过；六项同步详情见 spec-sync.json。
- `openspec validate --all --strict`：62/62 通过，见 all-strict.log。长 requirement 的 INFO 不代表失败。
- 最终 `make verify`：退出 0，909.44 秒；gofmt、go vet、Staticcheck、架构边界、全量测试与 race 全部通过。前后源码 SHA-256 一致，403 份固定测试数据未变化，见 make-verify-final.json、make-verify-final.log 和 fixed-fixtures-check.json。

实现中一轮完整 `make verify` 已退出 0（910.53 秒）。随后新增的取消中 20×5 视口回归发现多行溢出，修正为极小高度优先 composer，并同步固定快照；同时完成本地导航/取消的反馈失效审计，补充迟到回复保留草稿测试。中间重复运行被主动中断，记录为 `make-verify-superseded.*` 与 `make-verify-feedback-audit-superseded.*`，均不作为通过证据。最终一轮质量门对代码/测试/脚本与构建输入记录前后 SHA-256，要求保持一致。

实际原生平台为 macOS 27.2.1 arm64、Go 1.24.13。所有服务使用本地测试端点与虚构 fixture key，没有调用真实 Provider。Darwin PTY 与沙箱行为已原生执行；本次未在 Linux/Windows 执行原生终端或沙箱验收，不以交叉编译代替这些平台结果。

## 契约与交付审计

- domain/protocol 保持纯值与严格边界；预算逻辑在 context，共用现有 Runtime owner、worker、宿主 dispatcher 和唯一 stdout writer。没有新增后台资源 owner、可变全局、无消费者 facade 或反向依赖。
- 只有实际 wire effort 进入 operation profile；source、selection_revision 和 UI 世代不进入请求/Session/cache。原生 opaque、工具配对、checkpoint、固定 current fixture 与 append-only schema 均保持唯一当前契约；没有 effort record、格式版本、迁移器、兼容 reader 或 resume 重写。
- 设置/查询没有网络、工具、Hook、磁盘配置或 Session 写入，不改变调用预算、权限或信任；失败与取消不重跑副作用。snapshot 仅含有限字段，错误固定英文；没有 prompt、raw usage、opaque、key、header、工作区绝对路径进入新增控制投影。
- README、能力状态、架构、Roadmap、pitfall 及六项主规格已同步，Purpose 与旧 effort 启动冻结措辞已核对；其他推理字段热切换、模型发现与完整 P5 未写成已实现。

## 完整复审验收

本轮逐项核对原 38 项任务、六份 delta 和 AGENTS.md 七组约束，重新只读比较两个参考工程。发现及修复 R1–R7 详见 [review.md](review.md)：独立摘要网络前取消清理、Skill 保留名、TUI 叠加宽高、权威选择缓存、help 错误残留、严格活动/序号组合及过时文档。补充了真实 Stop+自动摘要组合验证，所有采样仍使用 root turn 的捕获策略。

| 本轮检查 | 当前结果与证据 |
| --- | --- |
| 完整质量门 | `make verify` 退出 0，897.92 秒；分支策略、gofmt、vet、Staticcheck、架构、全量测试和全量 race 全部通过。1,368 个源码/测试/脚本/构建及 fixture 输入在执行前后完全一致，汇总 SHA-256 为 `6d80929b274c674bd9444ce9f3fc1199fba4734218ede90709b7f14085e5a2cc`；见 make-verify.json、make-verify.log、source-before.json、source-after.json。 |
| 功能 race 验收 | `go test -race` 对 app/runtime/tui/headless/provider/protocol/domain/context/skill 的 `Effort\|ReservedSkill` 场景全部通过，31.72 秒，退出 0；包含真实 WS、双 Provider headless、恢复、摘要、Stop、故障和新 UI 回归，见 effort-acceptance-race.json 及 日志。 |
| Darwin effort PTY | 8/8 通过，双 Provider×颜色×取消/正常；新增真实 queued 输入、通知叠加及缩放后的物理行列断言，终端属性恢复，见 pty.json。 |
| 既有真实终端回归 | 推理控制 8/8、补全 2/2 通过，见 reasoning-controls-pty.json、completion-pty.json。 |
| OpenSpec | 当前 change 严格校验通过，全部主规格 60/60、全部 changes/specs 62/62 通过；六份 delta 的 14 条 requirement 与主规格当前正文逐段完全一致。见 change-strict.json、specs-strict.json、all-strict.json、spec-sync.json。 |
| 固定 fixture | 原 403 项中 394 项 bytes 不变，9 项显示快照按物理窗口修正，另新增 1 项 busy queue 快照；Provider/native/Session fixture 全部不变，未在测试运行时重建 current fixture。变更清单和原因见 fixed-fixtures-check.json。 |
| 参考与契约审计 | 两个参考 HEAD 与首次设计相同且 tracked diff 均为空；35 个涉及生产 Go 文件无新增可变全局，Session 生产文件、go.mod/go.sum 和 record vocabulary 未修改。见 reference-readonly.json、contract-audit.json。 |

复审修复未改变六项主规格已批准的能力范围，无需新增 delta 或同步新的能力声明；架构、pitfall、设计中的真实测试路径、任务和本验收记录均已更新。本轮证据独立保存在 `evidence/review/`，保留首次实现的完整记录。

本机原生执行平台为 Darwin；测试使用本地 HTTP/SSE/WebSocket 服务和假凭据，未调用真实远端模型，也未执行 Linux/Windows 原生终端验收，不据此承诺这些平台的交互结果。没有因检查失败而跳过或弱化测试，没有自动提交、发布或归档。

## 2026-11-15 再次回归与耗时结论

完整 `make verify` 保留全部质量门，仅通过临时 Go 包装增加 `-json -count=1` 以强制重跑并观测：普通全量 153.97 秒、race 全量 781.18 秒，总计 937.83 秒，退出 0。1,368 个源码/测试/构建输入前后完全一致，404 份固定 fixture 未变化；本轮未修改生产实现或测试断言。

耗时关键路径是 app 的 230 个顶层/975 个子测试在 race 下的串行大矩阵；大型 Session fixture 的重复恢复及严格 JSON 校验被 race 显著放大。app effort 相关 9 项顶层测试的 race 累计为 23.96 秒，约占该包用时 3.1%。没有发现死循环或无法退出的等待；另外观察到另一 worktree 同时运行全量 app race/profile，本轮时间不是隔离性能基准。详见 [完整结果、慢测试排名和 profile](regression-2026-11-15.md)、计时汇总、最终质量门。
- 变更保留供评审；未自动 commit、push、PR、发布或归档。
