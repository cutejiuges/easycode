## MODIFIED Requirements

### Requirement: Summary generation is bounded and cannot execute tools

摘要输入 SHALL 只由对应 Provider 的原生有效窗口编译，保留当前模型、稳定工具目录和项目指令；共享语义投影 MUST NOT 反向生成请求。可选保留重点 SHALL 是合法 UTF-8、去除外围空白后不超过 16 KiB 的文本，可为空。启用 PreCompact 时，最终重点 SHALL 先保留用户规范化的手动重点，随后按冻结的检查顺序追加获准的摘要重点。整个组合 SHALL 计入同一上限。这些内容只供本次摘要请求使用，不作为普通用户任务提交。摘要请求 MUST 在 wire 上禁止工具选择并在响应消费端拒绝工具调用，且不得启动工具执行器、审批或子代理。网络操作 SHALL 有 120 秒期限并支持更早的用户取消和有 owner 的清理。摘要请求只能在既有基础预检通过、压缩 started 已持久化、PreCompact 已确定允许且最终请求预检通过后发出。压缩前检查使用独立有界额度，Provider 生成阶段禁止执行 Hook。

摘要 MUST 为完整成功响应中的非空 UTF-8 文本，正文最多 256 KiB；refusal、截断/incomplete、协议失败、tool call、空白或超限文本 MUST 失败，不能用部分文本或错误正文形成成功 checkpoint。系统 MUST 按 typed 终态处理错误，不得按文本包含某个词判断成功。摘要生成 SHALL 关注最新任务、用户纠正、约束、已完成及验证、关键决策/文件、未解决问题和下一步，不要求披露推理过程。

#### Scenario: A server returns a tool call despite disabled tools
- **WHEN** 任一 Provider 的摘要响应包含工具调用
- **THEN** 压缩失败，模型工具执行、普通工具审批和 Provider 响应引发的副作用次数为零，旧窗口不变

#### Scenario: Reject incomplete output
- **WHEN** 摘要响应先产生文本再断流、取消或以输出长度限制结束
- **THEN** 部分文本不作为摘要提交，不发布 completed

#### Scenario: Preserve protocol meaning
- **WHEN** 成功摘要正文包含正在总结的错误日志词语
- **THEN** 系统不因正文含 Error 等词语拒绝它，而按合法终态和候选验证判断

#### Scenario: Reject incompatible endpoints without fallback
- **WHEN** 用户配置的 base_url 拒绝禁用工具或摘要输出限制字段
- **THEN** 报告安全压缩失败，不自动删除保护字段、换 API 或重试

摘要端拒绝已发送请求、响应过长或流失败时 SHALL 结束当前压缩。它不能重复执行检查、裁剪原窗口或重发另一摘要。OpenAI 已有的生成请求发送前 WS 握手回退 SHALL 使用同一通过决定及同一冻结的完整摘要输入，保留原传输分类。

任务保真 SHALL 由已验证 typed 状态和确定性宿主包装实现，不能要求模型摘要回显全部列表后再把其文字当成事实。摘要请求仍只由对应 Provider 原生有效窗口编译，冻结的任务状态仅供候选 checkpoint 构造和核验，不从共享语义投影反向生成原生调用。真实 generation、其推理/usage 和宿主任务段 MUST 保持可区分；ProjectCheckpoint 的单向摘要继续返回真实模型摘要，不把宿主 task JSON 伪成模型生成文本。

#### Scenario: Separate generated summary from the exact task wrapper
- **WHEN** Provider 成功生成合法摘要，宿主随后将冻结任务快照包装进候选原生摘要
- **THEN** generation 的原始文本与 provenance 保持不变，任务段可独立从 typed 来源验证；任务段不是 assistant sample、user InputID 或新的 tool output

#### Scenario: Keep task handling inert while summarizing
- **WHEN** 摘要 generation 或已提交任务文本包含待办、工具名或操作指示
- **THEN** Provider 摘要生成阶段零任务更新执行器、审批、工具、Hook 或子代理调用，原清单来源不变化

#### Scenario: Bound summary focus to a single request
- **WHEN** 用户提供手动保留重点，已受信任的 PreCompact 声明显式授权发送合法摘要重点
- **THEN** 两家 Provider 的摘要请求都保留用户规范化重点在前，随后附加已授权的 Hook 重点，普通请求、普通用户输入、稳定前缀、清单及权限状态不因附加文本直接变化

