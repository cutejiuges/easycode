# 启动推理控制归档核对（2026-11-11）

本次按用户明确请求，将 `add-provider-reasoning-controls` 的九份 delta 同步到主规格，并在归档前按当前 `AGENTS.md` 逐项核对实现、测试、架构、Roadmap、pitfall 与实际持久化词汇。全面回归的两处生产修复、四份新增测试文件及原始日志一并纳入。归档阶段未提交或发布 Git 变更，后续提交与 PR 由用户的独立交付请求触发。

归档目标为 `openspec/changes/archive/2026-11-11-add-provider-reasoning-controls/`；schema 为 `spec-driven`。原 40 项实现任务已完成，追加的回归与归档任务保留独立编号。原实施与回归的历史说明保留当时范围，最终状态以本记录和归档后校验结果为准。

## 规格、实现与测试对照

| Capability | 当前实现边界 | 实际验证入口 |
| --- | --- | --- |
| `config/json-file` | JSON-only 五字段、thinking/display 判别联合、最终 family 与普通输出/窗口预留校验；装配资源前冻结 | `internal/config/reasoning_test.go`、`internal/domain/inference_settings_test.go`、`internal/app/reasoning_config_test.go` |
| `context/compaction` | 普通/摘要沿用同一冻结策略；摘要独立 cap、manual 严格小于 cap；预算拒绝不写 started；工具 durable 不代表远端 turn 结束 | 两家 `compaction_stream_test.go`、`internal/runtime/reasoning_compaction_test.go`、`internal/app/reasoning_auto_compaction_test.go` |
| `context/planning` | 非默认普通策略进入 `request_policy`；缺省 profile bytes 不变；usage 和 UI 来源不进入 prompt/fingerprint | `internal/context/reasoning_test.go`、`planning_test.go`、`cache_plan_test.go` |
| `provider/anthropic-messages-text` | thinking/display/output_config 原生编译，缓存副本保留；thinking 计量独立观测、明确零更新、raw 当前结构 | `internal/provider/anthropic/reasoning_request_test.go`、`usage_test.go`、`usage_validation_regression_test.go` |
| `provider/openai-responses-text` | effort/summary 独立省略，HTTP/WS 保留同一逻辑设置，ReasoningSummary 按实例配置声明 | `internal/provider/openai/reasoning_request_test.go`、`websocket_test.go`、`usage_validation_regression_test.go` |
| `provider/reasoning-control` | 原生计划同源产生 body/properties/input hash/receipt，恢复等价；完整帧校验先于归并和终态，失败不降级/重试 | 两家 `reasoning_request_test.go`、`reasoning_restore_test.go`、`internal/app/reasoning_controls_e2e_test.go`、`reasoning_cli_e2e_test.go`、`reasoning_malformed_stream_test.go` |
| `provider/reasoning-display` | 仅显示返回白名单；display 选择、正文和计量分开，阅读不改变请求；opaque 不进入 headless | 两家 `reasoning_stream_test.go`、`internal/tui/inference_test.go`、`internal/headless/reasoning_test.go`、`scripts/reasoning-controls-smoke.py` |
| `provider/usage-accounting` | 五指标三态，Anthropic reasoning_output 为 known/unknown；当前 thinking_tokens 状态必需；repair 前核对 raw/normalized；重复 wire 成员不选后值 | `internal/app/reasoning_usage_restore_test.go`、`internal/runtime/anthropic_usage_test.go`、两家 `usage_validation_regression_test.go` |
| `tui/context-commands` | app 纯内存装配冻结值及来源；有界滚动、窄屏、NO_COLOR、Esc；外部 snapshot 词汇不扩张 | `internal/tui/context_test.go`、`inference_test.go`、`internal/app/reasoning_config_test.go`、`scripts/reasoning-controls-smoke.py` |

九份 delta 合并结果为新增 14 条、修改 8 条 requirement，共覆盖 115 个 delta scenario。新增 `provider/reasoning-control` 主规格沿用其明确 Purpose；其余八份主规格原 Purpose 不变。未涉及的 41 条 requirement 原文保留，所有原 MODIFIED 场景保留，不复制 delta 操作标题进入主规格。逐块比较及重复同步预检证明无剩余待合并内容。

## 本轮回归修复完整性

- 两个 reducer 在类型解码前调用已有固定 `codec.ValidateJSON`，先验证完整事件再改变状态，拒绝重复字段覆盖计量、总量关系或响应 ID；合法未知扩展继续接受。生产代码只修改 `internal/provider/anthropic/reducer.go` 与 `internal/provider/openai/reducer.go`，没有新增依赖或格式。
- 两家 `usage_validation_regression_test.go` 保留 8 个修复前失败复现；Anthropic 还覆盖非法整数、正值/明确零/缺失连续观测和独立判定 fuzz。
- `internal/app/reasoning_malformed_stream_test.go` 验证双 Provider × 三种 headless 模式：完整工具参数之后出现异常 usage，也没有成功 sample、工具执行、重试或私有内容泄露。
- `internal/app/reasoning_cli_e2e_test.go` 在 24 组真实子进程场景验证工具循环、独立进程恢复/流式继续、请求 bytes、原生与归一化证据，以及参数拒绝和无资源副作用的非法配置。
- 修复前后日志、32 个 PTY 场景、5 轮乱序 race、421,051 次 fuzz、81.9% 定向覆盖率和清除测试缓存后的完整质量门见 [全面回归记录](regression-2026-11-11.md)。这些是前一轮实际执行结果；归档阶段的检查另行记录，不将缓存结果冒充再次执行全部场景。

