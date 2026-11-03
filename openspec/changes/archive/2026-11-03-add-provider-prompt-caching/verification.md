# add-provider-prompt-caching 验收记录

日期：2026-11-03。平台：本机 macOS / Go 1.24.13。范围保持已确认完整方案，包括双 Provider 显式策略、两 TTL、可选 WebSocket、durable 增量、摘要、诊断、TUI 与 headless 查询。

## 为什么推进这一能力

原生历史、七工具、恢复和手动/自动压缩已有真实消费者，缓存可复用这些已验证边界，不需要提前引入扩展 registry 或重写 AgentLoop。产品采用 Claude 风格本地 /cache 与 /context，内核保留两家原生语义及 Codex 的连接内精确增量；完整参考文件、提交和取舍见 design 的证据表。

## V1–V16 到实际证据

下表中的测试均由全量 `make verify` 覆盖；针对性命令用于开发期定位，不替代完整质量门。既有测试原断言保留，关闭顺序按本次已批准契约更新。

| 编号 | 实际测试与 fixture | 已核对的业务证据 |
|---|---|---|
| V1 | `internal/domain/cache_test.go`、`internal/config/cache_test.go`、`internal/app/cache_e2e_test.go:TestInvalidCacheConfigurationHasNoNetworkOrSessionSideEffects`、两家 `cache_test.go` | 缺省/非法/重复/null/跨 family 与环境合并；非法配置零网络、未创建 Session，构造绑定合法身份 |
| V2 | `internal/provider/anthropic/cache_test.go`、`testdata/cache_5m_request.golden.json`、`cache_1h_request.golden.json` | 0–4 断点、工具/项目/锚点/尾部去重、19/20 blocks、两 TTL、禁用省略、原生冲突拒绝与重复编译稳定 |
| V3 | 两家 `restore_test.go`、`artifact_catalog_test.go`、`steering_test.go`、Anthropic `TestCacheOverlayPreservesRawAndHistory`、`internal/provider/request_baseline_test.go` | thinking/signature/redacted/encrypted/phase/未知扩展不丢失；工具 schema 与动态执行状态分离，native bytes 不被覆盖 |
| V4 | `internal/domain/cache_test.go`、OpenAI `TestThreadCacheKeySurvivesNativeRestore`、`cache_thread_request.golden.json`、`internal/context/planning_test.go` | 命名空间固定、同 thread 稳定/跨 thread 分离，身份不污染内容分段；key 不保存到 Session/诊断 |
| V5 | 两家 `compaction_test.go`、`compaction_stream_test.go`；缓存摘要 golden；OpenAI `TestWebSocketSummaryUsesSeparateFullConnectionAndResets`；app `TestCachedAutomaticCompactionRealProvidersAndResume` | 手动/自动、两 TTL/线程 key、成功/失败/取消/重复、指导后缀不缓存、旧窗口和授权保持、摘要独立 full 连接、下一普通 full |
| V6 | OpenAI `TestWebSocketDurableIncrementalAndReset`、`TestWebSocketEnvelopeCoversEveryLogicalProperty`；`websocket_full.golden.json`、`websocket_incremental.golden.json`；两家 reducer/stream/integration suites | 固定 full/incremental wire，省略 stream、保持 store=false，共用严格 reducer/native 输出与唯一终态规则 |
| V7 | `internal/provider/request_baseline_test.go`、OpenAI `TestEveryLogicalPropertyChangeInvalidatesContinuation`、`TestWebSocketDurableIncrementalAndReset`、app `TestWebSocketToolsAndCacheQueryEndToEnd` | 全部非 input 属性逐字段回归；原生 prefix + durable outputs + suffix 精确字节重组；三工具有序结果及随后 steering 输入 |
| V8 | `internal/runtime/cache_test.go`、`cache_websocket_test.go:TestWebSocketDurabilityAndOuterCancellation`、既有 runtime artifact/exec/input/compaction 故障 suites | Sync 返回前不推进；Sync 失败无成功观测/续写；外层取消显式重置；discard/finalize 保持单次提交，工具执行仍受原 ledger 约束 |
| V9 | OpenAI `TestWebSocketHandshakeFallbackIsSticky`、`TestWebSocketPostCreateErrorDoesNotRetry`；transport `TestWSClientHandshakeSecurity` | 404/405/426 固定回退、401/403/TLS/redirect/cancel 不回退；生成写入后的 error 不重发，下一显式输入 full；错误不带私有正文 |
| V10 | `internal/provider/transport/websocket_test.go`、`internal/app/cache_websocket_test.go`、`internal/app/app_test.go`、`internal/runtime/exec_shutdown_test.go` | ws/wss、路径/鉴权、无压缩、分片/UTF-8/二进制/超限、注入读写期限、阻塞原 write owner、握手/活动/空闲 Close、幂等 done、关闭顺序与 lease 释放 |
| V11 | app `TestCachedProvidersSameThreadLiveAndRestoredCanonicalRequests`、`TestCachedImmutableExecFixtureRestoresRealProviderNativePairs`、`TestCachedCompactionFixedFixturesResumeAndRepair`；两家 `TestCheckpointWindowAndRestoredRequest`；OpenAI `TestWebSocketDurableIncrementalAndReset` | 同 ThreadID 的完整 canonical bytes 直接比较；checkpoint 两 TTL / key 恢复；WS 独立 full bytes、native/content/request fingerprints、原生顺序；current fixture 只读，无 encoder 运行时生成历史 |
| V12 | `internal/context/cache_diagnostics_test.go`、`internal/domain/cache_receipt_test.go`、`internal/protocol/cache_test.go`、`internal/runtime/cache_test.go` | 首个内容/revision/added/removed/history append/opaque/恢复/切窗原因；策略独立；known zero/unknown/N/A/零分母/极值与溢出，摘要隔离 |
| V13 | runtime `TestCacheQueryOwnerIsNonblockingAndOrdered`、`TestCacheObservationOnlyAfterDurableFinalize`、`TestCacheSummaryObservationDoesNotReplaceSample`、`TestCacheDiagnosticsRemainBoundedAndInstanceScoped`；app WS E2E | 活动 stream 查询不等待、response 先于 snapshot、重复 request 拒绝、重复查询不推进；固定五来源/最近三项、10000 次更新有界且实例隔离 |
| V14 | `internal/tui/cache_test.go`、`context_test.go` 与固定 `testdata/cache*.golden` | /cache 精确解析、参数拒绝保稿、不进入输入回忆、关闭/重开与迟到 ack/error/snapshot 隔离、草稿/光标、Ctrl+C/Esc、窄屏/极小屏/无色、恢复/未观测/unknown、known zero、窄屏 unknown cache_write、控制字符和 resize |
| V15 | `internal/headless/cache_test.go`、`testdata/cache.golden.jsonl`、既有 stream/input/runner/app headless suites | 外部 v1 严格 decoder、任意 chunk/UTF-8/尾行、重复/未知/null/超限、显式 snapshot DTO/单 writer、顺序、EOF/shutdown/断管；/cache 的 input.submit 保持正文，一次性输出原 golden 不变 |
| V16 | 新增本地集成的 secret 哨兵、`internal/protocol/cache_test.go`、`internal/headless/cache_test.go`、`internal/app/cache_websocket_test.go`、`internal/architecture/cache_boundary_test.go` | 不使用真实 key，不默认外网；不安全/绝对路径形状模型名只显示摘要身份，不改变真实请求；无 key/URL/prompt/opaque/凭据进入诊断、错误或 Session；第三方 WS 生产 import 仅 transport，Resty/Sonic 边界保持 |

