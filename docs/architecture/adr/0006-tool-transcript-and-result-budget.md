# ADR-0006：有序工具记录与按调用顺序冻结的结果预算

- 状态：accepted
- 日期：2026-10-30
- Roadmap 阶段：P3
- OpenSpec：[add-tool-transcript-and-result-budget](../../../openspec/changes/archive/2026-10-30-add-tool-transcript-and-result-budget/design.md)

## 背景与选择

仅有命令详情无法展示 Read/Search 等工具，统一语义历史又不能保留 text/call/text 的原生顺序。并行工具各自按固定上限返回会放大单轮上下文，显示截断也无法说明模型究竟看到哪些内容。

Claude Code 2.1.88 的工具 UI、相邻读取计数、紧凑主线与展开行为作为产品参考；Codex `6f8bf683c` 的活动身份、有界输出、顺序提交与冻结历史作为内核参考。完整只读调研路径见关联 design。没有复制参考工程实现或添加其构建依赖；复用本工程的 FD 校验、发布与配额机制、头尾输出、审批指纹和 durable 提交骨架。

## 决策与边界

Provider 提供单向 `TranscriptProjector`，按 native entry/item 生成封闭的文本和调用锚点。它与 token 估算用的 SemanticHistoryView 分离，不用于反向生成 Provider 请求。app 用 native commit seq 和已验证的 ready 身份连接工具事实，失败关闭缺失、重复、错序和能力错配。TUI 只消费 typed 活动和有界查询，不解码 native、读文件或操作 Session。

Runtime 唯一 turn owner 按模型调用顺序分配额度、finalize worker 材料、计费与提交。单调用 256 KiB（搜索 64 KiB），sample 512 KiB、turn 2 MiB，后续调用各预留 1 KiB。Read 拒绝超额并废弃覆盖证据；ReadArtifact 在窗口上计算真实游标；Exec 将额度加入审批指纹；Patch/Write 保留真实终态。正文耗尽锁定后停止新增副作用，但仍完成已接收调用的配对、清理和 native outputs。

搜索保存选中原始行，预算裁剪仅影响模型预览及 matches。命令双流 capture 与搜索文本 capture 生命周期分别拥有，包内共享发布和配额骨架。对象完整性、采集覆盖、模型省略和界面折叠是四个独立维度。结果 Sync 前冻结保存状态和预算，Sync 后授权；恢复不访问对象、不重新截断。

宿主查询按稳定位置返回最多 50 个逻辑项、64 KiB 正文或 raw 范围。主线保留最近逻辑页，浏览缓存最多四页；反向查询不依赖无限游标栈。`transcript_committed` 在 finalize 后驱动 provisional 替换，查询带提交上界并使用 generation 拒绝迟到回复。失败增量只标为当前进程的 unfinished 文本。

## 当前格式与代价

Session 唯一当前 canary 从 5 替换为 6，native revision 保持 2，外部 headless JSONL 保持 v1。新增进程内 UI 事件不写 journal，原 record vocabulary 保持不变。当前 fixture 离线替换，旧开发 shape 失败关闭，不提供双 reader。

保留现有全量 Session replay 内存模型；每次逻辑分页不复制完整历史。native finalize 后投影仍需处理当前 native 历史，这不是分页缓存的职责。artifact 范围查询继续对同一 FD 校验完整对象，最多读取 16 MiB 校验数据，返回材料保持有界。

## 验证与回滚

核心回归位于双 Provider `transcript_test.go`、`internal/app/transcript_projection_test.go`、`internal/runtime/result_budget_test.go`、`internal/session/artifact/text_capture_unix_test.go`、`internal/tui/transcript_state_test.go` 与 `transcript_browser_test.go`。最终平台和质量门证据以 OpenSpec 验收记录为准。回滚须使用与旧 canary 匹配的独立开发数据，不能让旧 reader 原地修改新 journal。
