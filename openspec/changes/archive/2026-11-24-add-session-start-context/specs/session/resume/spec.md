## MODIFIED Requirements

### Requirement: In-chat resume reconstructs target state from target facts

系统 SHALL 从目标的已验证 native history、checkpoint 和当前 journal 重建独立控制实例、可见 transcript、工具和 Hook 历史、任务清单以及 artifact 访问范围。连接设置、启动推理配置、项目指令、Hook 声明和 Skill、Plugin 来源 SHALL 使用同一交互进程的冻结启动快照，沿用来源访问时的既有身份与内容校验。临时工具授权、Hook 信任或拒绝、Read 观察、运行中 effort 覆盖及瞬态缓存 continuation SHALL 按新实例初始化。接管前后的控制观察和 UI 结果 SHALL 绑定各自的实例身份。

在目标持久化事实、冻结设置、下次输入与该次 SessionStart 的确认结果及最终上下文字节相同的条件下，聊天内恢复和独立启动恢复 SHALL 生成等价的 canonical 请求、原生 item 顺序和缓存指纹。交接本身只使用已有 Session record/checkpoint 契约，无额外记录、版本或数据迁移。

#### Scenario: Continue the target's native history on either Provider
- **WHEN** 切换到已保存原生推理、工具调用配对与 checkpoint 的目标，随后提交首条新输入
- **THEN** 请求由目标的已验证原生历史与已提交的新输入及可选 SessionStart 上下文续写，canonical bytes、原生顺序和缓存指纹与相同条件下的独立恢复一致，新 records 追加到目标的下一 seq

#### Scenario: Switch back within the same process
- **WHEN** 用户为源会话临时授权工具和 Hook 并覆盖 effort，切换期间磁盘上的配置或声明发生变化，之后切回源
- **THEN** 新实例使用冻结的启动设置和声明，恢复此前 durable 历史与任务状态，工具权限、Hook 信任及 effort 从各自的初始状态开始

## ADDED Requirements

### Requirement: Resume reconciles SessionStart without rerunning old attempts

在线恢复 SHALL 在连续 exclusive lease 下校验完整的 SessionStart 前缀、关闭、实际原生正文与作用域。离线预览共用验证并保持只读。所有完整批次的在线语义、原生和 checkpoint 校验 MUST 早于 repair 和 append。已读到的尾批完整行须先通过固定形状及合法前缀校验，才可进入既有尾部修复。已保存 applied 关闭时，以其真实序号恢复已提交上下文，startup/resume 交付同时保留同批的真实输入。恢复不得重新注入或计费已提交的上下文，不读取旧脚本或信任，不自动发送网络请求。

合法未关闭前缀 SHALL 由原 lease 下的 writer 本地结算。只有已开始且无 finished 或已有不确定执行的前缀才报告结果未知，其他开放前缀依据已保存的事实报告中断。恢复关闭只保存能够由耐久证据推出的关联、范围和安全状态，不得补造进程退出、清理、授权或候选正文。恢复 SHALL 先按既有规则处理适用的压缩和 PostCompact，再关闭已有 durable plan 的旧 SessionStart，最后按原义补偿旧 Stop、工具或 turn 生命周期。恢复关闭自身须先 Sync 才能补偿同一 turn。未确认的写入 SHALL 阻止后续副作用，已提交的恢复关闭在下次恢复原样复用。成功 checkpoint 若尚无 compact 计划便中断，恢复仍只安装已保存窗口。

完成独占恢复和本地结算、成功接管交互根会话后，新实例 SHALL 将 `resume` 作为自己的待处理来源；仅由后续首个符合条件的实际对话回合消费。候选打开、预览、切换放弃与非交互路径 SHALL 不启动该检查。新尝试具有自身计划、真实 turn 和独立信任，旧尝试与旧压缩行为不产生新 `compact` 触发。

#### Scenario: Recover a started check without a result
- **WHEN** startup、resume 或 compact 的最后一个检查已有 durable started，进程随后在 finished Sync 前退出
- **THEN** 同一 lease 下先本地关闭旧检查为结果未知，再按现有规则关闭旧 turn，期间无 Hook 进程或 Provider 请求

#### Scenario: Restore an already applied compact context once
- **WHEN** compact 的 applied 关闭已 Sync，公开压缩成功或下一普通 sample 尚未发布
- **THEN** 恢复以已保存 checkpoint 和补充上下文的顺序构建有效窗口，无重复交付，canonical 请求、原生顺序和缓存指纹与相同已保存输入和设置下的持续运行等价

#### Scenario: Reject malformed context before repairing an unrelated tail
- **WHEN** 已提交的上下文与关闭证据不同，journal 末尾另有未完成写入
- **THEN** 全部原始 bytes 保留，不修复尾部，不运行脚本