## 质量门结果

- 第一轮 `make verify` 已退出 0，包含 gofmt、go vet、Staticcheck、架构、全量测试与 race。
- 最终代码及新增回归的 `make verify` 退出 0：gofmt、go vet、Staticcheck、架构边界、全量测试与全量 race 均通过；最终 app race 193.009 秒、runtime race 27.356 秒。
- `openspec validate add-provider-prompt-caching --strict` 退出 0，change 有效。
- `openspec validate --specs --strict` 退出 0：42 项通过、0 失败；长 requirement 的 INFO 提示不影响通过。
- `git diff --check` 退出 0；53 个本地文档链接有效，命名测试引用均存在。
- 主 specs 已同步 10 个 capability，新增 18 条 requirement、合并 5 条；其余 requirement 逐字保留、原场景不丢失，重复合并检查幂等。
- `GOPROXY=off go mod tidy` 退出 0；`github.com/coder/websocket v1.8.14` 的模块与 go.mod checksum 固定，ISC 许可，未引入官方模型 SDK。

既有真实子进程 Session suites 仍参加质量门，包含跨 Repository/进程 lease 竞争、崩溃释放、占用禁止 repair、续写 seq 单调与损坏/失败路径不改 journal prefix。所有新取消/Sync/读写/关闭边界使用 channel barrier、注入期限或故障注入，没有增加 sleep 或概率竞态断言。

## 契约复核与限制

- Factory 只有显式 ThreadID 的当前构造/恢复方法；PreparedSample 与 PreparedCompaction 都必须具有当前请求收据。
- 新增词汇属于进程内控制与外部 headless v1：query_cache/cache_snapshot 和 cache.query/cache.snapshot。没有新增 Session record，也没有 schema_version/payload_version、迁移或历史 reader。
- 三配置互相独立；只有已实现且显式启用的 capability 可为 true，会话安全回退后 PreviousResponse 为 false。
- 同步 ADR-0009、总体架构、Roadmap、pitfall、README 与配置示例。主 specs 已同步，变更已完成归档。
- 本次使用 macOS 本地模拟 HTTP/ws/wss 与受控 fault。没有调用真实 Anthropic/OpenAI 凭据；不声称实测远端命中率、TTL 驱逐、费用或性能收益。Linux/Windows 原生 smoke 未运行；已有平台不支持策略保持失败关闭，不据 Darwin 结果宣称跨平台等价。

## 归档前一致性复核（2026-11-03）

- schema 为 spec-driven，四项 artifacts 均完成，42/42 tasks 完成，无未完成项归档例外。
- 由当前 CLI status 提供的全部十个 delta 路径逐一比较，23 条 requirement 与主 specs 正文及场景完全一致，无待同步差异。
- 复核 Provider 缓存覆盖、WebSocket 选择与重置、Runtime durable publication、关闭顺序、capability 声明和 Session 事实词汇；缓存查询未成为 Session record，Session 源码与 current fixtures 未改动。
- 验收记录的 21 处命名测试引用及 17 个显式 Go 路径均存在；架构、Roadmap、pitfall 与实际实现和延期边界一致。
- 本次归档前重新执行 make verify、change strict 与主 specs strict 校验，均退出 0；主 specs 为 42 项通过、0 失败。
- 整个 change 目录移动时逐文件 SHA-256 一致，包含 .openspec.yaml；归档后更新 ADR、Roadmap、pitfall 的引用并复查本地链接。
