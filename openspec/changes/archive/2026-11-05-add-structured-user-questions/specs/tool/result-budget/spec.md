## ADDED Requirements

### Requirement: User decisions are complete before acceptance and persistence

提问最终 preview SHALL 最多 128 KiB，并计入现有每 sample 512 KiB/每 turn 2 MiB 和后续安全结果预留。到达提问 slot 时确定 allowance；不足以表示最小合法结果时在发布问卷前返回有界预算错误。回答接收前必须纯内存验证完整 preview、必要 metadata、Session record 与 Provider output 的实际编码大小，不能仅计原始文字字节。

超限回答 MUST 被拒绝并允许宿主保留草稿修改，不写回答、不关闭请求、不截断选择/说明、不保存部分成功决定、不自动转存 artifact。合法回答接收后，既定 allowance 不得被其他结果抢占；同轮 steering 不重置预算。已 durable 回答恢复只复用原 bytes，不重新裁剪或渲染成新模型结果。

#### Scenario: Reject escaping expansion
- **WHEN** 回答原始文字小于 16 KiB 但 JSON 转义后的完整 preview 超过当前 allowance
- **THEN** 拒绝 admission，问卷和草稿仍可编辑，journal 与预算账本不变

#### Scenario: Exhaust before asking
- **WHEN** 前序结果已耗尽提问可用正文额度
- **THEN** 不发布问卷或等待用户，按原 call slot 生成预算错误并沿用既有耗尽收口规则

#### Scenario: Keep an accepted answer complete
- **WHEN** 完整回答已通过编码和预算检查并被接收，随后 turn 取消
- **THEN** owner 先取得完整冻结结果的确定 durable resolution，不把已接收选择缩短成成功片段
