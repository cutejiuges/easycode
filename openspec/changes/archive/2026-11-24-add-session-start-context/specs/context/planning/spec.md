## ADDED Requirements

### Requirement: SessionStart context is budgeted in the effective history

已接纳的 SessionStart 上下文 SHALL 作为原生有效窗口中的独立历史内容参与 committed_history。规划 SHALL 使用同一窗口的原生 footprint 与只读语义投影，按既有覆盖估算覆盖实际规范包装一次，保留摘要、Hook 来源、真实用户输入及附件的顺序。上下文来源的固定顺序及生命周期沿用现有契约，current_input 保持无额外正文。内容指纹 SHALL 随实际包装字节及历史位置变化。运行请求、授权决定、执行统计、时间、cwd、Session 路径、CompactionID、TurnID 和 journal seq MUST NOT 注入缓存语义内容。来源类型用于分类实际内容和定位 committed_history 变更。未提交、未授权或跳过的上下文不能刷新 committed revision 或 request receipt。

startup/resume SHALL 在既有首 sample 自动压缩判断和执行完成后，与已冻结的真实首输入准备同一候选，使用本次实际 profile、推理配置、权限模式目录、语义视图和原生 footprint 校验模型请求与完整 journal 批次预算。真实输入自身 SHALL 在该激活的命令或审查前先通过纯 preflight。组合候选触发确定的 typed 窗口、Provider canonical、记录或批次额度时，SHALL 丢弃整份上下文，在同一窗口用相同真实输入至多重新预检一次。可提交时沿用原输入路径。真实输入自身超限仍按既有规则收口。不得截断获准正文、重复 Hook、为注入上下文再次摘要、将不确定估算视作确定超限或掩盖结构错误及不确定写入。

compact 上下文 SHALL 对已安装的新摘要窗口与确定的后续候选执行普通请求预算和完整编码检查。自动场景保持本次已冻结的真实初始或 steering 输入、工具配对及适用的 Stop 反馈义务，compact 上下文作为可省略增量评估。该上下文不得破坏既有窗口缩减条件或使本可交付的下一候选超限。发生取消、预算或编码拒绝时，既有 checkpoint 及候选相关性保留。compact 未提交内容不进入已经发出的摘要请求，也不重新开启此次压缩判断。手动场景检查已安装窗口，随后未知的真实用户输入仍走其原有接纳预算。

在相同目录、项目指令、Skill 目录、实际 Provider 属性与缓存策略下，加入 SessionStart 上下文只改变真实 committed_history 及其派生的完整请求与窗口诊断，稳定前缀 fingerprint MUST 保持。Context SHALL 随成功本地交付反映已提交窗口的新 revision 和 footprint。实际内容 fingerprint 由同一 planner 计算，在后续真实请求中形成缓存证据。最近实际 sample/summary 的请求 receipt、usage、baseline 和观察来源只随其真实网络和提交事实更新。本地追加不得制造一次 cache observation。缓存失效解释 SHALL 保留 context_compacted 及 restored_baseline 的现有优先级，实际历史 fingerprint 仍按真实内容计算。最新 checkpoint 只覆盖实际替换的历史，旧上下文不会被自动注入。

#### Scenario: Apply both eligible contexts after the one compaction
- **WHEN** 首 sample 的既有自动压缩已收口，compact 上下文和随后 resume 上下文均适用且合法
- **THEN** 实际请求依次包含已安装摘要、compact 上下文、resume 上下文和真实用户输入。候选与实际请求使用同一冻结属性，该边界不再发出摘要请求

#### Scenario: Preserve a valid input when optional context exceeds budget
- **WHEN** 本次首输入自身符合已安装窗口限制，而完整 startup 或 resume 上下文使候选触发确定的预算或编码上限
- **THEN** 省略整份上下文，同一真实输入最多重新预检一次，原输入关联保持，无附加摘要或命令

#### Scenario: Preserve a valid pending candidate after compaction
- **WHEN** 自动压缩后的 compact 上下文会使已冻结的工具续写、Stop 反馈或真实输入候选超限
- **THEN** 保留 checkpoint 和可交付的候选，零 compact 内容提交，完整保留已冻结正文

#### Scenario: Retain request evidence while the committed window grows
- **WHEN** compact 上下文已 Sync 和 finalize，但下一普通请求尚未发生
- **THEN** Context 反映新 revision 与体量，cache 观测继续报告最近真实 sample 和摘要请求，不提前更新 receipt、usage 或 reset 原因

#### Scenario: Explain a committed context change
- **WHEN** 两份已提交历史只在实际保存的 SessionStart 模型包装内容上不同
- **THEN** 可观察的变化定位于 committed_history，稳定来源与稳定前缀相同。相同历史在恢复前后的估算、有效窗口 revision 和分段 fingerprint 一致
