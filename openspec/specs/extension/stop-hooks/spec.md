# extension/stop-hooks Specification

## Purpose

定义 root 用户回合结束前的同步命令检查，使最终答复在真实保存后接受已信任的有界验证，检查阻断时可通过显式授权反馈继续原任务，同时保证取消、错误、预算、原生历史与恢复语义确定且不泄漏未授权脚本输出。

## Requirements

### Requirement: Stop checks guard successful root turn completion

系统 SHALL 仅在 root 用户回合的成功、无 ready calls 的 sample 完整归并并持久化后触发 Stop；有匹配项时所有检查通过之前 MUST NOT 发布 turn_completed。没有合法 completed、仍有未闭合工具调用、Provider/API 错误、用户取消、摘要操作或恢复阶段 MUST NOT 启动 Stop。合法空来源/无 Stop 沿既有完成路径；任一坏来源或 Stop 匹配超限 SHALL 在完成边界失败关闭，不从部分来源推断通过。所有候选答复和已有工具结果保持真实提交状态。

#### Scenario: Complete after passing checks
- **WHEN** 无 calls sample 已保存且全部 Stop 检查通过
- **THEN** 只在通过决定与成功 terminal 同批 Sync 后发布一次完成，答复不重复提交

#### Scenario: Skip an unsuccessful provider sample
- **WHEN** 模型断流、API 错误、取消或参数未完整
- **THEN** Stop 执行次数为零，沿原失败路径结束，不让 Hook 触发重试

#### Scenario: Fail on a damaged source
- **WHEN** 冻结 Hook 来源无效，无法可靠判断其中是否有 Stop
- **THEN** 已保存正文及只读诊断可读，最终以 hook_config_invalid 失败，零 Stop 命令且不宣称检查通过

### Requirement: Stop input contains only the exact committed assistant sample

Stop stdin SHALL 为实际编码至多 512 KiB 的严格 UTF-8 JSON，包含 Stop 事件、真实 session/thread/turn、sample_index、安全 cwd、stop_hook_active 和 last_assistant_message。正文 MUST 来自该已提交 sample 的单向语义投影，按原顺序保留可见文字及空白；合法空正文为空字符串，不回退之前非空答复。MUST NOT 包含 tool 身份、完整历史、transcript 路径、连接配置、thinking、reasoning summary、signature、redacted/encrypted、phase、Provider ID 或 raw 扩展。超限不得截断后执行。

#### Scenario: Check an empty final answer
- **WHEN** 前一个工具 sample 有 commentary，而本次无 calls sample 没有可见正文
- **THEN** last_assistant_message 为空，不错误检查旧 commentary

#### Scenario: Reject encoded input overflow
- **WHEN** 交互模式已通过来源/平台前检，而正文 JSON 转义后 stdin 超过 512 KiB
- **THEN** 以 hook_input_limit 失败，零进程，不截断或遗漏转义字符

### Requirement: Stop output supports a strict explicit block feedback subset

每流 SHALL 至多捕获 16 KiB。清理确认且 I/O 正常时，exit 0 的空白或严格空对象表示本项通过；唯一非空输出为严格 decision=block 与非空、最多 4 KiB UTF-8 reason。重复/未知字段、null、尾随值、非法 Unicode、其他 decision、continue/stopReason、additionalContext、async 或改写输出 MUST 拒绝。exit 2 SHALL 仅以 hook_denied 结束，不解析或披露双流；其他非零为检查错误。cleanup 不确定、取消、timeout、输出超限/I/O、退出及解析按既有优先级决定结果，禁止截断后解析。

仅声明 feedback=model、已信任且通过全部续轮/文本/编码预算的正常结构化 block reason SHALL 冻结为模型反馈。决定来源必须绑定实际最后执行的阻断项，且 finished 为正常 exit 0、清理确认、未取消/超时；不能引用另一 model 声明或借用 exit 2 结果。feedback=none 的 block 丢弃 reason，仅记录安全 hook_denied 并结束回合。其他双流、未授权正文和诊断 MUST NOT 自动保存、回显或成为 artifact。被明确批准的反馈是模型输入，审查必须说明其发送及持久化。

#### Scenario: Continue from an authorized JSON block
- **WHEN** 已信任的 model 声明 exit 0 返回合法 block/reason 且额度有效
- **THEN** 先冻结最终反馈字节及交付义务，候选准备/可选旧窗口摘要后原子交付反馈与合法用户追加；交付 Sync/finalize 后才允许下一 sample

#### Scenario: Block without disclosing text
- **WHEN** feedback=none 返回合法 block，或 exit 2 的 stderr 带合成 secret
- **THEN** 回合失败，journal/请求/错误/日志/快照/artifact 中不出现该原文

#### Scenario: Reject a seemingly complete truncated output
- **WHEN** 前 16 KiB 恰好为合法 JSON 但任一流实际超过上限
- **THEN** 清理后报告 hook_output_limit，不按前缀放行或续轮

### Requirement: Stop continuations have independent bounded budgets

