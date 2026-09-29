## MODIFIED Requirements

### Requirement: Request bytes are stable and bounded

Provider RequestCompiler SHALL 使用确定性 JSON 编码生成完整且有大小边界的 canonical request bytes；transport SHALL 将调用方提供的 method、headers 和已经编译的正文 bytes 作为同一次 HTTP 请求发送，不得接收无约束结构化值或再次执行 JSON 序列化。transport 在产生网络副作用前 SHALL 验证正文非空、是合法 JSON、未超过上限，并取得不受调用方后续修改影响的独立快照。

相同 Provider 输入重复编译 MUST 产生相同正文 bytes；请求错误和诊断信息 MUST NOT 包含 Authorization、API key 或完整请求正文。该职责调整不得改变现有 Anthropic Messages 或 OpenAI Responses 的 golden request bytes。

#### Scenario: Equivalent input produces identical request bytes

- **WHEN** 使用相同的强类型 Provider 请求输入重复编译流式请求
- **THEN** 每次 RequestCompiler 产生的 canonical JSON 正文字节完全一致
- **THEN** transport 逐字节发送该正文而不重新编码

#### Scenario: Snapshot request bytes before sending

- **WHEN** 调用方在请求被接受后修改其原始 byte slice
- **THEN** transport 发送的正文仍等于接受时的独立快照

#### Scenario: Reject invalid or oversized compiled bytes

- **WHEN** 调用方提供空正文、malformed JSON 或超过请求上限的 canonical bytes
- **THEN** transport 在建立网络请求前返回安全 provider 错误

#### Scenario: Request failure is safely reported

- **WHEN** 服务端在流建立前返回非成功 HTTP 状态
- **THEN** 系统返回包含安全状态摘要的 provider 错误
- **THEN** 错误、日志和测试 snapshot 均不包含敏感 header 或请求正文
