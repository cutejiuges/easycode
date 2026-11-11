# 启动推理控制验收记录

2026-11-11 补充：已完成新一轮全面用例、端到端与回归测试，并修复双 Provider 对重复 JSON 成员的接受问题。新增测试、修复前后证据、32 个 PTY 场景及重新执行的完整质量门见 [全面回归记录](regression-2026-11-11.md)。以下保留原实施验收结果。

2026-11-11 归档补充：九个 capability 已同步主规格，本轮回归修复同时纳入 proposal/design/specs/tasks、架构、Roadmap、能力清单和 pitfall；归档核对与本阶段检查见 [archive.md](archive.md)。原文中“未同步/未归档”保留原实施阶段的时间边界。

## 范围与环境

变更：`add-provider-reasoning-controls`；分支：`feat/provider-reasoning-controls`；基线 HEAD：`27be6353d4c50f70f214ab9a308ed0fba61c3729`。本轮依据已确认的九份 delta 实施，没有修改两个只读参考工程、增加依赖或引入新 Runtime kind。当前验证平台为 macOS 27.0.1 arm64；HTTP/SSE/WS 使用本地 fixture 与合成凭据，不访问真实模型。

选择这个切片，是因为已有原生推理保存/阅读、工具、缓存和恢复已形成闭环，启动控制可以直接复用它们。Claude Code 的原生 thinking/独立 effort 与紧凑只读交互、Codex 的 effort/summary 分离和请求属性准入分别体现在两家原生编译及共享生命周期中；没有复制账号鉴权、模型名称启发式或自动降级分支。参考快照、官方 API 调研和取舍见 [design.md](design.md)。

## 契约与实现对照

| Delta | 实现与真实消费者 | 核心验证入口 |
| --- | --- | --- |
| config/json-file | `internal/domain/inference_settings.go`、`internal/config/inference.go`，最终 family 后校验、启动冻结 | `internal/domain/inference_settings_test.go`、`internal/config/reasoning_test.go`、`internal/app/reasoning_config_test.go` |
| provider/reasoning-control | 两家 `user_input.go`、`request.go`、`request_evidence.go` 的原生用途计划；app 装配和 capability 核验 | 两家 `reasoning_request_test.go`；`internal/app/reasoning_controls_e2e_test.go`、`internal/app/questions_e2e_test.go` |
| provider/anthropic-messages-text | thinking/display/output_config、三种缓存副本、原生 reducer/codec | `internal/provider/anthropic/reasoning_request_test.go`、`usage_test.go`、`compaction_stream_test.go` |
| provider/openai-responses-text | reasoning 成员独立省略、输出 cap、HTTP/WS full/incremental 和允许的回退 | `internal/provider/openai/reasoning_request_test.go`、`websocket_test.go`、`compaction_stream_test.go` |
| provider/reasoning-display | 启动选择与白名单返回正文分开；阅读不改策略 | 两家 `reasoning_restore_test.go`；`internal/runtime/anthropic_usage_test.go`、`internal/tui/inference_test.go`；两个 PTY 脚本 |
| provider/usage-accounting | thinking_tokens 的 known/unknown、必需 current 状态、同批 Sync 与 repair 前核对 | `internal/provider/anthropic/usage_test.go`、`internal/app/reasoning_usage_restore_test.go`、`internal/runtime/anthropic_usage_test.go` |
| context/planning | 可省略 request_policy、默认 bytes/revision 保持、内容派生非默认身份 | `internal/context/reasoning_test.go`、`internal/context/planning_test.go`、`internal/context/cache_plan_test.go` |
| context/compaction | 同一冻结策略、独立输出、typed manual budget 拒绝和既有自动抑制路径 | 两家 `compaction_stream_test.go`；`internal/runtime/reasoning_compaction_test.go`、`internal/app/reasoning_auto_compaction_test.go`、`internal/app/compaction_e2e_test.go` |
| tui/context-commands | 应用值装配 `/context`，不扩张外部 payload；有界滚动与 Esc | `internal/tui/context_test.go`、`inference_test.go`；`internal/app/reasoning_config_test.go`；`scripts/reasoning-controls-smoke.py` |

## 请求、缓存与固定字节

