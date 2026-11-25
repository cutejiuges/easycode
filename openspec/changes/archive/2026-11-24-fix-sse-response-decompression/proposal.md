## Why

DeepSeek 的 Anthropic Messages 和 OpenAI Responses 端点均可认证，并在协商 gzip 时返回含正常终态的 SSE。EasyCode 的共用 HTTP 传输将 Resty 的底层响应体交给 SSE 解析器，绕过解压并可能读到已被解压器预取完的底层流，导致两个 Provider 都报告终态缺失。

## What Changes

- 将 Resty 解码后的响应体交给现有 SSE reader，并由同一 HTTP 传输 owner 取消请求、关闭响应体和等待读取结束。
- 补充压缩 SSE 的有序分帧与资源释放回归，用真实的 Anthropic Messages 和 OpenAI Responses 连接验证修复。
- 沿用现有请求字节、协议终态、重试和日志契约。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `provider/http-stream-transport` 明确受支持的 HTTP 内容编码先解码再分帧，并要求压缩流遵循现有取消、空闲超时与清理契约。

## Impact

变更集中在 `internal/provider/transport`，两种 Provider 的 HTTP SSE 及复用该传输的操作均受益。当前配置格式、Provider 原生请求与会话持久化契约保持原有语义。 [现有传输规格](../../../specs/provider/http-stream-transport/spec.md)
