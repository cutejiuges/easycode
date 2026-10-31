# ADR-0008：保留完整原生历史，以 durable checkpoint 切换有效窗口

- 状态：accepted
- 日期：2026-10-31
- Roadmap 阶段：P4
- OpenSpec：[add-durable-manual-compaction](../../../openspec/changes/archive/2026-10-31-add-durable-manual-compaction/design.md)

## 背景与选择

长会话需要缩减模型输入，同时保留原始历史、工具详情和恢复能力。删除或重写 native history 会破坏 item 顺序、opaque reasoning、工具关联和 transcript cursor；从公共语义消息反向构造请求无法保真。

由各 Provider 保存完整原生条目与独立 checkpoint。checkpoint 绑定来源模型、wire、committed/context revision、前驱、有效窗口 digest、版本化摘要指示身份，以及 Provider 私有摘要包装和生成 provenance。请求只编译当前摘要与后续增量；完整 HistoryProjector 保留全部原始条目。共享 ActiveContextView 单独表达摘要，不伪造用户输入或 turn。

## 生命周期与所有权

AgentLoop 只在 idle 且队列为空时预检并接收独立 CompactionID。started Sync 先于唯一摘要请求，候选预算与缩减检查先于 completed Sync，finalize 后才公布成功。prepared resolution 复用现有骨架。普通 turn 和摘要 worker 互斥；worker 在提交边界产生不可变查询快照，owner 只缓存和投影，避免跨提交读取不一致。

取消以 writer admission 为线性化点。已接收的完成写入必须得到确定结果；成功仍安装，未知则 poison。120 秒网络期限、shutdown 与输出失败均由唯一 owner 取消并等待清理；超时返回不代表资源可复用。失败单独保存 observed/unavailable usage，不混入普通 turn 计费。

## 安全与恢复

摘要是数据，不是授权。wire 禁止工具选择，consumer 再拒绝工具调用；不从摘要重建 grant、artifact reference 或文件观察，不自动读取旧文件。完整源链、原生语法、摘要包装和 usage 校验通过后，才按 ADR-0007 修复物理尾部。started-only 本地追加 interrupted，不重新请求摘要。

## 契约与代价

当前 Session canary 为 7，native canary 为 3，外部 headless 保持独立 v1；旧开发格式失败关闭，不提供双 reader 或原地迁移。原始 JSONL 与生成 provenance 保留，磁盘体积不会因压缩缩小；本地估算不是精确 tokenizer，无法严格缩减时明确失败。

自动压缩、Hook、分块/局部摘要、跨 Provider fork 与专用 compaction API 须另行设计。

## 验证

双 Provider 的 compaction_test.go、compaction_stream_test.go 与 steering_test.go 验证窗口、工具闭环、opaque 和恢复请求等价；internal/runtime/compaction_test.go 验证 durability、取消、队列与 cleanup；app 的 compaction fixture/e2e/headless/security 测试验证实际 Session 恢复、无额外副作用与输出隔离；internal/session/compaction_process_test.go 验证真实进程 lease 和崩溃释放。
