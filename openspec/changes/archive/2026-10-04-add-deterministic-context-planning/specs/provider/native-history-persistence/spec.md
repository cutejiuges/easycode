## ADDED Requirements

### Requirement: Native history exposes a private context footprint

每个受支持 Provider Conversation SHALL 从已成功提交的原生历史生成只读、强类型的上下文 footprint。footprint MUST 表示下一次请求会重放的已提交原生内容总估算，包括可见文本以及 Anthropic thinking/signature/redacted thinking、OpenAI reasoning/encrypted content 和受控未知原生项；不得计入 usage、response ID、transport metadata 或尚未提交的 streaming staging。

footprint SHALL 只暴露 Provider family、可恢复等价的单调 committed revision、版本化 estimator method、`estimated` 或 `unknown` 状态及 token 数，不得暴露原生 item、正文、signature、encrypted bytes、raw JSON 或可逆摘要。空历史 revision SHALL 为零；每个成功 finalized native commit SHALL 恰好推进一次 revision，失败或 staging sample 不得推进。读取 footprint MUST 是无网络、文件、数据库和进程副作用的内存操作，不得修改原生历史或后续请求编译结果。

#### Scenario: Estimate committed opaque Anthropic history

- **WHEN** Anthropic 已提交历史包含文本、thinking/signature 和 redacted thinking blocks
- **THEN** footprint 估算下一次 Messages 请求会重放的全部相关原生内容
- **THEN** 返回值不包含 block 正文、signature 或 redacted data

#### Scenario: Estimate committed encrypted OpenAI history

- **WHEN** OpenAI 已提交历史包含 message、reasoning summary、encrypted content 和受支持未知 item
- **THEN** footprint 估算下一次 Responses 请求会重放的全部相关原生内容
- **THEN** 返回值不包含 item、summary 正文、encrypted bytes 或 raw JSON

#### Scenario: Exclude an active staging turn

- **WHEN** Conversation 正在归并一个尚未成功 durable finalize 的 sample
- **THEN** footprint 与该 sample 开始前一致
- **THEN** 当前用户输入和部分 assistant 输出不进入已提交历史 footprint

#### Scenario: Advance revision only after native commit

- **WHEN** 一个 prepared sample 已完成但尚未 durable finalize，随后成功 finalize
- **THEN** finalize 前 footprint revision 不变，finalize 后恰好增加一次
- **THEN** 重复或失败 finalize 不会再次推进 revision

#### Scenario: Preserve request compilation after measuring

- **WHEN** 对同一 Conversation 读取一次或多次 footprint 后编译下一轮请求
- **THEN** 请求的原生 item 顺序、canonical bytes 和 cache fingerprint 与未读取 footprint 时完全相同

### Requirement: Restored native footprint matches uninterrupted history

对于相同 Provider 配置和相同已提交 native commits，从 Session 恢复的 Conversation SHALL 产生与未退出进程的 Conversation 相同 family、committed revision、estimator method、估算状态和 token 数。恢复路径 MUST 从已验证的 Provider-native history 重新计算 footprint，不得从 `SemanticHistoryView`、RuntimeEvent、normalized usage 或 Session 错误摘要反推 opaque 内容。

#### Scenario: Compare uninterrupted and restored Anthropic footprints

- **WHEN** Anthropic 会话持久化包含 opaque thinking 的 commits 并从相同 commits 恢复
- **THEN** restored 与 uninterrupted footprint 完全相同

#### Scenario: Compare uninterrupted and restored OpenAI footprints

- **WHEN** OpenAI 会话持久化包含 encrypted reasoning 的 commits 并从相同 commits 恢复
- **THEN** restored 与 uninterrupted footprint 完全相同

#### Scenario: Reject corrupt history before footprint use

- **WHEN** 任一 native commit 无法通过既有事务性恢复校验
- **THEN** Provider 不返回可供 footprint 读取的部分 Conversation
- **THEN** Runtime 不会使用部分历史继续规划或发起网络请求
