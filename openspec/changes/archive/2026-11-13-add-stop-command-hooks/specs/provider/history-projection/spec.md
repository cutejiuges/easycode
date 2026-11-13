## MODIFIED Requirements

### Requirement: Project committed native history into ordered semantic turns

每个受支持的 Provider SHALL 将已成功提交的原生历史投影为按提交顺序排列的语义 turn。每个语义 turn MUST 使用真实 turn identity，并以强类型有序消息保留用户与 assistant 可见文本；用户消息包含 InputID，同 turn 中不同用户输入不得合并，视图 MUST 标识原生历史所属的 Provider family。

投影 SHALL 只读取已提交历史；未提交 staging 和部分 assistant 数据 MUST NOT 出现在视图中；失败、取消、timeout 或提前 EOF 不得删除此前独立提交的用户输入及成功 sample。空会话 SHALL 产生来源明确且 turn 列表为空的有效视图。

#### Scenario: Project an empty conversation

- **WHEN** 任一受支持 Provider 的会话尚未提交任何 native history
- **THEN** 投影结果标识该 Provider family，且语义 turn 列表为空

#### Scenario: Preserve committed turn order

- **WHEN** 会话已按顺序成功提交多个 turn
- **THEN** 投影结果按相同顺序包含每个 turn 的用户文本和 assistant 可见文本

#### Scenario: Exclude an uncommitted turn

- **WHEN** 一个新 turn 仍在 streaming，或最终以失败、取消、timeout 或提前 EOF 结束
- **THEN** 投影保留该 turn 已提交的用户输入及成功 sample，不包含未提交 assistant 输出

#### Scenario: Preserve interleaved messages in one turn
- **WHEN** 同一真实 turn 依次提交用户、assistant sample、中途用户和最终 assistant
- **THEN** 投影保持四者消息顺序与同一 turn 归属，恢复不能以每条用户消息推断新 turn

已提交 hook_feedback SHALL 具有独立的 Hook 来源语义角色及真实 turn/source sample 关联，不能制造 InputID、用户召回文本或助手完成。其正文只来自已验证 native 事实，不重新读取脚本。既有用户/assistant 消息的原始顺序与身份保持。仅有 continued 冻结决定而 native 尚未交付时，语义历史不得提前出现反馈；真实纠正随三记录交付保存后，保留原 InputID/Skill 附件归属，不与 Hook 来源合并。

#### Scenario: Keep a hook message distinct from real user input
- **WHEN** 同一 turn 的候选答复后保存 Stop 反馈与真实纠正，再保存模型续答
- **THEN** 投影保持 assistant/Hook/user/assistant 顺序及真实用户身份；反馈不成为真实 user 或 recall 条目

### Requirement: Semantic history excludes Provider-private and opaque data

语义视图 MUST NOT 包含或暴露 Anthropic thinking、signature、redacted thinking、OpenAI reasoning summary、encrypted content、Provider phase、usage、response/message ID、未知扩展、原始 JSON 或请求字段。没有可见文本的受支持原生项 SHALL 被忽略且不得伪造占位文本。

投影不得改变、删除或重写原生历史；同一原生历史在投影前后编译出的后续 Provider 请求 MUST 保持相同的原生顺序、canonical bytes 和 fingerprint。

#### Scenario: Omit Anthropic opaque thinking data

- **WHEN** Anthropic 原生历史包含 thinking/signature、redacted thinking 和 text blocks
- **THEN** 语义视图只包含 text block 的可见文本，且不包含 signature、redacted data 或 thinking 正文

#### Scenario: Omit OpenAI reasoning data

- **WHEN** OpenAI 原生历史包含 reasoning summary、encrypted content、phase、未知 item 和 assistant text
- **THEN** 语义视图只包含当前切片支持的用户与 assistant 可见文本，不包含其他原生字段或 opaque bytes

#### Scenario: Preserve request compilation after projection

- **WHEN** 对一份已提交原生历史执行一次或多次语义投影后再编译下一轮请求
- **THEN** 请求的原生 item 顺序、canonical bytes 和 fingerprint 与未执行投影时完全相同

上述隐私排除 SHALL 同样适用于按 sample 的 Stop 正文读取；已授权 hook_feedback 的公开正文及受控语义来源可以投影，但不得带入其原始 stdout/stderr、原生 wire 或 Provider-private 字段。

#### Scenario: Keep hook text separate from opaque data
- **WHEN** 同一历史含明确 model 反馈、thinking/summary、签名、密文与未知扩展
- **THEN** Hook 语义只提供已授权正文与受控来源，opaque 数据仍只在 native 中

## ADDED Requirements

### Requirement: Completion hooks project the exact committed assistant sample

每个 Provider SHALL 提供按真实 turn/sample identity 读取已提交 assistant 正文的单向、纯内存、只读投影，验证目标存在且返回独立副本。只能读取目标 sample 内的 assistant 可见 text，保持原字节语义和顺序，不依赖 phase、正文相等、工具 ready 或可见列表长度推测位置。目标不存在必须明确失败；目标无 text 返回合法空字符串，不回退前一次非空答复。投影不读取 staged 数据、不更改后续请求/指纹、不访问磁盘或网络。

#### Scenario: Read an empty committed sample
- **WHEN** 前一 sample 含文本而指定 sample 只有不可展示原生内容
- **THEN** 返回同一目标的空正文，不泄漏 thinking/summary 或复制前一 sample

#### Scenario: Preserve whitespace and duplicate text
- **WHEN** 两个不同 sample 正文相同且含前后空白
- **THEN** 每次读取仍按各自身份准确返回原正文，不按内容去重或 trim

#### Scenario: Reject a staged or wrong target
- **WHEN** 指定 sample 尚未提交、属于另一 turn 或不存在
- **THEN** 返回明确错误，无部分投影或原生历史变化
