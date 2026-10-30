## MODIFIED Requirements

### Requirement: Range results are bounded and preserve byte progress

回读 SHALL 返回实际 offset、next_offset、stored EOF、source_complete 与有界正文。offset 大于保存长度 MUST 返回 invalid_range，等于长度返回空 EOF；offset 位于有效 UTF-8 字符内部 MUST 拒绝。页尾不得切断有效字符，limit 不足以容纳下一个字符时 MUST 返回 invalid_range 和最小所需 limit；无效原始字节 SHALL 稳定转义为 \xHH。完整模型结果 MUST 不超过 256 KiB，转义扩大时减少实际消费 bytes，next_offset 必须准确且非 EOF 成功页必须前进。读取结果不得再次生成 artifact。

#### Scenario: Read a partial artifact to stored end
- **WHEN** 页到达 partial 对象保存末尾
- **THEN** stored EOF 为 true、source_complete 为 false，不声称原输出完整

#### Scenario: Preserve multibyte boundaries
- **WHEN** UTF-8 多字节字符位于页边界或 limit 小于其长度
- **THEN** 系统返回完整字符边界内的页或明确 invalid_range，连续成功页无重复/丢字节或零进度循环

#### Scenario: Bound binary expansion
- **WHEN** 页含大量无效字节和终端控制序列
- **THEN** 模型结果转义且不超 256 KiB，next_offset 对应真实消费原始 bytes，TUI 使用安全显示副本

ReadArtifact SHALL 支持当前 thread 已授权的 search_result，并将采集覆盖与保存 EOF 分别报告。页头、转义和范围字段 MUST 计入实际 allowance；从已取得的有界 raw window 渲染时即计算真实 next_offset，不得先冻结游标再裁短正文。若额度不能形成非 EOF 前进页，返回有界 result_budget_exceeded。不得为预算重新读取文件或生成递归 artifact/Write 观察。

#### Scenario: Page under reduced allowance
- **WHEN** 输入 limit 为 64 KiB 但分配额度较小且正文存在转义放大
- **THEN** 成功页不超 allowance 且 next_offset 等于真实消费 bytes，后续页不重复或跳过；无法前进则明确预算错误
