# provider/reasoning-display Specification

## Purpose

定义双 Provider 已返回推理内容的单向展示白名单、各自流式交付规则与分层资源边界，使宿主能够阅读允许文字并识别真实活动，同时保持原生续写、请求策略、签名和不可展示数据不受界面操作影响。

## Requirements

### Requirement: Displayable reasoning uses an explicit provider whitelist

系统 SHALL 仅允许 Anthropic thinking 文字和 OpenAI 非零字节 summary_text parts 进入独立推理正文；OpenAI 各段保留原序和所有空白，段间两个 LF，零字节 part 不增加分隔。signature、redacted data、encrypted content、raw reasoning/content、原始 wire、Provider ID、phase 和未知扩展 MUST NOT 进入展示正文、语义事件、诊断或 headless。没有非空允许文字的已知推理项 SHALL 为无正文 unavailable，没有推理项不得伪造活动。文本语义历史继续排除全部推理，展示派生值不得新增到模型/压缩请求或工具结果。

#### Scenario: Read only allowed provider text
- **WHEN** thinking/summary 与签名、密文、未知 part 同时存在
- **THEN** 展示仅有白名单文字和准确来源，其他字段的独立哨兵均不可见，原生保存仍保真

#### Scenario: Preserve empty and whitespace-only parts
- **WHEN** summary 依次含零字节段、只有空格的段和普通段
- **THEN** 仅跳过零字节段，其他原文与双 LF 分隔准确，全部零字节时为 unavailable

### Requirement: Reasoning display has a strictly scoped stream lifecycle

内部展示 SHALL 使用稳定 sample/block scope、来源与 Started/Delta/Completed 三阶段的强类型当前契约，严格拒绝重复/未知字段、null、跨种类字段和非法状态。块只允许开始一次，Delta 为连续原文范围，Completed 是有界替换快照而不是再次追加；块完成不代表保存。正常 sample 成功 MUST 已关闭全部块；sample 失败或取消 SHALL 根据 runtime/chat-turn 的真实提交解析一次收口全部当前块，允许缺少块 Completed，且不影响先前已提交 sample。

Provider 完整 item 首次提供允许正文时 SHALL 发布有界的 Started/Completed 序列，仍只形成一个展示块；不允许内部消费者隐式接受未开始的 Completed。内部过期 sample、已关闭块的新 delta、重复关闭或身份错配 MUST 失败并沿原 owner 取消/排空。Provider 边界明确允许幂等或丢弃的外部完成通知不发布内部事件，不得混同内部非法事件。

#### Scenario: Consume item-only output once
- **WHEN** OpenAI 仅以合法完整 reasoning item 提供正文
- **THEN** 依次产生 Started/Completed 后发布该 native item，一个块、一份正文，不要求原服务端未要求的辅助事件

#### Scenario: Fail a sample with open blocks
- **WHEN** stream 在多个块尚未 Completed 时取消、失败或提前 EOF
- **THEN** 真实 sample resolution 收口所有当前块，不额外制造第二次 turn failure，不把先前 durable 块标为未保存

#### Scenario: Reject an invalid internal update
- **WHEN** 内部 delta 引用已关闭块或其他 sample
- **THEN** 返回安全协议错误并完成唯一 stream cleanup，不能以默认忽略事件绕过

### Requirement: Each provider has an explicit reasoning delivery contract

Anthropic SHALL 保持 start 中 thinking/signature 初值忽略、delta 累计及 stop 完成的原生规则，不能为了展示从 start 偷补正文；redacted start/stop 无正文。OpenAI SHALL 采用默认增量/段完成快照/完整 item 路径，不协商 sequential_cutoff。已收到的初值/delta 与段完成及最终 item 必须一致；没有该段文字时允许完成快照，最终 item 可补齐从未收到的段，不得纠正已知冲突。

