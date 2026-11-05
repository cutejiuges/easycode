## MODIFIED Requirements

### Requirement: Tool catalog snapshots are deterministic and executable

每次 Provider sample SHALL 使用一个不可变 Tool Catalog snapshot。snapshot 中每个 facade MUST 关联唯一当前、已注册且可执行的 capability、强类型 input/result、Provider facade及其wire kind、validator、policy、适用的 sandbox planner、executor 和结果 renderer；系统 MUST NOT 暴露缺少任一真实消费者的 schema或freeform定义，也 MUST NOT 通过 input revision、result codec revision 或版本后缀类型选择实现。

当前 snapshot SHALL 恰好包含 `fs.read`、`fs.glob`、`fs.grep`、`fs.patch`、`fs.write`、`process.exec`、`artifact.read`、`skill.read` 与 `user.question`；Anthropic SHALL 将 patch/command 分别暴露为 structured `Edit`/`Bash`，OpenAI SHALL 将同一 patch/command 能力分别暴露为 custom freeform `apply_patch` 与 structured `exec_command`。facade 按稳定 capability ID、wire kind 与 facade name 确定性排序。snapshot SHALL 具有非空内容派生 source revision、canonical definition bytes 和 fingerprint；调用方修改 getter 返回值不得改变 snapshot。时间戳、cwd、Session/thread/turn identity、随机 invocation/approval ID、权限临时状态、base evidence、diff、命令环境实际值和文件系统枚举顺序 MUST NOT 进入 definition bytes 或 fingerprint。

两个 Provider SHALL 都以 structured Write 暴露 fs.write。Write 的 request、strict decoder、native restore、result codec、候选、policy、executor 和审批展示缺一不可；创建/覆盖证据和 Read observation MUST NOT 进入稳定 definition bytes。

#### Scenario: Build the same catalog twice
- **WHEN** 两次使用相同 Skill、Read、ReadArtifact、Glob、Grep、Write、Edit、apply_patch、Bash和exec_command描述与定义创建catalog snapshot
- **THEN** facade顺序、wire kind、canonical bytes、source revision和fingerprint完全相同

#### Scenario: Reject a facade without an executor
- **WHEN** catalog尝试注册只有schema或freeform描述而没有匹配executor、validator、policy、sandbox planner或result renderer的facade
- **THEN** snapshot构造失败且该facade不会出现在任一Provider请求中

#### Scenario: Expose the complete current catalog
- **WHEN** Runtime为本变更编译一次Provider请求
- **THEN** catalog只声明Read、ReadArtifact、Glob、Grep、Write、对应Provider的Edit或apply_patch以及Bash或exec_command，额外声明 structured Skill 与对应 Provider 的 structured 提问 facade，不声明MCP、Agent、后台进程或write_stdin

#### Scenario: Expose the complete read-only catalog
- **WHEN** 将当前catalog中的只读子集投影给审计或测试
- **THEN** 该子集仍恰好包含Read、ReadArtifact、Glob、Grep、Skill，Patch、Write与Exec作为独立exclusive能力存在且不冒充只读工具

#### Scenario: Expose only current contracts
- **WHEN** 将当前catalog与早期开发期catalog比较
- **THEN** 当前唯一catalog直接包含九项能力且不保留旧catalog、旧schema或兼容分支

#### Scenario: Expose Provider-specific mutation wire
- **WHEN** 分别读取Anthropic与OpenAI catalog view
- **THEN** 两者分别路由到相同`fs.patch`与`process.exec`能力，但保持各自Edit/apply_patch与Bash/exec_command facade及wire

#### Scenario: Expose only Read
- **WHEN** 将当前catalog与最早只暴露Read的开发期catalog比较
- **THEN** 当前唯一catalog直接包含Read、Glob、Grep、Patch、Write、Exec、ReadArtifact、Skill与Question且不保留旧catalog分支

#### Scenario: Expose Provider-specific patch wire
- **WHEN** 分别读取Anthropic与OpenAI catalog view
- **THEN** 两者都路由到`fs.patch`，但Anthropic使用structured JSON而OpenAI使用custom freeform定义