### Requirement: Compaction validates input and replacement budgets

摘要请求 SHALL 执行独立输入估算和编码上限预检，并将实际摘要输出上限纳入预算。窗口已配置时，摘要输入估算加摘要输出预留及安全余量 MUST 不大于窗口；估算 unknown 时 MUST 返回 `compaction_budget_unknown`，明确超限返回 `compaction_budget_exceeded`。普通采样的 unknown 非阻断行为不变。基础预检 SHALL 对原始重点及当前摘要预算执行既有全部无副作用检查，即使存在匹配的 PreCompact 声明。存在追加重点时，PreCompact 完成后 SHALL 重新验证实际完整请求的估算与编码。原始重点保持原样时，基础预检已经覆盖最终请求。最终预检 MUST 使用同一来源、冻结的推理设置和任务状态，不得截断、静默丢弃已获准的重点或回退为未追加的请求。

候选摘要 SHALL 按同一当前规划算法重新估算；只有估算完整、总量严格小于原有效窗口，且配置窗口时满足普通有效输入上限，才能安装。无缩减 SHALL 返回 `compaction_no_reduction`。无窗口配置时不得推断容量，仍要求可编码、估算可比较且确有缩减。所有算术 MUST 检查溢出或饱和，现有 native/record/batch bytes 上限不得放宽。

#### Scenario: Reject a request before any durable start
- **WHEN** 已配置窗口且基础摘要请求加预留明确超限
- **THEN** 拒绝本次压缩，零压缩及检查记录、零 Hook 命令、零摘要网络调用，旧窗口不变。自动路径已有的 turn/tool/Stop 事实继续有效

#### Scenario: Reject focus that exceeds the final compaction budget
- **WHEN** 基础预检通过，但已授权的 Hook 重点使最终组合超过总文本、编码或窗口预算
- **THEN** 以持久化 `compaction_budget_exceeded` 终止，摘要计量为 unavailable，Provider 摘要请求次数为零，旧窗口和任务状态保持原值

#### Scenario: Keep the old window after an ineffective summary
- **WHEN** 生成摘要合法但新窗口估算大于等于原窗口
- **THEN** 持久化失败终态并保留可信 usage，旧窗口不变，不自动再次压缩

#### Scenario: Compact without a configured model window
- **WHEN** 无窗口配置且摘要请求可编码、候选估算完整并有缩减
- **THEN** 允许成功压缩，但查询不显示容量百分比或保证未超出服务端窗口

候选检查 MUST 包含完整原生摘要包装及 Task 段的真实编码和 footprint，不能只检查模型 generation 或计数确认。任务最多二十项及既有文字/规范快照上限仍适用；完整 wrapper、checkpoint、record/batch 的现有边界不得放宽。合成候选过大、unknown、没有缩减或超窗口时，压缩失败并保留旧窗口；不裁剪任务、不丢 activeForm/说明、不把部分清单声明为成功，也不自动重试另一摘要。

#### Scenario: Reject an oversized combined summary and task snapshot
- **WHEN** generation 与任务快照各自合法，但完整包装使候选 checkpoint/record 编码超限或无法缩减原窗口
- **THEN** 沿对应预算/候选失败路径保留旧窗口，不安装部分列表、不放宽大小限制，真实摘要计量仍按原来源保存

#### Scenario: Use native footprint to cover the complete wrapper
- **WHEN** 语义摘要仍只含模型生成文本，而原生包装另含完整任务快照
- **THEN** 候选普通预算及缩减比较使用覆盖完整原生 footprint 的计划，任务段计量一次且不能漏算

摘要候选规划 SHALL 在同一 committed_history 元数据中使用该候选冻结任务状态的内容 stamp；依照 context/planning，none 省略 task_content_digest，clear/snapshot 使用 SHA-256(CanonicalJSON({state, update})) 的 64 位小写十六进制摘要，排除所有来源身份与 facade profile。它不改变 generation、不增加 source/网络正文或 token；成功压缩前后的估算仍由语义/完整原生 footprint 的覆盖值决定。相同 revision/token/summary 下不同任务不能因估算相同而共享历史内容 fingerprint。

