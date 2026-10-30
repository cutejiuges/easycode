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

### Requirement: Provider projectors preserve visible text semantics

Anthropic 投影 SHALL 按原生 message 与 content block 顺序投影独立消息，仅在同一消息内部连接 text blocks。OpenAI 投影 SHALL 按真实 turn、原生 item 与 content part 顺序投影独立用户和 assistant 消息，仅在同一消息内部连接文本。连接 MUST 精确保留各文本片段的内容和接收顺序，不得自行插入、删除或规范化空格与换行。

对于同一成功 turn，live RuntimeEvent 文本增量按顺序连接后的 assistant 文本 MUST 与该 turn 完成后的历史投影文本一致。

#### Scenario: Project Anthropic text blocks

- **WHEN** 一个已提交 Anthropic turn 的 user 和 assistant message 各包含一个或多个 text block
- **THEN** 投影保持每条 user/assistant 消息边界并按各自 content block 顺序连接文本，并保留原始空格和换行

#### Scenario: Project OpenAI text parts and items

- **WHEN** 一个已提交 OpenAI turn 包含 user `input_text` 以及一个或多个 assistant message item 的 `output_text`
- **THEN** 投影按 item 和 content part 的原始顺序连接对应文本，并保持该 turn 的单一语义边界

#### Scenario: Match live and replayed assistant text

- **WHEN** 一个 turn 的 live stream 产生多个 assistant 文本增量并成功提交
- **THEN** 按事件顺序连接的 live 文本与随后投影得到的 assistant 文本完全相同
