## 1. Token estimate and planning contracts

- [x] 1.1 在 `internal/domain` 实现强类型、不可变的 token estimate 与 native history footprint，包含 method/state/token/revision/family 校验和 typed constructors；通过 domain 单测验证 unknown 不等于零、非法 family/method/state 被拒绝、getter 不泄露可变数据。
- [x] 1.2 在 `internal/context/estimate` 实现版本化 `byte_heuristic_v1`、结构开销和饱和/溢出运算；通过表驱动单测验证空值、UTF-8、多字节数据、边界除法与 `uint64` 极值不会 wraparound。
- [x] 1.3 在 `internal/context` 实现 Budget、ProviderProfile、PlanningInput、PlannedSource、ContextPlan 和纯内存 Planner，严格生成 `provider_profile -> committed_history -> current_input`；通过单测验证来源校验、family/method 匹配、max 去重、unknown 传播、深拷贝及重复规划完全一致。
- [x] 1.4 将三个来源映射到既有 cache `Plan` 并生成 canonical JSON；通过 cache regression 验证 profile 为 stable、history 为 turn-stable、input 为 volatile，修改当前输入不改变 stable prefix fingerprint，API key/base URL/cwd/时间/Session identity 均不进入稳定输入。
- [x] 1.5 覆盖预算判定的 `not_enforced`、`within_limit`、`over_limit`、`indeterminate` 四种状态；通过边界测试验证等于有效上限可通过、只有完整已知且大于上限才超限、错误摘要不包含 prompt 或 secret。

## 2. Explicit configuration

- [x] 2.1 扩展严格 JSON decoder 和 `config.Config`，支持三个可选 unsigned integer 预算字段且保持既有字符串字段、重复字段和 unknown-field 规则；通过配置单测验证完整配置、省略 reserves、environment-only 兼容和无新增环境变量覆盖。
- [x] 2.2 实现 budget 组合校验，拒绝零窗口、负数、非整数、超范围、reserve 无 window、reserve 总和溢出或大于等于 window；通过单测验证统一返回英文 `invalid_configuration` 且错误不回显配置正文或凭据。
- [x] 2.3 扩充 app/config 集成测试，证明非法预算在 Provider factory/网络请求之前失败、合法或省略预算可完成既有装配；验证配置文件权限、symlink/同句柄校验和 `--version` 行为未回归。

## 3. Provider-native history footprint

- [x] 3.1 为 `provider.Conversation` 组合最小 `HistoryFootprinter` 契约并更新共享 fake/compile-time assertions；通过 provider 包测试验证 footprint 是纯值、family/revision/method/state 均经过校验且不暴露 native item 或 raw bytes。
- [x] 3.2 为 Anthropic Conversation 实现 committed native history scorer 和单调 revision，覆盖 text、thinking/signature、redacted thinking、unknown block 与结构开销；通过单测验证 staging/失败 sample 不计入，durable finalize 后恰好推进一次。
- [x] 3.3 为 OpenAI Conversation 实现 committed native history scorer 和单调 revision，覆盖 message content、phase、reasoning summary、encrypted content、unknown item 与结构开销；通过单测验证 staging/失败 sample 不计入，durable finalize 后恰好推进一次。
- [x] 3.4 为两个 Provider 增加 uninterrupted/restored footprint 等价测试，证明相同 commits 的 revision/method/state/tokens 一致，损坏 commit 无法产生部分 Conversation；同时验证语义投影仍不包含 opaque 数据。
- [x] 3.5 使用现有 request golden/native fixtures 增加 footprint 前后回归，证明读取一次或多次 footprint 不改变下一请求的 native item 顺序、canonical bytes 或 fingerprint；若 golden 正文出现差异，先修复 RequestCompiler 回归而不是直接接受 fixture 更新。

## 4. Runtime integration

- [x] 4.1 新增英文 `context_limit_exceeded` fault code，并在 Runtime Config 中注入已校验 profile、budget 和 planner；通过构造测试验证 `New` 仍是纯内存操作，并拒绝 Conversation/profile family 不匹配和非法 planner 配置。
- [x] 4.2 在 durable `turn_started`/emit 之后、`Conversation.Stream` 之前读取两种历史快照并构造 ContextPlan；通过 timeline 测试验证明确超限按 `turn_started -> turn_failed` durable 收口、Stream 调用次数为零且 native history 未改变。
- [x] 4.3 覆盖 planner error、`not_enforced`、`within_limit` 和 `indeterminate` 路径；通过 Runtime 测试验证 error 路径不联网，后三种路径继续使用既有 Provider-native RequestCompiler，取消、并发 turn 和 poisoned journal 语义不回归。
- [x] 4.4 更新 app/session 装配以从最终配置传递 family/model/budget，并覆盖新建与恢复会话；通过集成测试验证 restored 路径规划成功且 request canonical bytes 与 uninterrupted 路径等价。
- [x] 4.5 增加协议与 Session 回归断言，证明本变更没有新增 RuntimeEvent kind、Session record/payload revision、headless JSON 字段或 native payload revision，现有 fixture 无需迁移或重写。

## 5. Documentation and contract consistency

- [x] 5.1 更新总体架构与产品 Roadmap，记录 ContextPlanner、HistoryFootprinter、显式预算和后续 project instructions/tools/skills/cache/compaction 的依赖关系；通过文档审查确认未把远程计数、缓存执行或压缩写成已完成。
- [x] 5.2 检查并按实际实现更新 pitfall log，至少记录“normalized usage 不能累计成上下文占用”“semantic/native estimate 不能相加”“模型名不能推断窗口”；通过路径和术语核对确认与代码、spec、record vocabulary 一致。
- [x] 5.3 复核 change artifacts 与实现，确保任何范围扩大、接口方向变化或估算语义调整都先更新 proposal/spec/design/tasks；通过 `openspec validate add-deterministic-context-planning --type change --strict --no-interactive` 验证工件一致。

## 6. Verification and delivery

- [x] 6.1 运行 context、config、provider、runtime、app 的定向测试及相关 golden/cache/restore regression，验证全部新增场景通过且测试不访问真实外网或 API key。
- [x] 6.2 运行 `make verify`，确认 gofmt、go vet、Staticcheck、架构边界、全量测试和 race test 全部通过；若有无法执行的检查，在交付说明中列明原因与风险而不是声称通过。
- [x] 6.3 审查最终 diff，确认没有修改或依赖构建两个只读参考工程，没有 package-level mutable state、`any` 核心契约、secret/prompt 日志、无用代码、失效 flag 或未授权 schema/event 能力。
- [x] 6.4 创建 commit 或 PR 前逐项检查双语格式：标题、正文和每个列表条目均使用 `English // 中文翻译`；通过实际 commit message/PR description 预览确认不存在单语标题或条目。
