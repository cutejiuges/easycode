## MODIFIED Requirements

### Requirement: Live committed projection matches restored transcript

成功 sample SHALL 以相同 native 投影生成 live committed 与 restored 内容，包含准确来源、文字和 unavailable。活动 `{turn,sample,block}` SHALL 通过经过验证的纯内存绑定连接 `{sequence,item}`，不依赖正文相等、最新列表是否保留目标或工具 ready facts。streaming 推理/文本与 block Completed 均不证明已保存；只有真实提交结果才能解析保存状态。

成功提交后临时范围恰好替换一次；提交结果不确定时 MUST 明示未知，恢复以严格验证的实际事实为准，不推断记录必定不存在。partial tool arguments 仍不得产生 ready、审批或执行；先前已提交用户输入和 sample 保留。未提交失败内容不得伪造恢复历史。

启动与恢复 SHALL 完整核对 native 与 tool facts 的锚定。实时 commit SHALL 仅投影此次已提交原生条目及其相关事实，原子更新受影响的条目和索引。不可见原生条目仍按真实 ordinal 推进，复合 Stop 交付同批发布。已有条目的 cursor 和正文不会因追加或计划归并而重算或重编号。当前工具事实错配的拒绝同样覆盖无界面宿主。

#### Scenario: Finalize streamed text without duplication
- **WHEN** 推理和文本 sample 成功 Sync/finalize
- **THEN** 按真实映射替换临时范围，恢复后内容和 cursor 相同，无重复

#### Scenario: Disconnect during arguments
- **WHEN** 工具参数未完整时断线且 sample 未进入提交
- **THEN** 不展示 ready/执行，本 sample 明确未保存，先前 durable 输入与结果保留

#### Scenario: Commit a selection outside the latest list page
- **WHEN** 用户正在阅读的活动块提交后不在最新 50 项详情页
- **THEN** 仍通过精确 cursor 读取相同正文，不退回正文搜索或错选其他项

#### Scenario: Append an atomic transcript suffix
- **WHEN** 已有长历史的会话完成一个复合 Stop 提交和后续带工具调用的 sample
- **THEN** 每次实时发布只访问新原生位置及相关事实，读者只能观察完整发布，恢复后 cursor、顺序、正文和工具关联一致