#### Scenario: Expose structured Write beside native Patch
- **WHEN** 获取两家完整当前 catalog
- **THEN** 两者均有 structured Write，OpenAI 同时保留一个 custom apply_patch
- **THEN** Write 定义顺序与 fingerprint 不受目标存在性和 Read 证据变化影响

两家 Provider SHALL 使用 structured ReadArtifact 暴露 artifact.read；保存路径、ID、计数、动态配额使用量 MUST NOT 进入 catalog definition 或 fingerprint。

#### Scenario: Expose executable artifact reading
- **WHEN** 构造两家 Provider 当前 catalog
- **THEN** 恰好九项能力，ReadArtifact 具有完整 request、reducer、native restore、policy、executor 和 result codec

两家 Provider SHALL 以 structured Skill 暴露 skill.read，具备完整读取、validator、policy、来源范围约束、renderer 与恢复实现。目录为空仍保留真实 executor，未知名称返回安全错误，不暴露未来来源能力。

#### Scenario: Expose executable Skill loading
- **WHEN** 两家 Provider 构造当前目录
- **THEN** 均恰好九项能力，Skill 的主文档与资源分支都有 typed input/result，定义不包含实际目录正文或绝对路径

user.question SHALL 具有完整 typed input/result、strict decoder、policy、实际交互执行消费者、renderer 和 native restore。Anthropic facade 为 AskUserQuestion，OpenAI facade 为 request_user_input，均为 structured wire。该能力不操作文件、进程或网络，不生成虚构 sandbox 候选或副作用批准；无交互宿主由真实执行路径返回配对不可用结果。目录定义中不得出现动态题目、回答、请求身份、结果余额或 UI 状态。

#### Scenario: Expose executable user questions
- **WHEN** 两家 Provider 构造完整当前 catalog
- **THEN** 均恰好九项真实能力，提问 facade 按既有规则排序，不能只提供 schema 而没有交互、拒绝及恢复消费者

#### Scenario: Keep the read-only subset precise
- **WHEN** 检查可并行读取子集
- **THEN** 仍为 Read、Glob、Grep、ReadArtifact、Skill，提问不因没有文件写入而被放入并行读批次


### Requirement: Tool turns use a bounded ordered multi-sample loop

一个用户turn SHALL支持最多16个Provider samples和累计最多64个tool calls。completed sample不含tool calls时 SHALL结束turn；含calls时，Scheduler SHALL按call index把连续`parallel_read`调用组成最大8并发的批次，并把每个`exclusive` Patch、Write、Exec 或 Question 作为前后屏障。Runtime SHALL在到达每个调用或批次时完成其policy；Patch候选准备和所需approval只能在其前序批次结果durable后发生，matching decision收口后才可越过该屏障。结果收集到固定call slot并按call index durable提交全部results和匹配Provider-native outputs，outputs durable后才可开始下一sample。

当前Read、Glob、Grep、ReadArtifact、Skill属于`parallel_read`，Patch、Write、Exec与Question属于`exclusive`。单个调用的普通执行错误、拒绝、冲突或无匹配结果不得取消independent sibling；`abort`、turn取消或poisoned状态按取消契约收口。goroutine完成顺序、approval响应时间和实际执行时间不得影响Session seq、Provider output顺序、preview bytes或后续请求。超过sample/call上限 SHALL以稳定英文`tool_loop_limit_exceeded`失败，并且已经durable的sample、approval、ledger和outputs不得被删除或改写。

本条的 exclusive 屏障和串行审批语义 SHALL 同样适用于 Write；不能将非 Patch 调用默认视作 parallel_read。

#### Scenario: Complete a two-sample search turn
- **WHEN** 首个sample请求Glob与Grep，结果提交后第二个sample返回最终文本且不再调用工具
- **THEN** turn只产生一个开始和一个成功终态，并在两次sample之间保持合法native call/output顺序

#### Scenario: Complete a two-sample Read turn
- **WHEN** 首个sample请求Read，结果提交后第二个sample返回最终文本且不再调用工具
- **THEN** turn只产生一个开始和一个成功终态，并保持合法native call/output顺序

#### Scenario: Complete an approved patch turn
- **WHEN** 首个sample请求Patch，用户批准且结果提交后第二个sample返回最终文本
- **THEN** turn只产生一个开始和一个成功终态，并保持合法native patch call/output顺序

