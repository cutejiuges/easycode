## MODIFIED Requirements

### Requirement: Hooks browser is local bounded and draft preserving

TUI SHALL 提供 /hooks，按事件、适用工具 matcher、无 matcher 的回合完成范围或 压缩事件的触发 matcher 及来源展示冻结声明与 available/untrusted/trusted/denied/invalid/unsupported 状态，并可进入完整命令、timeout 和安全限制详情。浏览 MUST 无 Provider、磁盘、Session 写入或进程副作用；View 只读取内存投影。窄屏与长命令 SHALL 可滚动完整审查，不将截断行冒充完整声明；Esc 返回原草稿。

空闲时详情 SHALL 允许对精确匹配快照的单项 reset 内存信任或拒绝；reset 不运行命令、不建立授权、不重试旧调用。活动 turn 或独立手动压缩中 reset MUST 拒绝。无效或不支持项不得提供可执行信任动作。

#### Scenario: Browse without side effects
- **WHEN** 用户打开 /hooks、浏览详情并返回
- **THEN** 进程/Provider/Session 写入与来源读取次数为零，原草稿保持不变

#### Scenario: Inspect a long declaration in a narrow terminal
- **WHEN** 终端宽度不足以单行显示命令
- **THEN** 可滚动查看完整命令和限制，没有隐藏的授权字段或按键误提交

Stop SHALL 明确显示回合完成检查、feedback 策略和现有信任状态，不显示伪造工具能力或 matcher。浏览、窄屏完整命令、空闲 reset 继续无 I/O/授权/重试副作用。

#### Scenario: Browse completion declarations
- **WHEN** 用户打开带 Stop 的 /hooks
- **THEN** 显示回合完成范围、完整命令与披露策略，无 matcher、进程或 Provider 操作，Esc 保留草稿

#### Scenario: Browse compaction declarations

- **WHEN** 用户打开带 PreCompact 的 /hooks 并进入详情
- **THEN** 显示 `manual`、`auto` 或 `*`、来源、披露策略及完整命令。浏览无脚本、Provider 或 Session 写入，Esc 返回时保留草稿

### Requirement: Hooks review exposes event timing and model disclosure

/hooks SHALL 同时展示 PreToolUse、PostToolUse、PostToolUseFailure、Stop、PreCompact、PostCompact 六事件、来源、工具 matcher、回合完成范围或压缩触发范围、feedback=none/model 与原有信任状态，完整详情支持窄屏滚动且不执行 I/O。两个后置分支的信任 overlay MUST 出现在原工具候选/普通批准/执行之前，分别说明成功后检查或执行尝试确定失败后检查、只读/禁网及 model 模式内容会发送给 Provider 并保存到 Session；失败说明须明确准入后的启动失败可诊断、正常非零退出仍走成功事件，不宣传测试失败自动修复。默认拒绝、Esc 拒绝、Ctrl+C 取消。reset 不隐式开启披露、不重试旧工具，历史信任不建立当前授权。paste、过期响应和 modal 输入不得泄漏到 composer 或其他批准。

失败检查的审查 MUST 发生于原工具候选与普通批准前，不因可能不执行而隐式信任。实际运行期间主线与详情 SHALL 保留原 Error/Conflict，检查成功不能显示为工具修复成功。只有当前选择分支可发布 checking/finished；不同事件、plan、旧 turn 或过期 detail_sequence 不得覆盖当前状态。Ctrl+O 只展示安全统计及已授权 durable 反馈；未披露 stdout/stderr 不展示。原 error 保存后当前取消不得把已选失败事件改成 none 或把原工具显示为未执行；原 result 自身 cancelled 而选择 none 时也不得据此显示工具从未运行。取消后等待唯一 owner cleanup，保留草稿/锚点，完成前不能 idle。

后置持久化提交通知 SHALL 仅包含调用、计划摘要、持久化详情序列及阶段，不保存固定成功事件或可空事件字段。计划阶段原结果未知时，宿主只显示“后置计划已保存”，不能预选成功/失败或显示检查已开始。实际运行事件 SHALL 从同一调用、同一计划和最新已验证事实快照读取；通知不能替代结果或事实源。字段缺失/未知、错调用/计划与过期序列必须拒绝或按既有过期消息规则忽略，构造/getter/render 不执行 I/O。

完整命令审查 SHALL 支持 command 契约允许的最大原始字节，包括 JSON 转义后大于原始长度的命令。携带声明的信任请求 SHALL 采用与合法声明最坏编码相容的有界预算；MUST NOT 因合法转义扩张而截断审查、误报无效控制事件或取消回合。原始命令长度、严格字段校验、默认拒绝和精确信任归属 MUST 保持。

#### Scenario: Review a model-feedback hook in a narrow terminal
- **WHEN** Post 命令较长且 feedback=model
- **THEN** 用户可滚动审查完整命令、事件及披露用途，直接 Enter 仍默认拒绝且零修改

#### Scenario: Keep the successful result visible during post failure
- **WHEN** Write 已成功但后置检查失败或被取消
- **THEN** 显示成功 Write 与独立检查失败/取消，原草稿与阅读锚点保持，cleanup 完成前不允许下一 turn

#### Scenario: Accept post ordinal zero after pre ordinal zero
- **WHEN** 同 invocation 的 Pre 第 0 项已完成，工具成功后 Post 第 0 项开始并完成
- **THEN** 历史关联不冲突，Post checking/终态均可见，原工具成功保持

#### Scenario: Ignore an old phase update
- **WHEN** 工具之后收到 Pre 重开、错误 plan 或旧 detail_sequence 的消息
- **THEN** 不改变 Post 状态、信任或草稿，不让旧消息覆盖新快照

