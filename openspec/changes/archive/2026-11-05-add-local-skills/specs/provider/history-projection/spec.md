## MODIFIED Requirements

### Requirement: Provider projectors preserve visible text semantics

Anthropic 投影 SHALL 按原生 message 与 content block 顺序投影独立消息，仅在同一消息内部连接属于原始正文的 text blocks。OpenAI 投影 SHALL 按真实 turn、原生 item 与 content part 顺序投影独立用户和 assistant 消息，仅在同一消息内部连接属于原始正文的文本。连接 MUST 精确保留各文本片段的内容和接收顺序，不得自行插入、删除或规范化空格与换行。

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

显式 Skill 附件 SHALL 使用独立强类型只读投影关联到原 InputID，不拼入用户原文；用户 recall 只读取原文，详情可读取附件来源及正文，有效窗口估算覆盖实际附件内容。离线预览与正常恢复使用相同投影，附件不能成为 Provider 请求来源。

#### Scenario: Recall a Skill command without its expansion
- **WHEN** 原始输入 /review diff 携带已提交 Skill 附件
- **THEN** recall 仍为 /review diff，附件详情单独可见，估算不遗漏或重复计算附件

#### Scenario: Preview Skill history offline
- **WHEN** 不提供凭据且 Skill 文件不存在时投影合法存储历史
- **THEN** 与正常恢复具有相同原文和附件投影，不访问磁盘、网络或 opaque reasoning
