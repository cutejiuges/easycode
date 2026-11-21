## ADDED Requirements

### Requirement: Resume closes an interrupted PostCompact without retrying observation

当成功 checkpoint 已持久化但 planned PostCompact 仍有未关闭的尾部时，恢复 SHALL 在连续 exclusive lease 下完成 Session 与双 Provider 原生校验，按原成功序号安装同一窗口并用当前 writer 追加一次本地 `post_compact_hook_closed`。关闭遵循 Session 前缀规则。孤立 started 或已有清理不确定的 finished 记为结果未知，真实取消保留取消，全部确定结束按真实结果结算，其余前缀记为中断。恢复不得补造 started、finished、授权、清理或退出信息，不重读旧命令或运行进程，不发 Provider 请求，不写重复 checkpoint 或 `compaction_failed`。尾部关闭先于既有 Stop 或 turn 的本地收口。关闭追加或 Sync 失败须阻止恢复接纳；已成功提交的关闭在再次恢复时复用。离线预览共用完整验证并保持只读。

#### Scenario: Recover either side of dispatch

- **WHEN** 成功 checkpoint 已 Sync 后进程退出，journal 尚无 PostCompact started，或最后一个 started 还没有 finished
- **THEN** 恢复新窗口，无 started 时记录中断，孤立 started 时记录结果未知并保留真实准入身份；不询问旧信任、不运行脚本或摘要，下一实际请求的原生顺序、canonical bytes 和缓存 fingerprint 等于同一已提交窗口的持续运行

#### Scenario: Recover the observer before resolving a pending Stop

- **WHEN** 自动压缩后置检查中断，而 Stop 的本地 native 反馈仍待交付
- **THEN** 先关闭后置尾部，再依原规则本地交付和结束旧 turn；再恢复时三者均不重做
