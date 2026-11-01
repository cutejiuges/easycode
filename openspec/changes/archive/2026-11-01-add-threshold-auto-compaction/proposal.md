## Why

七工具、安全执行、同轮输入及手动压缩已完成，但长 coding turn 仍可能在工具续写时耗尽上下文，迫使用户中断工作后手动处理。阈值自动压缩承接已有 durable checkpoint 纵向切片，以较少的新基础设施改善持续工作的体验。

## What Changes

- 全工程审计后删除无业务需求的 native/checkpoint payload_version、固定 compaction prompt_revision 与 Catalog user_version 门禁；保留实际字段结构、Provider/wire/model、完整性与内容身份校验。

- 按用户明确修正删除 Session envelope 的 schema_version/payload_version、版本常量及版本门禁，只保留唯一当前结构；规约禁止擅自设计多版本兼容或迁移。

- 在显式配置窗口时默认启用可关闭的阈值自动压缩，未知估算不猜容量。
- 统一在下一 sample 的准备阶段检查初始/追加输入候选与完整 durable 工具 outputs；压缩后继续原任务，保持输入 ID、turn、sample/call 上限和工具授权。
- 复用双 Provider 原生摘要与 checkpoint，重构独占入口与已持有 owner 的内部执行骨架，禁止嵌套 goroutine 或重入 active 锁。
- 每个采样边界最多一次自动尝试；可恢复失败在旧窗口仍通过普通请求预检时继续，否则停止。相同来源抑制与进程级连续三次失败熔断共同避免反复消耗。
- 摘要以确定性包装接续原任务，不复述摘要或伪造新授权；TUI 提供 Claude 风格简洁压缩边界、可取消状态和历史详情；所有宿主共用 Runtime 行为。
- 不包含自动裁历史、服务端专用 compact、跨 Provider fork、Hooks 或后台压缩。

## Capabilities

### New Capabilities

- `context/auto-compaction`: 阈值、采样安全点、失败与取消、输入重建和跨宿主继续工作。

### Modified Capabilities

- `provider/native-history-persistence`: native/checkpoint 使用唯一当前无版本 opaque envelope。
- `session/catalog`: 按实际表结构验证和重建派生索引，移除内部 schema 版本门禁。

- `architecture/single-current-contract`: 明确无内部格式版本，未经用户明确要求不得引入兼容设计或内部版本门禁。

- `session/jsonl-store`: 不保留内部格式标记，直接更新唯一格式的强类型压缩 scope 和活动 turn 采样安全点的 replay 语法。
- `headless/stream-control`: 允许自动摘要通过原 submit 关联输出压缩事件，保持一次性输出不变。

保留现有手动压缩独立操作语义，不改变手动成功后等待输入的要求。

## Impact

涉及 internal/context、config、runtime、protocol、session、双 Provider、headless、tui、app 的装配与测试。新增自动触发来源及关联信息直接更新唯一当前字段结构与 fixture，不递增 Session 版本，不保留旧开发 reader；外部 headless JSONL v1 的既有必填字段保持，新增字段按既有严格契约测试核验。不新增依赖，不修改两个参考工程。实现阶段同步架构、Roadmap 与 pitfall，再执行 make verify；本提案不代表功能已完成。
