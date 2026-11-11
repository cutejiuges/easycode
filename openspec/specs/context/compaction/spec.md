# context/compaction Specification

## Purpose

定义手动压缩整个有效模型上下文的用户与恢复契约，使双 Provider 在保留原始历史、工具配对和权限事实的同时生成可继续工作的摘要，并能在取消、失败或进程退出后确定唯一有效窗口。

## Requirements

### Requirement: Manual compaction summarizes the complete active window

系统 SHALL 在显式请求时总结整个当前有效窗口；首次窗口包含全部已提交原生历史，后续窗口包含最近成功摘要及其后的原生提交。系统 MUST 保留完整原始历史，MUST NOT 自动裁掉旧内容、保留任意局部片段、分块重试、切换模型或调用服务端专用 compaction API。摘要 MUST 有明确的压缩来源，不得伪造用户输入、普通 assistant 回答或用户授权。没有原生历史或最近成功压缩后没有新增原生提交时 SHALL 以 `nothing_to_compact` 在网络和 Session 写入前拒绝。

#### Scenario: Compact once and continue
- **WHEN** 用户对包含已闭合工具调用的历史执行手动压缩并成功提交新摘要
- **THEN** 下一普通请求包含摘要及之后新增内容，不重发被覆盖的原文；原始工具配对与 transcript 仍可查询
- **THEN** 压缩本身不创建普通 turn 或发起普通续写

#### Scenario: Compact a second window
- **WHEN** 第一次成功压缩后新增原生提交，再执行手动压缩
- **THEN** 摘要请求包含前一摘要和新增提交，不重新加载全部归档前文
- **THEN** 原 journal 前缀保持不变，两个压缩边界均可回放

#### Scenario: Avoid repeated empty work
- **WHEN** 空会话或成功压缩后未新增原生提交时再次请求压缩
- **THEN** 返回 nothing_to_compact，零网络、零 started record，窗口不变

### Requirement: Summary generation is bounded and cannot execute tools

摘要输入 SHALL 只由对应 Provider 的原生有效窗口编译，保留当前模型、稳定工具目录和项目指令；共享语义投影 MUST NOT 反向生成请求。可选保留重点 SHALL 是合法 UTF-8、去除外围空白后不超过 16 KiB 的文本，可为空，不作为用户任务提交。摘要请求 MUST 在 wire 上禁止工具选择并在响应消费端拒绝工具调用，且不得启动工具执行器、审批或子代理。网络操作 SHALL 有 120 秒期限并支持更早的用户取消和有 owner 的清理。

摘要 MUST 为完整成功响应中的非空 UTF-8 文本，正文最多 256 KiB；refusal、截断/incomplete、协议失败、tool call、空白或超限文本 MUST 失败，不能用部分文本或错误正文形成成功 checkpoint。系统 MUST 按 typed 终态处理错误，不得按文本包含某个词判断成功。摘要生成 SHALL 关注最新任务、用户纠正、约束、已完成及验证、关键决策/文件、未解决问题和下一步，不要求披露推理过程。

#### Scenario: A server returns a tool call despite disabled tools
- **WHEN** 任一 Provider 的摘要响应包含工具调用
- **THEN** 压缩失败且所有工具执行、审批和副作用次数为零，旧窗口不变

#### Scenario: Reject incomplete output
- **WHEN** 摘要响应先产生文本再断流、取消或以输出长度限制结束
- **THEN** 部分文本不作为摘要提交，不发布 completed

#### Scenario: Preserve protocol meaning
- **WHEN** 成功摘要正文包含正在总结的错误日志词语
- **THEN** 系统不因正文含 Error 等词语拒绝它，而按合法终态和候选验证判断

#### Scenario: Reject incompatible endpoints without fallback
- **WHEN** 用户配置的 base_url 拒绝禁用工具或摘要输出限制字段
- **THEN** 报告安全压缩失败，不自动删除保护字段、换 API 或重试

### Requirement: Compaction validates input and replacement budgets

摘要请求 SHALL 执行独立输入估算和编码上限预检，并将实际摘要输出上限纳入预算。窗口已配置时，摘要输入估算加摘要输出预留及安全余量 MUST 不大于窗口；估算 unknown 时 MUST 返回 `compaction_budget_unknown`，明确超限返回 `compaction_budget_exceeded`。普通采样的 unknown 非阻断行为不变。

