# extension/user-prompt-submit-hooks Specification

## Purpose

为即将交付模型的每条对话输入提供同步本地检查。按真实输入身份管理审批、运行、拦截和恢复，并保留通过项的原生正文和顺序。

## Requirements

### Requirement: Prompt checks match frozen declarations before model work

系统 SHALL 接受冻结用户与项目 Hook 配置中的 `UserPromptSubmit`，按既有来源和声明顺序选择，最多匹配 8 项。配置组只含 `hooks`。声明使用既有 `id`、`type: command`、`command`、可选 `timeout` 与 `feedback: none`。非同步执行、matcher、`feedback: model` 及其他事件控制字段按严格配置规则拒绝。

检查 SHALL 发生在 turn owner 领取真实输入之后、该次输入可能触发的自动压缩、首轮 `SessionStart`、原生输入提交和采样之前。覆盖首轮、工具结果后的追加、Stop 续轮中的真实用户纠正，以及明确批准的 Plan 实施文本。只读或非采样本地命令、问卷与审批响应、系统生成的 Stop 反馈、压缩摘要、恢复与继承历史不触发。无匹配时继续原行为，配置诊断或超过匹配上限时拒绝受影响提交。

#### Scenario: Reject before first activation and sampling

- **WHEN** 尚待 `SessionStart` 的会话领取首条输入，输入足以触发自动压缩，而已信任的检查拦截该输入
- **THEN** 本次没有摘要或采样请求，没有原生用户输入，`SessionStart` 仍等待下一条通过检查的输入

#### Scenario: Inspect the accepted Plan handoff as a submission

- **WHEN** 用户确认实施已审阅的计划
- **THEN** 审阅确认本身不触发检查。之后 Default 回合领取实际实施正文时，只检查一次。通过后原字节进入既有输入路径并服从普通工具审批

#### Scenario: Check invocation text and keep resolved attachments private

- **WHEN** 用户通过普通对话输入调用 Skill 或引用工作区文件
- **THEN** 检查收到冻结输入中的调用文字。展开内容只在该输入通过后，按既有附件契约进入 Provider 请求

### Requirement: Prompt hooks use a bounded private command contract

系统 SHALL 复用逐声明信任、默认拒绝、描述符校验、只读禁网执行与进程清理。stdin 由封闭强类型构造，字段为 `hook_event_name`、`session_id`、`thread_id`、`turn_id`、`input_id`、`cwd`、`prompt`。`prompt` 保留已领取输入的实际 UTF-8 字节，含用户自行键入的敏感文字。传输包含 JSON 包络，最大 512 KiB。该协议不额外提供文件或 Skill 展开值、历史、宿主凭据、环境快照或 transcript 路径。输入超限时用 `hook_input_limit` 可恢复地拒绝该条提交。

每条匹配声明在同一输入上最多启动一次，按顺序执行，在第一项拒绝或失败处结束该输入检查。沿用单项默认 5 秒、最大 30 秒及独立的 16 KiB stdout/stderr 上限。同一 turn 的 `UserPromptSubmit` 命令执行共用累计 30 秒额度，覆盖各采样安全点。信任等待可取消，独立于命令运行时间。未获信任、平台或沙箱不可用、超时、响应无效或其他确定的命令失败都拒绝该输入并报告相应固定原因。事件级准入先按已捕获快照选择。匹配后，无交互在本次 stdin 编码、平台前检与信任前返回 `hook_trust_required`。启动时原有全局可用性探测仍按现有宿主流程工作。一次性 `--print` 与 `--json` 遵循原失败退出和输出契约。

#### Scenario: Review before exposing the prompt

- **WHEN** 匹配的命令未获信任，或该声明刚通过 `/hooks` 重置信任
- **THEN** 先请求当前声明的审查。拒绝或取消时该声明零进程启动、零 stdin 披露。此前已获准并完成的声明保留其真实执行结果

#### Scenario: Enforce the encoded input budget

- **WHEN** 交互模式中某条合格对话输入的完整 JSON stdin 因转义或真实正文长度超过 512 KiB
- **THEN** 仅在匹配门禁的情况下用 `hook_input_limit` 返回该输入。正文保持完整，同批其他输入仍可独立检查

### Requirement: Prompt control output is local and explicit

取消、截断、超限、I/O 与进程故障优先按 runner 固定状态解释。双流处理成功且清理确认后的正常退出 `2` SHALL 直接拦截该输入，即使 stderr 为空。只有正常退出 `0` 才解释 stdout，去除空白后为空或空 JSON 对象时 SHALL 通过。严格 JSON 对象 `{"decision":"block"}` SHALL 拦截，可带字符串 `reason`。未知或重复字段、`null`、尾随值、非法编码、非空自由文本以及 `continue`、`stopReason`、`decision: approve`、`hookSpecificOutput` 等其他控制形状均属于 `hook_output_invalid`。其他正常非零退出属于 `hook_exit_failed`。

原始 stdin、stdout、stderr、自由文本 reason 与脚本命令 MUST NOT 通过新事实进入 Session、日志、错误、TUI 历史或 Provider。诊断仅暴露固定英文原因、安全的声明来源和 ID（若已知）及有界执行统计。脚本输出无模型披露能力。通过项使用既有原生输入编译、投影和缓存逻辑。

#### Scenario: Keep both blocking forms local

- **WHEN** 脚本以退出码 `2` 阻断，或返回 `decision: block`，并在 stderr 或 reason 中包含输入复述
- **THEN** 只显示固定英文阻断摘要，复述未出现在外部事件、日志、Session 或 Provider 请求中

### Requirement: Prompt checks settle durably without replaying commands

系统 SHALL 保存匹配计划、命令启动和终态、逐输入决策及计划收口的强类型事实。先 Sync 对应启动事实，再启动已信任命令。决策与关闭事实先于用户输入接纳，耐久记录只含关联身份、声明引用、结果和必要的统计，计划摘要不派生自 prompt 或脚本输出。Session 回放 SHALL 严格检查同一 turn、采样边界、有序 claim、声明和运行前缀、重复事实与已通过输入的有序原生关联。普通接受批次或已有 SessionStart/Stop 复合批次的原子提交规则继续生效。

恢复 SHALL 在现有 writer lease 下验证并只用本地事实关闭中断检查。已启动而无确定结果的运行记录为中断未知，未启动项不产生伪造运行。任何未完成或已通过但未提交的输入都不得由恢复发送，既有原生提交结果以完整 Sync 批次为准。写入不确定时停止新副作用并等待 cleanup owner，恢复零命令、零网络。

若运行或清理结果未知，当前 turn SHALL 停止后续命令、原生用户输入写入和 Provider 请求，沿用进程 owner 及最终清理路径。检查结果未知但原生输入确定尚未写入时，应通过可取回状态报告。只有原生写入本身无法确认时才使用不可重试的输入 `uncertain` 状态。Stop 反馈仅在检查已耐久收口、清理确认且 writer 健康时由当前 owner 本地交付。检查写入结果不确定时由恢复在同一 lease 下完成缺失记录并结算反馈。已经耐久的拒绝保持原原因。

#### Scenario: Recover after a durable command start

- **WHEN** 进程在已 Sync 检查启动与检查结果之间中断
- **THEN** 恢复本地关闭未完成检查，保留未确认结果，且未发送任何仅存在于内存的输入

#### Scenario: Recover the exact accepted native history

- **WHEN** 输入通过检查并已写入完整原生批次，进程在发布 UI 状态前中断
- **THEN** 恢复只读取一次通过输入，保持原生项顺序、续写请求的 canonical bytes 与缓存 fingerprint 一致