## 架构与实际 record vocabulary

核对 `internal/domain/inference_settings.go`、`internal/config/inference.go`、`internal/app/app.go`、两家 request/compaction/evidence/reducer、`internal/codec/validation.go` 和 `internal/session/types.go`：领域值与请求计划保持纯内存，配置读取和生命周期仍由既有边界拥有；完整帧校验复用已有 codec，不增加全局状态、公共接口、goroutine 或 owner。原生历史继续为续写事实源，共享语义投影不反向构造请求。架构守卫仍覆盖依赖方向、唯一当前契约、纯构造、包级变量与生产可达性。

Session 的实际 record kind 使用 `turn_inputs_committed`、`turn_started`、`provider_native_commit`、`sample_usage`、`tool_call_ready`、`tool_call_result`、`turn_completed`/`turn_failed` 及 `compaction_started`/`compaction_completed`/`compaction_failed`。`user_inputs` 是 native commit 的现有 kind，不是新增 Session record；`turn.failed` 是 headless 外部 JSONL v1 名称，不能与内部 `turn_failed` 混写。`thinking_tokens` 是 Anthropic raw usage 的必需状态成员，`reasoning_output` 是已有 normalized 指标；没有新增第六指标、Runtime kind、外部输出字段、版本路由或历史 reader。

恢复与损坏验证仍使用仓库固定 current bytes。异常 sample 不提交 native/usage、不触发工具；已保存用户输入和先前成功事实保留。设置不回写 journal，`Sync`、取消、poisoned 和 cleanup owner 沿用当前契约。完整请求和缓存 golden、opaque 原生顺序、默认 profile 身份保持既有断言。

## 文档对照

- README 概览、P1 状态和剩余目标已改为“启动控制已交付、运行中切换未实现”，补入真实 CLI/异常流验证入口与归档链接。
- 总体架构补入完整帧校验边界，修正视图章节仍把主动请求列为未实现的旧描述；启动冻结、摘要预算、同源证据和计量语义保持一致。
- 当前能力清单与 Roadmap 的核对日期更新至 2026-11-11，记录修复、真实测试范围和归档证据；没有将本切片写成 P1/P5/P6 整体退出。
- pitfall 新增“类型反序列化前遗漏完整事件帧校验”，解释后值覆盖的根因、复用现有 codec 的修复、合法扩展边界和实际回归入口。
- 相关文档改用归档路径。归档目录内部的报告和日志均用相对链接，随 metadata 和 artifacts 整体保留；本轮新增的四份测试文件仍在源码树中。

## 归档阶段验证

主规格严格校验已通过 55/55。归档阶段再次执行 `make verify` 退出 0；其中 Go 测试结果复用本轮先前完整执行产生的缓存，Go 源码未变化。清理测试缓存后的全量执行证据仍为 [回归质量门](regression-2026-11-11/make-verify.log)，本阶段实际命令输出另存 [归档质量门](archive-make-verify.log)。

最终 delta/main 逐块一致、主规格原 Purpose/无关 requirement 保留、任务完成、当前分支命名、文档链接/源码路径、实际 record 词汇与 `git diff --check` 均作为归档准入检查。完整清单及归档后严格校验结果随本记录保留。

归档前全库严格校验为 57/57（2 个活动 change、55 份主规格），零失败；[原始结果](archive-before-validation.json)。归档路径预检覆盖 116 个本地链接、29 个文档和 29 个明确测试路径，并核对 815 份 Go 文件在归档复核期间未改变；结构化结果见 [准入核对清单](archive-checks.json)。新增状态说明链接会在移动后再次检查。

归档已完成，原 change 目录已移至上述归档路径；移动时 43 个文件逐个核对 SHA-256，包含隐藏 metadata、九份 delta、回归报告与全部日志。45/45 tasks 完成，无未完成工件或任务警告。归档后全库严格校验为 **56/56 通过**（1 个其他活动 change、55 份主规格），零失败；[原始结果](archive-after-validation.json)。主规格单独校验结果见 [55/55 主规格检查](archive-main-specs-validation.json)。最终链接、词汇、源码不变性和九份 delta/main 比对见 [归档后核对](archive-final-checks.json)。

实际验证平台为 macOS 与本地模拟服务，未连接真实模型；没有新增 Linux/Windows 原生终端或服务端支持承诺。运行中 `/effort`、远端探测、自动降级、跨 Provider fork、MCP 和 Subagent 仍为后续能力。