#### Scenario: Execute multiple calls with an exclusive barrier
- **WHEN** 同一sample依次产生Read、Grep、Patch与Read calls
- **THEN** 前两个只读调用可并行，Patch独占，最后一个Read等待Patch完成
- **THEN** results与outputs始终按原index提交

#### Scenario: Execute multiple calls sequentially
- **WHEN** 同一sample产生两个Read calls
- **THEN** 两个调用在同一parallel-read批次按call index完成durable admission，执行可以重叠但results与outputs始终按原index提交

#### Scenario: Preserve call order across parallel completion
- **WHEN** 同一只读批次的call 0执行较慢而call 1先完成
- **THEN** result facts与Provider outputs仍按call 0、call 1顺序提交
- **THEN** 并行完成时间不进入durable metadata或模型preview

#### Scenario: Bound worker concurrency
- **WHEN** 同一连续批次包含超过八个合法`parallel_read` calls
- **THEN** 任意时刻至多八个executor正在运行，全部结果仍占据其原始call slot

#### Scenario: Stop an infinite tool loop
- **WHEN** 模型持续请求工具并达到sample或call上限
- **THEN** Runtime不发起超限sample或invocation，并以`tool_loop_limit_exceeded`durable收口turn

#### Scenario: Order Write between read and Patch calls
- **WHEN** 同一 sample 依次产生 Read、Write、Patch、Grep
- **THEN** Write 与 Patch 各自独占且按 index 串行处理，所有 results/outputs 保持原顺序
- **THEN** Write 若缺少先前已提交输出的完整 Read 证据，则在自己 slot 返回错误，不冒用本 sample 的 Read

#### Scenario: Order mixed artifact reads
- **WHEN** ReadArtifact、Exec、ReadArtifact 按此顺序 ready
- **THEN** 后一个读取等待 exclusive Exec 结果，所有 result/native output 按原 call index 提交

自动追加安全点 SHALL 仅在整组 results 与匹配 native outputs 均 durable/finalize 后开放；上限、取消、poison 或无 calls 最终决策后不得领取。输入提交不占用 sample、不重置 16/64 上限，不改变工具执行权限与 pairing。

#### Scenario: Consume steering after complete outputs
- **WHEN** 并行调用乱序完成且存在排队自动输入
- **THEN** 全部结果仍按原 call index 提交，outputs 成功后才可提交追加输入并开始下一 sample

#### Scenario: Leave late input for the next turn
- **WHEN** 无 calls sample 已选择提交最终边界后输入到达
- **THEN** 当前 turn 正常完成，输入保留 FIFO 等待新 turn

Runtime SHALL 在同一 turn owner 持有 tool/result-budget 的纯内存账本，sample 512 KiB、turn 2 MiB，按调用 index 最终分配而非 worker 完成时间。每个当前及后续允许调用保留 1 KiB 安全结果空间，已冻结实际 preview bytes 扣账。steering 不重置额度；只读 worker 返回有界执行材料，由 owner 按序 finalization、result Sync 和 native outputs 提交；exclusive 屏障前不得延迟前序 result durable。

#### Scenario: Do not replenish budget with steering
- **WHEN** 同轮用户追加输入在 outputs 安全点提交
- **THEN** 后续 sample 继续使用同一 turn 剩余额度，16/64 上限和权限不改变

#### Scenario: Stop admission when turn body budget is exhausted
- **WHEN** turn 除安全结果预留外已无正文额度
- **THEN** 不再创建新审批或准入 executor；accepted 调用仍执行并清理一次，未 accepted ready 获得 result_budget_exceeded 配对；当前 outputs 完成后以 tool_result_budget_exceeded 失败收口且不领取 steer

提问 exclusive 屏障 SHALL 仅约束调度；是否需要副作用审批必须独立判定。前序结果 durable 后才可准入提问，完整回答或跳过结果 durable 后才能准入后序调用；只有整组 matching outputs durable/finalize 后才开放 steering 和下一 sample。提问计入原有 16 samples/64 calls 上限；不新增执行循环或重置预算。

#### Scenario: Preserve a question barrier
- **WHEN** sample 按 Read、Question、Write、Question 排列
- **THEN** 两个问题逐个处理且不重叠，Write 仍需单独批准，所有结果和 outputs 保持原 index
