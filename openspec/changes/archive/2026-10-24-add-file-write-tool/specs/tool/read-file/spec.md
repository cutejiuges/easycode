## ADDED Requirements

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
- **WHEN** 相同输入和文件从新 Read 执行路径返回
- **THEN** Provider output preview 与既有 Read 字节完全相同，新增观察只在进程内返回
