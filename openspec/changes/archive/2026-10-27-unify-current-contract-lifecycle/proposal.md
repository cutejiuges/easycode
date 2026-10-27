## Why

当前架构检查和质量门虽通过，会话关闭、工具审批与 JSON 边界仍存在未覆盖的约束缺口，且 Provider 保留仅供旧测试调用的简化构造入口。项目尚未上线，应在同一变更中收敛到唯一当前实现，避免继续累积生命周期分叉和隐式宽松解码。

## What Changes

- **BREAKING**：统一 TUI、一次性 headless 和 stream-json 的内部会话控制 owner；删除直接驱动 Runtime 的平行会话实现，宿主 adapter 只做命令与事件适配，保持已有 CLI 输出契约。
- 修复 TUI 背压、启动取消、关闭超时和后台分发清理；资源释放必须等待真实活动 owner 完成。
- 按职责拆分 Runtime 中的工具调度、审批与执行收口，以组合共享 durable 生命周期；统一 Patch/Write/Exec 的批准生效、取消接收点、结果关联验证和不确定结果处理，保留各 capability 的安全差异。
- **BREAKING**：PreparedSample 与 PreparedToolOutputs 各保留一个显式要求 finalize/discard 的构造入口，删除自动注入空 discard 的旧入口并迁移测试。
- 固定实例或值构造的 Sonic 配置，去除 canonical/checksum/fingerprint 路径对第三方可变全局 codec 的依赖；集中 JSON 基础校验并统一当前契约的严格解码。
- **BREAKING**：拒绝当前封闭 schema 不允许的 null、大小写别名、重复字段和有损 Unicode 等非法输入；不保留宽松旧 decoder。合法当前 bytes、原生 opaque 数据及真实边界 canary 保持其语义。
- 补充确定性故障回归、架构守卫和能力文档同步；上述问题在本 change 中全部验收，不拆出延期子 change。

## Capabilities

### New Capabilities

无；复用现有能力边界。

### Modified Capabilities

- `architecture/single-current-contract`：固定 codec 所有权、统一严格 JSON 边界、禁止旧构造入口残留，并扩充可执行守卫。
- `runtime/input-control`：所有聊天宿主使用同一控制生命周期，保证分发背压和关闭升级下的最终清理。
- `tool/approval`：明确共享执行接收点、grant 生效时机、policy 优先级、结果关联校验与不确定状态收口。
- `provider/native-history-persistence`：显式且唯一的 prepared commit 构造与互斥 finalize/discard 生命周期。

## Impact

主要涉及 `internal/runtime`、`internal/app`、`internal/headless`、`internal/tui`、`internal/provider`、`internal/codec`、`internal/tool`、`internal/session`、`internal/config` 和 `internal/architecture`，以及总体架构、Tool 专题、Roadmap、pitfall 文档。

不引入新依赖、历史 migration、双 reader、兼容开关、跨 Provider 转换或未来能力占位。保留既有外部 headless 格式、Provider wire、Session record vocabulary 与 canary；有效当前 fixture 不因纯重构而变更。涉及解码收紧的负例与工具失败语义直接采用唯一当前基线。需要双 Provider golden/cache、恢复等价、真实进程 lease 与 shutdown 故障注入回归，最终执行 `make verify`。