每 turn SHALL 至多两次提交 continued 反馈决定，stop_hook_active 仅在此前有 continued 时为 true。第三次候选检查仍可通过，但原本可授权续轮的第三次正常 model 阻断 MUST 以 stop_hook_limit_exceeded 结束；none/exit2 仍以 hook_denied 终止。每候选最多 8 个串行检查，遇阻断/错误停止后项；单项默认 5 秒、配置 1..30 秒，同 turn 所有 Stop 执行累计 30 秒。信任等待不计执行时间但可取消；预算耗尽不得当作通过或伪造已运行项。

单次规范模型反馈 SHALL 至多 8 KiB、同 turn 至多 16 KiB UTF-8 文本 bytes，并继续满足原生/请求编码和上下文窗口约束。16 sample/64 tool call 原上限、工具结果预算、自动摘要熔断均不重置。Hook 自身不伪造真实用户输入、InputID 或新 TurnID；但 continued 决定闭合后必须在下一普通请求前，经唯一 owner 领取队首连续 auto 前缀，保留真实 InputID/原文，strict-next 不可跨越。通过后的队列沿原下一 turn 语义；新安全点仅服务 Stop 自动续轮。

#### Scenario: Block twice then finish
- **WHEN** 两次合法反馈后第三次候选通过
- **THEN** 三次答复同 TurnID，active 为 false/true/true，普通 usage 按实际 samples 聚合且只有一个成功终态

#### Scenario: Stop a repeated block
- **WHEN** 已有两次 continued 而 model 检查再次正常 exit 0 block
- **THEN** 不提交第三段反馈、不发第四次请求，以稳定上限错误结束

#### Scenario: Keep the original sample limit
- **WHEN** 较长工具回合到 Stop 时已消耗普通 sample 额度
- **THEN** 第 16 个 sample 的检查可通过；若正常 model 阻断且无下一 sample 余量，在 continued 决定前以 blocked/tool_loop_limit_exceeded 结束，不保存反馈意图、不领取或摘要；64 calls 上限仍在新 ready 提交前校验，纯文本续轮不因此禁止

### Requirement: Stop uses explicit trust and one cancellation cleanup owner

Stop SHALL 需要与事件、来源、声明、披露策略及真实回合目标绑定的独立进程内信任，默认拒绝。sample/计划成功提交及预检通过后审查全部声明，全部信任前零 Stop 命令。命令 started 的成功 Sync 为执行接收点；接收前取消零进程，接收后恰好取得一次真实结果并等待 owned 清理。运行采用现有只读/禁网边界，不允许自动批准修复工具、提升权限或生成 Read 证据。

#### Scenario: Cancel before run admission
- **WHEN** 最终 sample 已保存但在首个 Hook started 接收前取消
- **THEN** 正文保持已保存，零进程并以唯一取消终态结束

#### Scenario: Keep ownership during shutdown
- **WHEN** 已接收 Stop 的 Wait 或输出清理阻塞而 shutdown 到期
- **THEN** 同一 owner 保留进程与 lease，禁止下一 turn，最终 cleanup 后才结束，不另建遗留等待 goroutine

### Requirement: Stop has no implicit unattended or recovery execution

前检 SHALL 按已保存且健康的 sample/plan、当前取消、不可用来源/匹配、headless、交互平台/沙箱、stdin 编码、剩余执行额度、信任、运行的顺序处理，并在实际接收点复核取消。三个 headless 入口在有效匹配 Stop 时以 hook_trust_required 失败，零新增 Stop 专用正文投影/平台前检/runner，不等待输入或使用配置绕过信任。既有启动 availability 探测及普通采样上下文投影不计入 Stop 门禁调用；启动平台错误快照不能覆盖 headless 拒绝。交互平台无法兑现沙箱时在信任前失败。恢复只读取已验证 durable facts，不运行旧检查或请求模型；已接收但未完成的本地反馈交付必须从冻结最终字节纯内存编码并补齐一次，不能重新渲染或重复交付；历史信任不恢复。外部 JSONL v1 和文本输出形状保持，脚本双流不成为宿主 stdout。

#### Scenario: Fail a headless completion check
- **WHEN** --print、--json 或 stream-json 回合匹配 Stop
- **THEN** 安全失败且零 Stop 执行/信任等待，不新增外部 Hook 或伪用户消息

#### Scenario: Resume an interrupted completion
- **WHEN** 合法 journal 留有未闭合 Stop
- **THEN** 本地补偿并关闭旧回合，零配置/脚本/工具/Provider 访问，下一真实用户输入才可开始新工作

#### Scenario: Resolve headless and input limits deterministically
- **WHEN** 有效 Stop 匹配的 headless 回合同时位于不支持平台且最终正文超过 stdin 上限
- **THEN** 固定 hook_trust_required，零新增 Stop 平台探测/专用正文投影/命令；若来源先已无效则使用来源错误，当前取消则遵循取消优先

#### Scenario: Respect a correction before automatic continuation
- **WHEN** Stop 检查时队列为 auto 纠正 A、strict-next B、auto C，随后合法阻断
- **THEN** 下次普通请求前只领取 A，反馈数据在前、A 的真实用户原文在后；B/C 不越过屏障
