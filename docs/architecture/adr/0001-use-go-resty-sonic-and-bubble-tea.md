# ADR-0001：使用 Go、Resty v3、Sonic 和 Bubble Tea

- 状态：accepted
- 日期：2026-09-18
- Roadmap 阶段：P0

## 背景

EasyCode 需要以单二进制形式提供本地 coding agent，同时处理双 Provider 流式协议、工具并发、Session、缓存和终端交互。

## 约束

- 工程语言由项目负责人确定为 Go。
- HTTP 与 SSE 统一使用 Resty v3。
- JSON 统一使用 Sonic。
- TUI 使用 Bubble Tea。
- 必须支持任意 `base_url`，不绑定官方模型 SDK。
- 必须保持请求字节和缓存前缀稳定。

## 候选方案

1. Go + Resty v3 + Sonic + Bubble Tea。
2. Rust + reqwest + Ratatui。
3. TypeScript + fetch + Ink。

## 最终决策

采用 Go 1.24+：

- `resty.dev/v3` 负责 HTTP/SSE。
- `github.com/bytedance/sonic` 负责 JSON。
- `github.com/charmbracelet/bubbletea` 负责 TUI。
- 标准库 `context`、goroutine 和 channel 负责取消与并发。
- 标准库 `log/slog` 负责结构化日志。

## 代价与风险

- Resty v3 在决策时最新版本为 `v3.0.0-rc.4`，稳定版升级前需要完整契约回归。
- Sonic 默认高性能配置不保证 map key 稳定排序；缓存敏感路径必须使用 `sonic.ConfigStd` 或显式开启稳定排序。
- Bubble Tea update loop 不能直接执行网络和工具副作用，必须通过 command/message 返回结果。

## Provider 与缓存影响

- Provider request 先由 Sonic 生成确定字节，再交给 Resty，禁止混用 Resty 自动 JSON 编码。
- SSE frame 由 Resty 解析，但 Anthropic/OpenAI event reducer 保持独立。
- 请求、Tool Schema 和 CachePlan 使用稳定序列化并具有 golden test。

## Session/协议迁移影响

当前为初始架构，无旧 Session 迁移。未来 JSON schema 必须显式版本化，不能依赖 Sonic 的默认兼容行为。

## 验证与回归测试

- `internal/codec/json_test.go` 验证 map key 稳定排序。
- `internal/context/cache_plan_test.go` 验证缓存稳定前缀。
- `internal/provider/transport/client_test.go` 验证 Resty SSE 与 Sonic request body。
- `internal/tui/model_test.go` 验证 Bubble Tea projection。
- 全工程执行 `go vet ./...`、`go test ./...` 和 `go test -race ./...`。

## 回滚或替代方案

如果 Resty v3 的 SSE 无法满足协议精确性，可以在 `provider/transport` 内使用 Resty 的 raw response body 实现独立 SSE frame parser，但 HTTP transport 仍保持 Resty v3，且不得改变上层 Provider Kernel。