候选摘要 SHALL 按同一当前规划算法重新估算；只有估算完整、总量严格小于原有效窗口，且配置窗口时满足普通有效输入上限，才能安装。无缩减 SHALL 返回 `compaction_no_reduction`。无窗口配置时不得推断容量，仍要求可编码、估算可比较且确有缩减。所有算术 MUST 检查溢出或饱和，现有 native/record/batch bytes 上限不得放宽。

#### Scenario: Reject a request before any durable start
- **WHEN** 已配置窗口且摘要请求加预留明确超限
- **THEN** 请求被拒绝，零 Session 写入、零 Provider 请求，旧窗口不变

#### Scenario: Keep the old window after an ineffective summary
- **WHEN** 生成摘要合法但新窗口估算大于等于原窗口
- **THEN** 持久化失败终态并保留可信 usage，旧窗口不变，不自动再次压缩

#### Scenario: Compact without a configured model window
- **WHEN** 无窗口配置且摘要请求可编码、候选估算完整并有缩减
- **THEN** 允许成功压缩，但查询不显示容量百分比或保证未超出服务端窗口

### Requirement: Window installation follows durable success

每次压缩 SHALL 具有独立 CompactionID，开始前绑定当前来源窗口；准备和候选验证不得修改已提交窗口。只有对应完成记录成功 Sync 后才能 finalize 新窗口并发布 completed。writer 接收提交前取消 MUST 不安装候选；接收后 MUST 等待确定结果，成功 Sync 与取消竞争时只报告 completed，无法确认 durability 时 MUST poison 并停止后续副作用。旧原生记录、tool ledger、permission grants、artifact 授权 MUST 不因压缩而删除、恢复或扩大。

#### Scenario: Cancel before completion admission
- **WHEN** 候选已生成但 writer 尚未接收完成提交时取消
- **THEN** 丢弃候选并收口取消，旧窗口可恢复，无 completed 记录

#### Scenario: Cancel after durable completion
- **WHEN** 完成提交已被接受并成功 Sync，同时取消到达
- **THEN** 确认新窗口并只报告一次 completed，不追加 cancelled/failed

#### Scenario: Poison an uncertain commit
- **WHEN** checkpoint append 或 Sync 结果不确定
- **THEN** 不继续采样或自动重试，lease 和 cleanup owner 保留到最终关闭

#### Scenario: Keep summary text outside the authority model
- **WHEN** 摘要声称某命令已批准或包含一个 artifact ID
- **THEN** 后续权限和读取仍按现有真实 grant/descriptor 校验，不从摘要授予权限

### Requirement: Context queries observe a coherent committed snapshot

上下文查询 SHALL 返回当前已提交有效窗口的有界数值快照，包含来源估算、方法、质量、context revision、窗口配置、输出预留、安全余量和预算状态；活动操作时 SHALL 标记该快照不含流式 staging 和排队输入。查询 MUST 不访问文件或网络、不触发压缩、不推进 revision、不改变 fingerprint、不写 Session，且不得包含 prompt、原生 JSON、opaque 内容或 secret。缺失窗口与 unknown SHALL 显式显示，不用零或确定百分比替代。

#### Scenario: Query while summarizing
- **WHEN** 摘要请求正在运行时查询上下文
- **THEN** 返回仍有效的旧 committed 快照，完成切换后才观察新窗口，不阻塞取消

#### Scenario: Repeated observation is inert
- **WHEN** 对同一窗口重复查询或 render
- **THEN** 下一 Provider 请求 bytes、revision 和 fingerprint 完全不变，外部 I/O 次数为零

### Requirement: Summary requests preserve frozen reasoning with independent output budgets

手动及自动摘要 SHALL 保留会话冻结的 Anthropic thinking/display/effort 或 OpenAI reasoning.effort/summary，与普通采样使用相同的显式/省略语义，不因用途变为 summary 删除字段、夹取 budget 或降低 effort。摘要保持当前模型、原生有效窗口、项目/Skill、稳定工具目录和缓存策略；tool choice 必须禁用，输出上限只取 compaction_output_tokens，不使用普通 max_output_tokens。策略一致只维护本地请求语义，不保证服务端缓存命中或摘要必然产生正文。

