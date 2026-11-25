## ADDED Requirements

### Requirement: Streaming prompt checks fail on missing interactive trust

stream-json v1 SHALL 维持 `input.submit` 的 strict-next 和 `input.append` 的 auto 归属。配置有匹配 `UserPromptSubmit` 声明时，已接纳输入所属 turn 通过现有 `turn.failed` 的 `error.code` 报告 `hook_trust_required`。strict-next 只有原有的 `control.response`、`turn.started`、`turn.failed` 顺序。auto 仍按各自 `request_id` 和 `entry_id` 输出 claimed 及 `input.status(state=returned, reason=session_failed)`，每个共享 turn 只按最初承载请求的 `input_id` 输出一次终态。关联可通过原有 `turn_id` 完成。脚本、信任审查、Provider 请求和原生输入均不发生。检查事实和交互状态不扩展 v1 帧或 `input.status` 枚举。失败后控制、EOF drain、关闭及进程退出码继续按既有流式契约处理。

#### Scenario: Reject a matched headless prompt without running its script

- **WHEN** v1 提交合法对话文本，且冻结快照存在匹配声明
- **THEN** 维持已有接纳和回合终态顺序，失败原因是 `hook_trust_required`，模型端点未收到请求

#### Scenario: Return each automatic input in an all-rejected turn

- **WHEN** v1 的两条 `input.append` 共享一个新的 turn，环境存在有匹配声明但无交互信任
- **THEN** 按原 `request_id`/`entry_id` 各发布一次返还及共享 `turn_id`。仅首条请求承载 `turn.started` 和 `turn.failed`，后续只读控制仍可正常响应
