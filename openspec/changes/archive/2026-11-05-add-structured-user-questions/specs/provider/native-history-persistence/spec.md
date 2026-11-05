## ADDED Requirements

### Requirement: Committed question answers restore byte for byte

两家 Provider SHALL 在现有 sample/tool_outputs 当前契约内保存提问调用和冻结回答，不添加 entry kind、格式版本、游标、特殊 user_inputs 或额外可选 record。恢复必须验证 call ID、capability、wire kind、题目和回答关联、顺序及完整 preview，任何错配整体失败且不 repair 或返回部分 Conversation。

相同 catalog、项目指令、配置与已提交事实下，提问完成或 result/output 缺口补偿后的 uninterrupted/restored 下一完整请求 canonical bytes、原生 item 顺序、context footprint/revision 与内容/请求 fingerprint MUST 相同。Anthropic signature/redacted thinking、OpenAI reasoning/encrypted content/phase 原样保留。恢复不能重新生成回答、读取终端或依赖请求曾被 UI 展示。

#### Scenario: Restore a mixed native call group
- **WHEN** Read、Question、Patch results 已 durable，而 native outputs 尚未提交
- **THEN** 恢复按原 call index 用冻结 bytes 补齐同一组 outputs，无交互、工具和网络执行

#### Scenario: Compare uninterrupted and restored requests
- **WHEN** 两家分别恢复含提问及 opaque reasoning 的固定 current fixture
- **THEN** 下一完整请求 bytes、原生顺序、footprint 和 fingerprints 与不中断路径相同

#### Scenario: Reject a forged answer association
- **WHEN** checksum 合法但 question result 与 ready/native 的题目或 call identity 不匹配
- **THEN** 完整恢复失败，journal bytes 保持不变且无交互副作用
