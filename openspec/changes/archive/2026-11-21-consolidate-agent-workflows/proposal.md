## Why

#48 已共享双 Provider 的采样、原生窗口和回放骨架。后续审查发现，OpenAI 普通 sample 恢复仍跳过计量对账，Provider 准入和工具审批的所有权可交错，Session 与实时 transcript 还会反复消费相同历史。

## What Changes

- 第 1 批在在线恢复和离线预览补齐双 Provider 普通 sample 计量对账，拒绝与原生事实不符的记录。
- 第 2 批为 Conversation 建立统一操作准入、窗口取样和资源释放边界，修复输出背压下的取消以及 Responses SSE 已接收的终态后事件处理。
- 第 3 批统一 Patch、Write、Exec 的审批决定和 durable 收口，复用 Patch 与 Write 的暂存所有权校验。
- 第 4 批复用已验证的 Session 事实和 Provider checkpoint 解码结果，补齐恢复取消、writer 确定失败分类与 Catalog 私有原子快照与实际文件句柄检查。
- 第 5 批增量更新 transcript，收敛 TUI 焦点与续行处理，修复 headless 终态写入和命令响应校验。
- 第 6 批收敛剩余 Provider 输入、请求证据、工具装配和局部重复，清理已确认无消费者的实现。
- 第 6 批复审补齐工具取消后的可恢复性、文件已提交结果、会话选择器的快照一致性，并收窄 Provider 流与宿主接口。工具分片线性累加，共享两种 Responses 传输的共同字段和在线、离线压缩用量校验。
- 第 6 批再次复审补齐 headless 命令级拒绝、Responses 消息和错误帧的跨传输语义、时钟回拨下 Catalog 的可发现性，以及工具组的严格回放与错误传播。缩小活跃窗口复制与本地命令预解析，清理经核实的无消费者入口。
- 在一个 change 中逐批交付。每批以现有回归、临时确定性场景和完整质量门验收，提交审查结论后等待用户确认，再开始下一批。

### Non-Goals

- Provider 原生 wire、合法请求字节、缓存指纹、现行 journal 和 headless v1 结构沿用既有契约。已识别的无效证据、错误准入与取消行为按各批 delta 修正。
- 范围限于本次审查确认的问题。现有 Provider 专用策略、独立格式边界、名称和文件匹配各自的排序规则继续由所属边界负责。
- 验收使用编译后的真实入口，复用现有测试与固定 fixture。补充端到端场景放在临时或忽略目录，关键链路可保留最小回归测试。原始测试报告不进入仓库。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `architecture/single-current-contract` 约束共享执行、验证与装配边界，明确本次允许的行为修正。
- `provider/usage-accounting` 双方普通 sample 的共享计量与原生事实对账。
- `provider/stream-lifecycle` Conversation 排他准入与背压下唯一终态。
- `provider/reasoning-display` Provider 私有原生归并与语义展示事件的边界。
- `provider/openai-responses-text` HTTP 已接收的终态后事件拒绝。
- `tool/approval` 实际选项校验、Stop/取消准入和准确的审批来源。
- `tool/file-patch` 暂存对象身份、发布与清理的所有权约束。
- `session/jsonl-store` 写入前确定失败与未知持久状态的区别。
- `session/resume` 原生恢复检查使用调用方取消并保留完整验证顺序。
- `session/catalog` 用私有原子快照承载可重建目录，校验实际打开对象和当前结构。
- `tui/session-picker` 同一次协调发布范围列表与提示，分页前排除当前会话。
- `provider/transcript-projection` 实时原生提交按新增事实有序增量发布。
- `tui/basic-chat` 阻塞交互焦点与续行优先级。
- `headless/stream-control` 终态输出清理与 v1 响应关联校验。

## Impact

变更涉及 Provider、transport、Runtime、Tool、Session/Catalog、App、TUI、headless 和 codec 内部接口。设计以 `0f700a0` 的生产源码为对照，应用时按实际接收提交固定各批基线。Catalog 保留持久派生索引和当前公开行为。第 4 批切换到单一快照格式，遗留派生库由部署前留存的 JSONL 重新投影，无须迁移。实施验收沉淀相关架构、路线图和踩坑说明。