#### Scenario: Review failure timing before an action
- **WHEN** 同调用存在成功和失败声明，用户在窄屏打开信任审查
- **THEN** 可完整审查事件、命令和披露用途；默认 Enter/Esc 仍拒绝，不进入普通批准或修改文件

#### Scenario: Preserve the failed tool during a successful diagnostic
- **WHEN** 工具 error/conflict 已保存，而失败诊断脚本通过并提供已授权反馈
- **THEN** 主线保留原工具失败并独立展示检查完成，Ctrl+O 可读反馈，不显示工具修复或自动重试

#### Scenario: Ignore a stale alternative branch update
- **WHEN** 当前选择失败分支，而成功分支消息或旧序列/旧 turn 异步响应到达
- **THEN** 当前检查、信任、草稿和阅读锚点不改变；paste 不授权或提交其他 modal

#### Scenario: Present a plan before any result exists
- **WHEN** 双分支计划已保存而原工具尚无 result，宿主收到合法调用/计划/序列/阶段通知
- **THEN** 仅显示中性计划状态，不预填 PostToolUse 或 PostToolUseFailure，不显示原工具或脚本已执行

#### Scenario: Reject the superseded fixed-event notification
- **WHEN** 后置提交通知仍含 event 字段，或通知身份/序列与最新事实快照不一致
- **THEN** 当前 strict decoder 或投影校验拒绝旧 shape/错配，不用固定事件刷新详情或改变信任

#### Scenario: Show cancellation without erasing the durable failure
- **WHEN** 原 error 已保存且匹配失败分支尚未 started，用户取消
- **THEN** 保留原工具错误、failure 选择及独立检查取消，零脚本；草稿和阅读锚点保持，不显示工具未执行或已修复

#### Scenario: Review a maximum command after JSON expansion
- **WHEN** 六事件之一的合法最大命令包含 JSON 需要转义的字符，审查消息编码后超过 16 KiB
- **THEN** 构造、严格解码、AgentLoop 与本地审查保留完整命令和身份；未知字段或原始命令超限仍被拒绝，不默认授权或取消合法回合

Stop 历史详情及提交通知 SHALL 精确绑定回合/sample target、plan digest、durable detail sequence 及 plan/decision/delivery 阶段；不借用 Post 的原工具 success/failure route。旧 target/plan、过期序列或跨事件更新不得覆盖当前状态。Ctrl+O 复用既有有界页缓存、异步查询与代次校验，render 不读取 journal。恢复后的候选/反馈/续答、检查结果及真实 cursor 与 live committed 一致，Hook 正文不进入用户 recall。

#### Scenario: Read completion details after restart
- **WHEN** 用户恢复含候选/Stop 反馈/续答的会话并打开 Ctrl+O
- **THEN** 展示同一顺序/来源/cursor 及安全统计，只有授权正文可读，无脚本重读或副作用

#### Scenario: Ignore a previous sample check response
- **WHEN** 当前正在检查较新 sample 而旧 sample/plan/detail sequence 消息迟到
- **THEN** 当前状态、信任、草稿和阅读锚点不变，不将旧通过应用到新候选

continued 决定的正文虽然已 durable，也只能在 Stop 详情显示待交付来源；native cursor 必须来自实际一/三记录交付，不能在决定阶段伪造。真实用户追加使用原 InputID/committed 通知与 recall，Hook 与真实用户条目分别显示。取消返还不能覆盖草稿；复合 Sync 成功后的真实输入不得错误放回托盘或自动重发。

#### Scenario: Distinguish pending and delivered feedback
- **WHEN** 决定已保存、摘要正在运行，而 native 反馈还不存在
- **THEN** 可读已授权冻结详情，但不生成假 native cursor 或声称模型已接收；交付后刷新同一目标的真实位置

#### Scenario: Keep a committed correction out of the returned tray
- **WHEN** 复合交付 Sync 成功后用户取消
- **THEN** 原反馈和真实输入保持已保存，取消只结束后续工作，不返还或重复提交该输入

## ADDED Requirements

### Requirement: PostCompact review and status preserve the successful window

TUI SHALL 在真实的已安装 checkpoint 阶段显示已压缩及后置审查/运行信息。`/hooks` 与完整审查 MUST 显示事件、触发 matcher、来源、timeout、完整命令和向本地脚本披露当前压缩摘要。默认选择为拒绝，Enter 确认当前选择，Esc 拒绝，Ctrl+C 定向取消。长命令和窄屏可完整安全审查或明确禁止授权；草稿、历史锚点和清理所有权保持。状态 MUST 绑定真实 scope、checkpoint 序号、实际 PostCompact plan、事件与相位，忽略旧 PreCompact 或其他操作的活动和信任响应。交互路径的未计划执行跳过只展示匹配 checkpoint 和稳定原因，禁止伪造计划、审查或运行。合法无匹配沿用原显示。

当前操作的终态 SHALL 同时显示成功压缩和独立的后置结果。取消期间仍接纳真实范围匹配的安装与关闭通知、拒绝迟到审查或 checking，已安装后继续显示压缩已保存，清理未完成前维持活动。检查关闭未确认时只显示安全的不确定状态。检查展示限当前操作、有界分类及计数，历史摘要沿原 Ctrl+O 权限和阅读路径。

#### Scenario: Cancel during post-install review

- **WHEN** checkpoint 已安装且当前 PostCompact 审查正等待，用户 Ctrl+C 并随后收到旧审查响应
- **THEN** 取消等待期间保持已压缩，接纳匹配的关闭并将结果并入唯一压缩成功终态；解除审查后草稿保留，旧响应不授权新命令
