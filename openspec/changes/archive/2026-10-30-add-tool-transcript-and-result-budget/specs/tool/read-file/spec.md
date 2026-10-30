## MODIFIED Requirements

### Requirement: Read results are deterministic and bounded

成功结果 SHALL 包含 workspace 相对路径、请求 offset/limit、实际返回的起止行、是否到达 EOF、是否发生长行或总预算截断，以及稳定带行号文本。行号从原文件的 1-based 行号开始；每行最多保留 2000 个 Unicode code points，截断不得产生非法 UTF-8，并 MUST 以确定标记报告省略。整个模型 preview 的 UTF-8 bytes MUST 不超过 256 KiB；达到预算时 SHALL 在完整行边界前停止并报告尚有内容省略。

相同文件 bytes、typed input、当前渲染规则和分配 allowance MUST 产生逐字节相同的 preview。错误结果 SHALL 使用稳定英文 code/message，只包含 workspace 相对路径和安全边界信息，不得包含绝对 workspace、底层 OS 路径、文件正文、secret 或原始系统错误。

#### Scenario: Render selected lines
- **WHEN** offset `3`、limit `2` 读取一个至少五行的文本文件
- **THEN** preview 只包含原文件第 3 与第 4 行及其稳定行号，并报告尚未到达 EOF

#### Scenario: Truncate a long line safely
- **WHEN** 选中行超过 2000 个 Unicode code points
- **THEN** preview 在合法 UTF-8 边界截断该行并包含确定省略标记

#### Scenario: Enforce total preview budget
- **WHEN** 所选行的编码结果会超过 256 KiB
- **THEN** preview 不超过该预算、只包含完整编码边界并明确报告剩余内容被省略

#### Scenario: Keep errors path-safe
- **WHEN** Read 因权限、路径或读取错误失败
- **THEN** 返回给模型和宿主的错误不包含绝对 workspace、底层 cause 或文件内容

上述 256 KiB 是既有硬上限。原范围按该规则生成的结果若超过本次聚合预算分配 allowance，系统 MUST 返回有界 result_budget_exceeded，提示缩小 offset/limit，不得事后再默默裁短成功结果或自动转存。预算错误不得包含原正文。

#### Scenario: Reject a range that does not fit its allowance
- **WHEN** 既有合法 Read preview 为 32 KiB 但本次仅分配 8 KiB
- **THEN** 返回不超 allowance 的预算错误和缩小范围建议，不生成 artifact 或成功的部分正文

### Requirement: Complete reads provide transient file observations

成功 Read SHALL 在既有冻结结果之外提供封闭的 complete/unavailable 瞬态观察状态。只有同一安全文件句柄读取前后 identity、mode、size 与修改状态一致，且 preview 从第一行到 EOF 无长行或总预算截断时才能 complete；空文件从第一行完整读取亦可 complete。complete MUST 绑定 invocation identity、规范相对路径、原始 bytes 摘要和实际文件 identity/size/mode。

该观察 SHALL 不包含文件正文，MUST NOT 写入 Session、Provider native output、RuntimeEvent、日志或稳定 cache segment；模型 preview 与原 Read 截断/行号口径保持一致。partial/unavailable 必须显式表示，不能通过可选旧返回类型、事后重读或带行号 preview 计算伪造证据。

#### Scenario: Observe a complete empty file
- **WHEN** Read 从第一行读取一个稳定的空文件
- **THEN** 结果保持既有空文件 preview，并返回绑定零字节原始内容的 complete observation

#### Scenario: Hash raw CRLF bytes
- **WHEN** Read 返回经过现有显示处理的 CRLF 文本
- **THEN** observation 的摘要对应实际原始 bytes，而非带行号或归一化 preview

#### Scenario: Do not certify truncated content
- **WHEN** Read 因请求范围、长行或总预算仅显示部分文件
- **THEN** observation 为 unavailable，即使底层曾读取整个文件

#### Scenario: Reject an unstable observation
- **WHEN** 故障注入在读取期间修改文件，即使修改后的长度相同
- **THEN** 不提供 complete observation，不能让后续 Write 使用混合或过期基线

#### Scenario: Keep model bytes stable
- **WHEN** 相同输入和文件从新 Read 执行路径返回且 allowance 足以完整容纳原 preview
- **THEN** Provider output preview 与既有 Read 字节完全相同，新增观察只在进程内返回

complete observation SHALL 以最终模型结果完整成功为必要条件，只有配对 native outputs 成功 Sync/finalize 后才激活。聚合预算拒绝必须转为 unavailable 并失效对应旧观察，保存内容或宿主展开不构成模型已读证据。

#### Scenario: Do not authorize Write after budget rejection
- **WHEN** Read 执行材料完整但 finalization 因 allowance 不足返回预算错误
- **THEN** 随后 Write 不能使用该观察或对应旧观察覆盖文件