#### Scenario: Plan equal-footprint candidates with different tasks
- **WHEN** 两个压缩候选的真实模型摘要、context revision 和完整原生估算相同，规范任务内容不同
- **THEN** 候选历史 fingerprint 不同，估算各只覆盖实际完整原生包装一次，任务来源身份不进入 stamp

### Requirement: Summary requests preserve frozen reasoning with independent output budgets

手动及自动摘要 SHALL 保留本次操作捕获的 Anthropic thinking/display/effort 或 OpenAI reasoning.effort/summary，与普通采样使用相同的显式/省略语义，不因用途变为 summary 删除字段、夹取 budget 或降低 effort。自动摘要继承活动 turn 捕获的策略；独立手动摘要在纯预检接纳时捕获当前选定策略。任一活动操作期间后续接受的 effort 更新 MUST NOT 改变该摘要的预检、实际请求或收据。

摘要保持当前模型、原生有效窗口、项目/Skill、稳定工具目录和缓存策略；tool choice 必须禁用，输出上限只取 compaction_output_tokens，不使用普通 max_output_tokens。策略一致只维护本地请求语义，不保证服务端缓存命中或摘要必然产生正文。

通过 PreCompact 后的最终摘要预检、估算、实际发送和 receipt MUST 来自同一摘要计划，其输入证据包含实际发送的摘要指导后缀，属性包含工具禁用和真实摘要上限。不得从普通请求再次生成 evidence，也不得使用检查前计划描述不同正文。准备/预检/失败不安装普通 baseline，completed Sync 后才切换有效窗口；摘要 generation 的推理与 raw usage 按当前 provenance 保存，但不发布普通推理正文或外部推理事件。成功摘要不得在活动 turn 内应用后续 effort，既有 manual budget 冲突、自动来源抑制及三次熔断规则保持。

独立摘要的 starting/running/finishing 以实际 started/terminal/cleanup 观察推进，terminal 后清理完成前仍持有原捕获策略。摘要失败不撤销已接受的未来选择，不安装普通成功 baseline；既有摘要连接隔离、关闭和 full 重建规则仍生效，不能承诺保留摘要前 socket。系统 MUST NOT 参照其他产品的摘要优化自动禁用 thinking、降低 effort 或扩大额度。context 查询的 ordinary committed 估算不得标为真实摘要输入或 usage。

#### Scenario: Preserve adaptive display and effort during compaction
- **WHEN** 普通操作策略为 adaptive/display summarized/high，摘要上限 4096 且预检合法
- **THEN** 摘要保留该 thinking/display/effort，并发送独立 max_tokens 4096；成功后当前 turn 的下一普通请求仍使用原策略和普通上限

#### Scenario: Preserve OpenAI reasoning controls
- **WHEN** 普通操作 effort high、summary detailed、上限 32768，自动摘要上限 4096
- **THEN** 摘要保留同一 reasoning，只用 4096 上限与 tool choice none；不能删 summary 或降 effort 追求成功

#### Scenario: Keep a failed summary inert
- **WHEN** 摘要取消、截断或完成 Sync 结果不确定
- **THEN** 分别沿既有取消/失败/poisoned 契约收口，不安装部分摘要、不改变操作策略、不重跑工具；取得可信终态但候选失败时单独保存真实摘要 usage

#### Scenario: Select a future effort during automatic compaction
- **WHEN** high 工具回合的自动摘要正在进行，low 设置被接受
- **THEN** 摘要和该回合后续普通请求仍为 high，low 只影响下一个独立操作，旧摘要失败计数不清零

#### Scenario: Keep manual admission and execution consistent
- **WHEN** low 手动摘要通过基础预检并被接纳，在 PreCompact 审查或执行期间 reset 恢复启动 high
- **THEN** 当前摘要的最终预检、预算、编码、evidence 和 receipt 仍描述 low，实际发送正文与审批后计划一致，下个独立操作才使用 high

#### Scenario: Keep selection and summary failure independent
- **WHEN** high 自动摘要期间接受 low，摘要失败后旧窗口仍可请求
- **THEN** 当前回合继续使用 high，后续选择仍为 low；摘要失败不使选择回滚，也不恢复已经失效的连接资格

#### Scenario: Hold a manual policy through final cleanup
- **WHEN** high 手动摘要 terminal 已发布，而清理被确定性暂停，后续选择已是 low
- **THEN** 查询仍显示 high/manual_compaction/finishing，原 CompactionID 保留；清理完成后才显示 idle 或启动下一操作