OpenAI parts SHALL 以 summary_index 独立关联，较后 part 先到时只暂存，待前段结束或最终 item 确认空段后才发布连续前缀。相同重复段完成幂等消费，冲突失败；已关闭已知 item 的迟到 text.done/part.done 只在 Provider 边界丢弃，不更新 native 或新活动项。未知引用、错配 index 和已关闭 item 的新 delta 必须失败。未协商模式下 partial/done 不一致不得宣称为已支持的 sequential_cutoff 行为。

#### Scenario: Keep Anthropic start fields out of accumulation
- **WHEN** start 带 thinking 初值且后续 delta 含相同文字
- **THEN** native 与展示只累计 delta 一次，signature 保留原语义；没有 delta 不因 start 初值产生可阅读正文

#### Scenario: Publish parts in their source order
- **WHEN** 后段文字先到，前段随后完成，或最终 item 才确认前段为空
- **THEN** 共享展示只发布已确定前缀，最终正文顺序和分隔与原生投影一致，不移动已发布文字

#### Scenario: Distinguish snapshots conflicts and stale completions
- **WHEN** 分别收到无 delta 的段快照、相同重复 done、冲突 done、已关闭已知项的迟到 done 和未知项 delta
- **THEN** 分别接受一次、幂等消费、失败、仅在 Provider 边界丢弃、失败，全部行为不重放模型或工具

### Requirement: Reasoning resources are bounded before allocation and publication

每 sample 推理 source 的已解码逻辑文字/opaque 字段总量 SHALL 不超过 16 MiB，新增 source/scratch 暂存总量不超过 16 MiB；items/blocks 与 parts 合计最多 4096，index 小于 4096，新增元数据最多 1 MiB。追加、创建、替换和复制前 MUST 校验；最终 native encoded 16 MiB 限额独立有效，不能替代流式累计检查。

单 Delta SHALL 最多 16 KiB UTF-8，较大合法增量分段；Completed 保留快照最多 64 KiB，所有推理和提交绑定事件的 serialized payload 最多 512 KiB，包含 JSON escaping。事件发布前检查，绑定编码在 sample 提交 admission 前预检；Provider 和 AgentLoop 的输出队列各最多 16 项，两处新增展示排队 payload 合计最多 16 MiB，处理中的新增展示 payload 临时副本另限 4 MiB，不新增无限聚合。UI 显示上限按 tui/reasoning-transcript 独立处理，显示淘汰不能关闭协议验证或改变模型结果。

#### Scenario: Enforce cumulative bounds across small frames
- **WHEN** 多个合法小 frame 累计达到 source、part 数、index 或元数据限额
- **THEN** 在下一次超限追加或分配前失败，不等最终编码才检查，不遗留无 owner 状态

#### Scenario: Bound escaped queued snapshots
- **WHEN** 完成快照含大量需要 JSON escaping 的字符，或消费者暂停接收
- **THEN** serialized 大小与各队列容量仍受限，背压沿既有 owner 收口，不能按未转义文字大小绕过预算

### Requirement: Presentation cannot enable reasoning or affect native truth

展示 SHALL 只消费实际返回值，不修改 thinking/effort/budget/summary 请求、工具目录、Provider 选择或缓存策略，不按模型名称推断远端能力。主动请求 summary 的能力与被动展示分开，当前 ReasoningSummary/RawReasoning 不因本切片置 true；已实现原生保存能力保持原有含义。

相同历史展示前后及恢复前后的 canonical request、native 顺序和 fingerprint MUST 等价。OpenAI WebSocket 恢复的 full/incremental 按既有连接重置口径比较，Anthropic marker/TTL 独立核验；不改变原生 history 在续写/压缩中的使用方式。

#### Scenario: Continue after reading or restoring reasoning
- **WHEN** 用户阅读、原文切换、resize、恢复后继续同一历史
- **THEN** 对应请求 bytes、native 顺序及 fingerprint 与未进行展示操作时相同，签名和密文完整，无额外模型请求

#### Scenario: Receive no readable reasoning
- **WHEN** 服务端没有推理项或只有 opaque 推理
- **THEN** 分别无提示或只有 unavailable，不主动开启能力、不生成替代摘要
