# SSE 响应解压验收

- 两种真实端点协商 gzip 时返回 HTTP 200，解压后具有各自的正常终态。本地 Resty 探针复现包装响应体与底层响应体的差异。修复后的 SSE 读取使用解压包装，空闲终止先取消真实 HTTP 请求，再关闭响应体和等待读取结束，保留调用方的空闲错误分类。 [传输实现](../../../../internal/provider/transport/client.go) · [读取收口](../../../../internal/provider/transport/sse.go)
- 真实本地 HTTP 回归确认 UTF-8 首帧在服务端结束前交付，后续帧及 EOF 有序。空闲回归确认 gzip 读取阻塞时实际请求被取消、输出关闭且只发起一次请求。解码初始化失败时响应体在返回前关闭。仅将压缩流空闲收口纳入默认 race 清单。 [HTTP 回归](../../../../internal/provider/transport/client_test.go) · [关闭回归](../../../../internal/provider/transport/close_test.go)
- macOS arm64 上使用 `make build` 编译 CLI，在真实 PTY 和隔离的会话存储中，直接连接 DeepSeek 的 OpenAI Responses 和 Anthropic Messages。分别完成 4 个持久化回合，用时 10.689 秒和 10.045 秒。验证正常流式输出、取消恢复草稿、重复 Enter、编辑后提交、源和工作区字节、日志切点 SHA-256、`/resume` 返回源线程，以及移走祖先 journal 后独立冷恢复。两份手动验收配置使用 `deepseek-flash` 和关闭推理的设置。
- `make verify` 通过，用时 69.356 秒，覆盖全量普通测试、静态检查和已审计 race。定向传输 race 通过，用时 1.988 秒，其中新增的 gzip 空闲子场景约 0.03 秒。`GOOS=linux GOARCH=amd64 go build ./...` 通过。运行验收在 macOS arm64 完成，Linux 仅验证交叉构建，Windows 未执行。

## 两项变更联合复验

- 当前 worktree 在 macOS arm64 上的 `make verify` 通过，用时 60.649 秒。另有 9 项定向 race 用例通过，覆盖压缩首帧和解码错误清理、双 Provider 原生续写、历史草稿发送及回收。10 个会话选择器 PTY 场景通过，用时 7.930 秒，会话命名和 Plan Mode 的双 Provider PTY 冒烟也通过。Linux amd64 交叉构建通过。
- 重新构建 CLI，以真实 PTY 连接两种本地 gzip SSE 夹具，各完成 5 次请求，精确核对请求中的输入顺序、取消时的保留草稿、原始日志切点、源文件以及冷恢复。
- 本机直连 DNS 解析失败后，验收进程使用系统已有代理完成真实请求。Anthropic Messages 经 HTTPS 代理，OpenAI Responses 经 SOCKS 代理，各完成 4 个持久化回合，用时分别为 7.124 秒和 7.559 秒。终态通过原生助手样本和 `turn_completed` 验证，核对父子日志、原始前缀 SHA-256、输入顺序、`/resume` 返回源线程及移走祖先 journal 后冷恢复。在真实 Responses HTTPS 探针中另确认 gzip 响应包含 `response.completed`。代理只用于测试进程。
- 两项变更的四份主规格均与归档增量一致，73 项 OpenSpec 全量严格校验通过。 [历史回合分支验收](../2026-11-24-add-historical-session-branch/verification.md)