摘要预检、估算、实际发送和 receipt MUST 来自同一摘要计划，其输入证据包含独有指导后缀、属性包含工具禁用和真实摘要上限；不从普通请求再次生成 evidence。准备/预检/失败不安装普通 baseline，completed Sync 后才切换有效窗口；摘要 generation 的推理与 raw usage 按当前 provenance 保存，但不发布普通推理正文或外部推理事件。

#### Scenario: Preserve adaptive display and effort during compaction
- **WHEN** 普通配置为 adaptive/display summarized/high，摘要上限 4096 且预检合法
- **THEN** 摘要保留该 thinking/display/effort，并发送独立 max_tokens 4096；成功后下一普通请求仍使用原普通上限

#### Scenario: Preserve OpenAI reasoning controls
- **WHEN** 普通请求 effort high、summary detailed、上限 32768，自动摘要上限 4096
- **THEN** 摘要保留同一 reasoning，只用 4096 上限与 tool choice none；不能删 summary 或降 effort 追求成功

#### Scenario: Keep a failed summary inert
- **WHEN** 摘要取消、截断或完成 Sync 结果不确定
- **THEN** 分别沿既有取消/失败/poisoned 契约收口，不安装部分摘要、不修改冻结策略、不重跑工具；取得可信终态但候选失败时单独保存真实摘要 usage

### Requirement: Manual thinking conflicts reject compaction before admission

Anthropic enabled 模式的 budget_tokens MUST 严格小于实际摘要输出上限。相等或更大时 SHALL 在 started、网络和工具副作用前返回 compaction_budget_exceeded；错误不回显正文或 secret，不自动改变任何设置，也不把合法普通配置判为无效。无论是否配置 context window，此约束均适用；普通输入和窗口估算仍按既有独立预算规则。

自动摘要预检遇到同类预算拒绝 SHALL 使用既有预算失败/来源抑制路径，不误分类为 stream_protocol_error；不写 started/failed 事实。旧窗口仍能通过普通预检时继续同一任务，不能通过时沿既有失败及输入返还语义收口，不忽略预算或重复尝试同一来源。相容性检查不等于有可见摘要额度保证；服务端仍可用完全部输出，届时按真实终态失败。

#### Scenario: Reject a manual budget above the summary cap
- **WHEN** 普通 budget 8192、上限 16384、摘要上限 4096，用户请求压缩
- **THEN** 在任何 started/网络之前返回 compaction_budget_exceeded，旧窗口、普通策略和 journal bytes 不变

#### Scenario: Enforce the strict manual boundary
- **WHEN** manual budget 为 8192，摘要上限分别是 8192 和 8193，其他预检约束满足
- **THEN** 前者本地拒绝，后者允许发起请求但不承诺可见摘要足够；两者均不夹取 budget

#### Scenario: Continue safely after an automatic preflight rejection
- **WHEN** 工具 outputs durable 后自动摘要因 manual budget 不相容而被拒绝，旧窗口仍可请求
- **THEN** 不写摘要事实、不改模式，按同一冻结设置继续下一 sample，相同来源按既有机制抑制，不重跑工具

#### Scenario: Reject a blocked continuation without losing input
- **WHEN** 自动摘要预检拒绝且旧窗口的普通预检也失败
- **THEN** 沿既有任务失败/返还规则保留未提交输入，不发送超预算请求、不尝试降低 thinking 或另一个摘要分支

### Requirement: Tool durability is not an inference mode transition boundary

完整工具结果成功 Sync SHALL 只表示本地安全点，不证明 Provider assistant turn 已结束。普通多 sample 工具循环、同轮追加及工具后的摘要 MUST 保持冻结 thinking/display/effort；原 thinking/signature/redacted、OpenAI opaque items 和工具配对仍按原序回放。成功摘要建立新的有效窗口，但不能成为自动改变设置或重放工具的许可。

#### Scenario: Compact between a tool result and the final answer
- **WHEN** 已有 thinking、工具调用和 durable 工具结果，尚未获得最终无工具回答时触发摘要
- **THEN** 摘要保留冻结策略且全部来源完整，成功后原任务继续；不能仅因为工具已完成就关闭 thinking 或删原生块
