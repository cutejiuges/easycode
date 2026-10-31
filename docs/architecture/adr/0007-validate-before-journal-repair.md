# ADR-0007：完整验证后才修复 Session 尾部

- 状态：accepted
- 日期：2026-10-31
- Roadmap 阶段：P4
- OpenSpec：[add-durable-manual-compaction](../../../openspec/changes/archive/2026-10-31-add-durable-manual-compaction/design.md)

## 背景与选择

只验证 JSONL 结构不能证明其 committed 前缀可恢复。若 Loader 发现半行就立即截断，而 replay 或 Provider 随后拒绝此前记录，恢复失败仍会改变用户文件。压缩 checkpoint 增加来源链校验后，这个顺序问题更明显。

选择只读 Load 与显式 Repair 分离。app 在同一 exclusive lease 下依次验证结构、replay、Provider 配置和全部原生内容，之后才修复尾部并 Sync，再转交 writer。Catalog 无需完整 Provider 恢复，因此始终只读 journal，只索引可验证的 committed 前缀。

## 所有权与取消

修复凭据为私有对象，绑定产生它的 lease、扫描大小、完整内容 digest 与截断位置；公共记录或报告的修改不能改变修复授权。重复消费、跨 lease、扫描后文件变化均拒绝。待修复及 poisoned lease 不允许 writer 接管。

截断接收前取消保持原文件；接收后必须等待 Truncate/Sync 确定结果，失败 poison。lease 的 I/O 完成信号使 Close 保持所有权到操作完成；关闭开始后拒绝新 I/O，重复关闭等待同一最终结果。锁只保护有界状态转换，不执行 I/O 或等待。

## 代价与边界

修复前需要重新读取文件校验 digest，增加一次顺序读，但不保存第二份全文。文件互斥仍依赖协作进程遵循 advisory lease，不能保证抵御忽略锁的外部进程持续改写。

本决策不改变 Provider wire、普通请求、缓存内容或持久化格式。checkpoint 与 Session canary 的改变由同一 OpenSpec 和 ADR-0008 定义。

## 验证

`internal/session/repair_test.go` 覆盖只读扫描、凭据、取消、内容变化及 Truncate/Sync 故障；`internal/app/repair_order_test.go` 验证 Provider/模型校验失败时完整 bytes 不变。原跨进程 lease、崩溃释放、descriptor-bound 修复及 Catalog 测试保留，并改为显式修复或只读断言。
