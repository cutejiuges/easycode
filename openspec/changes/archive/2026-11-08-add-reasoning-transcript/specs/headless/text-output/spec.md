## ADDED Requirements

### Requirement: Headless validates internal reasoning without publishing it

一次性 print、json 及 stream-json SHALL 严格校验内部推理 scope/stage、提交来源/绑定和失败 sample resolution，再消费而不生成新的外部输出。正常成功 sample 不允许未关闭块；失败/取消允许活动块由真实 sample resolution 一次闭合。input commit 不能冒充 sample commit，提交不确定或已保存后的失败不能被解码为新的外部成功事件。

推理文字、unavailable、范围、映射、内部身份和保存状态 MUST NOT 混入最终回答、stderr 或外部 JSONL；既有外部词汇、字段、响应顺序、退出、背压、EOF drain 和 cleanup owner 保持不变。未知内部事件、非法字段/状态、错配身份及 terminal 后事件仍按安全 stream_protocol_error 失败；不能用忽略全部未知事件实现。Provider 边界已消费的合法迟到 done 不应再作为内部事件到达。

#### Scenario: Preserve all three output modes
- **WHEN** 两家 Provider 分别返回推理、工具和普通回答，在三种模式运行
- **THEN** stdout 只含既有回答/机器事件，推理正文、占位提示和新增内部解析均不出现

#### Scenario: Close incomplete reasoning on a genuine failure
- **WHEN** 若干推理块只有 Started/Delta，随后收到合法 not_committed 或 uncertain sample failure resolution
- **THEN** 校验状态收口一次，外部沿既有失败 code/message 结束，不因缺块 Completed 再报第二个错误

#### Scenario: Reject malformed internal reasoning
- **WHEN** 内部推理/提交/失败解析含重复字段、null、错配 sample、非法绑定或终态后事件
- **THEN** 按既有安全协议错误收口且保留 owner 清理，不伪造成功，不泄漏载荷
