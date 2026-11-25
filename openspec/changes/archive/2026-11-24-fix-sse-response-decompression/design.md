## Context

两种 DeepSeek 端点在接受 `gzip, deflate` 时均返回 HTTP 200 和 gzip SSE。解压后分别可见 `message_stop` 与 `response.completed`。本地 HTTP 探针用项目锁定的 Resty 版本复现了裸流为空、包装响应体可读到完整 SSE 的行为。编译后的 EasyCode 经临时回环服务透传真实上游的非压缩响应，两种协议均走完四次请求和历史分支冷恢复。 [提案](proposal.md) · [DeepSeek Messages](https://api-docs.deepseek.com/guides/anthropic_api/) · [DeepSeek Responses](https://api-docs.deepseek.com/guides/responses_api/)

## Goals / Non-Goals

修复 HTTP 响应解码与 SSE 读取的交接。压缩后的变换由 HTTP 层拥有，SSE 继续负责分帧与流活动识别，Provider reducer 继续负责协议终态。模型兼容、WebSocket、Provider 原生记录和用户配置契约属于各自既有边界。

## Decisions

### 使用 Resty 包装的响应体

`StreamSSE` 获取 Resty 的 `Response.Body`，让现有 SSE reader 消费已经按内容编码解码的流。Resty 在关闭自动解析时仍安装内容解码器和响应体包装。将协商编码改成 `identity` 会依赖上游遵守协商，并让实际线上出现的压缩响应失去覆盖。单独用 gzip 替代 Resty 解码会重复恢复已有资源所有权和内容协商。 [现有 HTTP 边界](../../../../internal/provider/transport/client.go)

### HTTP 取消先于关闭压缩响应体

Resty gzip 包装可以在 `Read` 期间持锁。SSE owner 在终止时先取消实际 HTTP 请求，解除底层读取，再关闭包装响应体、等待 reader 收口并注销活动请求。HTTP 请求的取消与向调用方交付终态的控制 context 分开，使空闲超时仍能交付原错误。非成功状态以及 Execute 返回错误且附带可关闭响应体时，在交接前由请求路径收口。外部取消仍能覆盖连接建立和 Resty 解码器初始化。Resty 返回可读响应体后，既有 SSE owner 接管空闲超时。 [SSE owner](../../../../internal/provider/transport/sse.go) · [活跃请求关闭回归](../../../../internal/provider/transport/close_test.go)

### 保留分帧和完成边界

解码后的字节使用同一分帧器、UTF-8 处理、最大事件限制及 EOF 分类。Provider 已走完原有终态验证时，HTTP sample 继续沿用已有读取截止行为。压缩响应体读取在所需终态之前报错时走既有流失败路径。保留已有的有序交付和取消结果语义。

## Risks / Trade-offs

- Resty 的默认 gzip 解码器可能在 Execute 内等待 gzip 头。调用方取消覆盖该阶段。已交付的 SSE 流由已有空闲计时器负责；建立请求和等待可用响应体仍属于现有的 HTTP 接入阶段。
- 压缩流的完整字节可能集中到单次读取。用服务端显式 flush 及测试门控验证首帧在上游结束前可见，再用确定性阻塞覆盖实际 HTTP 请求的解阻路径。 [现有 HTTP 回归](../../../../internal/provider/transport/client_test.go)