- 默认 Messages/Responses、项目指令、缓存和普通 WS 原有请求 golden 保留；新 golden 位于两家 `testdata/reasoning_requests.golden.jsonl`，分别包含 54 和 36 个逻辑请求/传输投影案例。OpenAI 的 websocketBody 投影单测不构成摘要增量准入，真实摘要网络入口始终 full，并由 compaction/WS 网络回归单独核验。OpenAI 另外固定 `reasoning_websocket_full.golden.json`、`reasoning_websocket_incremental.golden.json`。
- body、properties、输入 fingerprint 和 receipt 从同一执行计划生成。properties 排除 input/messages；逻辑输入包含项目/Skill/摘要指导，排除 Anthropic marker。窗口来源 digest 独立核验；预检后来源变化在网络前失败。
- 恢复/阅读前后比较完整请求 bytes、原生顺序、输入/请求 fingerprint 及 profile/cache sources；OpenAI 新连接按 full 比较，连接内 suffix 与 retained prefix/output 重建完整逻辑输入。404/405/426 安全回退保留设置与完整逻辑 receipt；400/401/403/429 与生成写入后错误不重试。
- Anthropic 原生恢复 fixture 为 `internal/provider/anthropic/testdata/reasoning_restore.current.jsonl`；Session 下 14 份受影响 Anthropic current journal 已替换必需 thinking 状态、normalized 指标与 checksum。新增 `reasoning_usage_positive_anthropic.jsonl`、`reasoning_usage_zero_anthropic.jsonl`，未知状态使用既有 `compaction_completed_anthropic.jsonl`。所有恢复基准为仓库固定 bytes，不在测试运行时调用当前 encoder 生成。
- 损坏用例从固定 bytes 定点改写并重新计算独立 checksum，删除 thinking 字段或令 raw:30 与 normalized:0/unknown/not_applicable 冲突，再附加 torn tail。resume/preview 均失败，文件逐字节不变；没有旧 reader、版本门禁或迁移。

## 生命周期与计量

真实双 Provider Read 工具后、最终回答前触发自动摘要，验证来源包含 reasoning 与有序调用/结果、策略保持、tool choice none、独立 cap，普通 turn 计量为 60/80、摘要独立 30/40。manual 8192 对摘要 4096/8192 在预检拒绝，8193 通过该项检查；合法普通 16384 启动不受摘要默认 4096 影响。自动拒绝不写 started/failed，旧窗口可用则继续，超限则关闭失败并保留先前 sample usage。

Anthropic 正值、明确零、缺失/保留观测、坏类型/负数/溢出/超总量，以及 output unknown、thinking known 均有测试；不从空正文、display 或 budget 推断。真实 Provider 的 raw/native/normalized 在同一 sample batch 进入 Sync，提交前不发布；Sync uncertain 不安装样本并 poison。摘要候选无缩减与 completion Sync uncertain 保留可信 observed usage；无可信终态沿已有 unavailable 路径。多 sample known 求和、unknown 传播，output 不重复加 thinking。

Read → Question → 批准 Write → 最终回答保留每 sample 设置、原生配对和一次执行断言；固定 UTF-8/SSE chunk、活动推理取消/断管、部分工具参数与阻塞 shutdown 使用现有确定性门闩/故障注入。text/json/stream-json 外部字段及 v1 不增加，允许正文与 opaque 哨兵不进入输出，已有 reasoning_output 按新 known/unknown 语义交付。

## 终端验证

- `python3 scripts/reasoning-controls-smoke.py`：双 Provider × NO_COLOR 开/关 × 完成/取消，共 8 个真实 PTY 场景通过。检查 `/context` 冻结来源、`/cache` 正 thinking usage 的可达滚动、Ctrl+O/r、窄屏 resize、真实工具续写、OpenAI 工具后摘要、Anthropic manual 预算拒绝、实际请求数和终端属性恢复。
- `python3 scripts/reasoning-smoke.py`：原有双 Provider × NO_COLOR × 完成/取消的 8 个场景通过，保留原有保存不确定状态与 cursor handoff 断言。新脚本复用其 PTY 驱动，没有重复建一套 owner。
- 真正终端验收范围仅为上述 Darwin；不代表 Linux/Windows 终端或真实 endpoint 支持矩阵。

## 架构与安全复核

设置和计划只做内存值操作；未增加生产 goroutine、锁、全局 registry、可变 codec、任意参数透传或跨层依赖。原生 wire 留在各 Provider，semantic projection 不反向编译请求；用途计划复用各自 wire 类型，不引入通用 builder 框架。仅新增有真实消费者的 SampleUsageValidator 和预算错误分类。源文件注释为中文，公共错误为固定英文，不回显非法值或 secret。配置和 display 标签不持久化；usage 不进入 prompt/profile/fingerprint；opaque 字段只沿既有 native 边界保存。

README、总体架构、能力清单、Roadmap 与 pitfall 已按实际字段、计量与平台边界同步。本轮 apply 不执行 Git commit、主规格 sync 或 archive；后续使用独立流程。

## 完整质量门

- `make verify`：exit 0。包含分支策略回归、gofmt、go vet、Staticcheck、架构守卫、全量测试和全量 race；完整输出见 [make-verify.log](make-verify.log)。race 的 app 包实际用时 421.014 秒，不代表性能目标。运行期间及其后未修改被验证的 Go 源码。
- `openspec validate --all --strict --json --no-interactive`：56/56 通过（2 个 change、54 个主规格），0 失败，无 WARNING/ERROR；既有长 requirement 仅 INFO。
- 当前分支命名检查、`git diff --check`、九份 delta/capability 路径与任务逐项对照、Markdown 本地链接检查均通过。默认 request/缓存/WS golden、go.mod/go.sum、主规格和参考工程未修改。
- 40 项 tasks 全部完成；主规格 sync 和 archive 保留给独立后续流程，不把规划齐备或旧基线质量门当作本轮功能验收。
