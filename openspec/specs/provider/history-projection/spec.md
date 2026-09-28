## Purpose

定义 Provider 原生历史到共享文本语义视图的单向、只读投影契约，使 token 估算、历史回放、Hooks 和 Subagent 等后续消费者能够读取一致语义，而不解析 Provider wire 或替代原生续写事实源。

## Requirements

### Requirement: Project committed native history into ordered semantic turns

每个受支持的 Provider SHALL 将已成功提交的原生历史投影为按提交顺序排列的语义 turn。每个语义 turn MUST 保留对应的用户可见文本和 assistant 可见文本，视图 MUST 标识原生历史所属的 Provider family。

投影 SHALL 只读取已提交历史；活动 turn 的 staging、失败、取消、timeout 或提前 EOF 产生的部分数据 MUST NOT 出现在视图中。空会话 SHALL 产生来源明确且 turn 列表为空的有效视图。

#### Scenario: Project an empty conversation

- **WHEN** 任一受支持 Provider 的会话尚未成功提交 turn
- **THEN** 投影结果标识该 Provider family，且语义 turn 列表为空

#### Scenario: Preserve committed turn order

- **WHEN** 会话已按顺序成功提交多个 turn
- **THEN** 投影结果按相同顺序包含每个 turn 的用户文本和 assistant 可见文本

#### Scenario: Exclude an uncommitted turn

- **WHEN** 一个新 turn 仍在 streaming，或最终以失败、取消、timeout 或提前 EOF 结束
- **THEN** 投影结果与该 turn 开始前一致，不包含其用户输入或部分 assistant 输出

### Requirement: Provider projectors preserve visible text semantics

Anthropic 投影 SHALL 按原生 message 与 content block 顺序连接 user 和 assistant 的 text blocks。OpenAI 投影 SHALL 按原生 turn、output item 与 content part 顺序连接 user `input_text` 和 assistant `output_text`。连接 MUST 精确保留各文本片段的内容和接收顺序，不得自行插入、删除或规范化空格与换行。

对于同一成功 turn，live RuntimeEvent 文本增量按顺序连接后的 assistant 文本 MUST 与该 turn 完成后的历史投影文本一致。

#### Scenario: Project Anthropic text blocks

- **WHEN** 一个已提交 Anthropic turn 的 user 和 assistant message 各包含一个或多个 text block
- **THEN** 投影分别按 content block 顺序连接 user 与 assistant 的文本，并保留原始空格和换行

#### Scenario: Project OpenAI text parts and items

- **WHEN** 一个已提交 OpenAI turn 包含 user `input_text` 以及一个或多个 assistant message item 的 `output_text`
- **THEN** 投影按 item 和 content part 的原始顺序连接对应文本，并保持该 turn 的单一语义边界

#### Scenario: Match live and replayed assistant text

- **WHEN** 一个 turn 的 live stream 产生多个 assistant 文本增量并成功提交
- **THEN** 按事件顺序连接的 live 文本与随后投影得到的 assistant 文本完全相同

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

### Requirement: Projection returns an independent read-only snapshot

每次投影 SHALL 返回与会话内部 native history 以及其他投影结果不共享可变集合的独立快照。调用方修改或丢弃已返回视图 MUST NOT 改变后续投影、后续请求或其他会话的历史。

投影 SHALL 是无网络、文件、数据库和进程副作用的内存操作，并 MUST 与已提交历史的并发读取和 turn 提交保持数据竞争安全。

#### Scenario: Mutate a returned view

- **WHEN** 调用方修改一个已返回视图中的 turn 文本或 turn 列表
- **THEN** 再次投影仍返回未被修改的已提交语义，后续 Provider 请求也不受影响

#### Scenario: Project isolated conversations

- **WHEN** 同一个 Provider 创建两个具有不同已提交历史的 conversation
- **THEN** 两个投影只包含各自 conversation 的语义 turn，不共享或混合数据

#### Scenario: Project while another turn completes

- **WHEN** 历史投影与同一 conversation 的 turn 成功提交并发发生
- **THEN** 每次返回的视图都是提交前或提交后的完整快照，不出现部分 turn 或数据竞争

### Requirement: Semantic history cannot become a Provider request source

Provider RequestCompiler、原生 resume 和未来 Session 恢复 MUST 继续消费对应 Provider 的 native history，MUST NOT 从语义视图构造、补全或修复 Provider 请求。共享消费者 SHALL 只依赖语义历史契约，不得导入 Anthropic 或 OpenAI wire 类型来重新解释历史。

#### Scenario: Continue after reading semantic history

- **WHEN** 共享消费者读取语义视图后，会话提交下一条用户输入
- **THEN** Provider 仅使用自身已提交 native history 和新输入编译请求，不读取语义视图

#### Scenario: Keep shared consumers independent of Provider wire

- **WHEN** token、resume 渲染、Hook 或 Subagent 等共享消费者接入历史读取能力
- **THEN** 消费者只接收语义历史类型，不需要 Anthropic Messages 或 OpenAI Responses 原生类型
