## Context

动机及已确认范围见 [proposal.md](proposal.md)。本设计已完成实施与验收，48 项任务及主规格同步均已完成；实际文件、测试与平台证据见 [verification.md](verification.md)。文末规划阶段记录保留当时的基线与问题，当前状态见归档前核对。

### 当前接缝与限制

| 实际源码 | 观察 | 设计约束 |
| --- | --- | --- |
| `internal/runtime/runtime.go` 的 `runTurn` / `commitFinalSample` | 无 calls 的成功 sample 与 usage、turn_completed 同批提交，随后立即返回 | 必须在有 Stop 时拆开“sample 已保存”和“回合成功”，不能先发 completed 再撤回 |
| `internal/session/replay.go` 的 `acceptSampleBatch` | sample batch 至少三条；第三条是 completed 或 ready，单独 native commit 被解释为 outputs | 新的 Stop 边界必须有明确 grammar，不能根据 batch 长度猜测 opaque role |
| `internal/domain/hooks.go`、`internal/protocol/hooks.go` | plan、run、trust、activity 均绑定 mutation invocation/capability | 回合检查需要真实目标，不能制造假的第十项工具或把 TurnID 填入 invocation_id |
| 两家 Provider 的 `history.go` | 投影只读取已提交历史，跳过无可见正文的 sample | 不能直接选视图中最后非空 assistant；那可能是之前工具 sample 的 commentary |
| `internal/provider/user_input.go` | prepared native commit 已有 finalize/discard 组合骨架 | 共享纯提交生命周期；Hook 反馈仍须有独立 typed API 和来源 |
| `internal/runtime/auto_compaction.go`、主规格 `context/auto-compaction` | 自动摘要只在初始输入或完整工具 outputs 后准备 | 新增 Stop 决定闭合、反馈尚未交付的候选安全点；只摘要旧窗口，原字节交付候选 |
| `internal/tool/builtin/hook_process.go`、`internal/executor/process` | 有界双流、stdin、取消、唯一 Wait/cleanup owner 已共用 | 复用真实进程生命周期，不复制 Exec 或另建后台 owner |
| `internal/app/hooks.go` | 当前来源在旧事实 reconciliation 后安装 | Stop 恢复继续零配置读取、零外部执行，完成后才安装未来调用配置 |

### 两个参考工程的证据与取舍

本地参考根分别为 `/Users/cutejiuge/IdeaProjects/claude-code-sourcemap` 与 `/Users/cutejiuge/IdeaProjects/codex`。复核证据固定于 sourcemap `a8a678c`、Codex `c0cc9fc`；同时对照首轮 Codex `6f8bf683c`，不再以会变化的参考工程工作目录代替固定快照；这是本地快照行为，不是对最新商业发行版的承诺。EasyCode 基线为 `539d54e`。

| 工程及相对文件、函数 | 已观察行为 | 本设计采用 / 不采用的理由 | 主要验收映射 |
| --- | --- | --- | --- |
| Claude `restored-src/src/query.ts`，`handleStopHooks` 调用及 `stop_hook_blocking` 状态转换 | 无下一工具工作时检查；阻断反馈继续相同 query，设置 stopHookActive；API error 跳过普通 Stop | 采用完成前检查、同回合继续和错误不触发；EasyCode 另加两次续轮硬上限 | Runtime 单终态、错误零 Hook、三次答复同 TurnID |
| Claude `restored-src/src/query/stopHooks.ts`，`handleStopHooks` | 检查期间展示 progress、汇总及 Ctrl+O 提示；取消阻止继续；blocking error 作为 meta user message | 采用紧凑状态、独立详情和取消优先；不复制背景记忆、团队任务和原始输出回显逻辑 | TUI 快照、取消/过期响应、零伪用户输入 |
| Claude `restored-src/src/utils/hooks.ts`，`executeStopHooks` | 输入含 Stop、stop_hook_active、last_assistant_message；从最后 assistant 提取 text | 采用字段语义；正文改由精确 sample 的 HistoryProjector 提供，保留空白、不回退旧答复，不提供 transcript 路径 | 空 sample、相同正文、opaque-only 与带空白正文测试 |
| Codex `codex-rs/hooks/src/events/stop.rs`，`StopRequest` / `StopOutcome` / `parse_completed` / `aggregate_results` | 独立事件请求/结果；严格 block reason，聚合有序 feedback fragments；Stop 没有工具 matcher | 采用 typed 请求/结果、非空反馈和声明顺序；首版串行遇阻断短路，不引入并行 Hook 聚合 | decoder、声明顺序、短路、非法/空 reason |
| Codex `codex-rs/hooks/src/engine/mod.rs`，`ConfiguredHandler::can_apply_control_effects` | 只有同步执行允许产生控制效果 | 采用独立控制准入，EasyCode 额外绑定信任与事件/披露；不把普通工具批准视为 Hook 授权 | 跨事件、目标错配、迟到授权测试 |
| Codex `codex-rs/core/src/session/turn.rs`，`run_turn_stop_hooks` 后的循环分支 | 阻断反馈记录后继续同 turn，stop_hook_active 保持 true | 采用持久化反馈先于下一采样；EasyCode 另设独立续轮硬上限，sample/call 预算不重置 | 16/64 原上限、2 次新上限、计量不重复 |
| Codex `codex-rs/protocol/src/items.rs`，`build_hook_prompt_message` | Hook fragment 带来源 metadata，以 user role 形成独立消息 | 采用独立来源与 user-level 数据，不赋予 system/developer 权限；两 Provider 各自编码而不共享 wire DTO | native golden、真实用户 recall 隔离、来源伪造拒绝 |
| Codex `codex-rs/core/src/session/turn.rs`，`get_pending_input`、`has_pending_input`、`needs_follow_up` 和阻断后循环 | 待输入会影响是否结束；续轮前由唯一 owner 接收输入 | Stop 自动续轮前也领取队首 auto，保留 strict-next 屏障；通过后仍按原下一 turn 队列规则处理 | 检查中纠正、strict barrier、claim/stop 竞争 |
| Codex `codex-rs/core/tests/suite/hooks.rs`，`stop_hook_can_block_multiple_times_in_same_turn` | 三次模型答复同一 TurnID，active 为 false/true/true，反馈保留于 rollout | 将这些业务断言移植为本地双 Provider fixture 测试；不依赖参考工程测试、真实服务或其网络开关 | 三 sample E2E、重启后 canonical bytes 比较 |

参考工程允许的部分行为在本仓库会产生不同结果：exit 2 的 stderr 不自动成为模型输入；非法输出、无反馈的阻断、超时及配置损坏不会被当作通过；没有 SubagentStop、异步通知、自动信任或完整 Claude/Codex Hook 配置兼容。差异均为本次显式契约，而非实施中的隐式降级。

## Goals / Non-Goals

**Goals:**

- 以真实成功 sample 为检查依据，保留原生事实、单一终态和唯一取消 owner。
- 把用户交互保持为“答复已保存 → 检查中 → 通过完成 / 收到反馈继续 / 明确失败”。
- 让检查结果、反馈、预算和恢复均有可核对的强类型事实，不借 UI 推断成功。
- 在双 Provider、压缩、缓存及离线阅读中维持同一来源、顺序和权限语义。

**Non-Goals:**

范围排除见 proposal。额外明确：检查通过只表示用户配置的检查通过，不证明模型任务客观正确；只读沙箱不能直接运行需要写缓存/产物的任意 `make verify`，修复和有副作用验证仍经过现有工具批准。无需通用任务调度器、事件 registry、跨版本兼容或新 Provider capability flag。

## Decisions

### 0. 本次复核发现与收敛

| 缺漏 | 反例和实际依据 | 修订 |
| --- | --- | --- |
| 最新反馈可能先被摘要替换 | 两家 installCheckpoint 都把 offset 设为当前 entries 长度；合法摘要可省略具体 reason，随后采样便只见摘要 | 先保存阻断意图及最终反馈字节，再把反馈保留为待交付候选，只压缩旧窗口 |
| 检查中用户纠正被延迟 | 原稿只在工具 outputs 后领取，Stop 直接继续可能先发下一模型请求；Codex 在续轮前检查/领取 pending input | 决定闭合后、下一请求前领取队首 auto，候选保留真实 InputID，并原子交付 |
| 原 sample 配额耗尽没有决定分支 | 第 16 个 sample 合法无 calls，检查仍可通过，但阻断后不能再采样；原决定 code 表缺该分支 | 阻断意图前检查 sample 余量，无余量以 blocked/tool_loop_limit_exceeded 收口，零反馈/摘要/下一请求 |
| 多故障预检优先级未冻结 | headless 匹配大正文时既可能要求 hook_trust_required 又可能先返回 input limit | 固定取消、来源、headless、平台、编码、执行额度、信任、运行顺序，headless 不新增 Stop 专用正文投影/平台预检，启动可用性快照不改变拒绝优先级 |

跨主规格复核另发现 tool/invocation-loop 把自动追加限定在完整工具 outputs 后，且无 calls sample 直接结束；本次用完整 MODIFIED 要求明确 Stop 的完成门禁/追加安全点，保留原调度、结果预算和全部原场景。runtime/conversational-input 的 FIFO、claim、编码及 Skill 规则继续原样适用，无需新增另一套输入路径。

以下规则替换首轮的“continued 决定和 native 反馈同批、随后摘要”路径。没有新增局部 checkpoint、通用任务系统或 Provider wire 转换；手动/自动摘要仍处理完整旧有效窗口。

### 1. 选择 Stop 的阶段依据

| 候选 | 当前前置条件 | 本轮取舍 |
| --- | --- | --- |
| Stop 完成检查 | 三类工具 Hook、HistoryProjector、Session、进程 owner 均已有真实消费者 | 选择；直接补齐“结束前检查、反馈后继续”的缺口，且能独立验收 |
| 后台进程 / PTY | 目前只有前台命令，没有可恢复 handle、write_stdin 或任务 UI | 需同时设计资源长期所有权和跨 turn 通知，另做 change |
| MCP / Plugins | 有静态 Skills/Hooks，没有动态 catalog、安装贡献和远程生命周期 | 依赖面更广，当前不扩大到远程执行与认证 |
| 运行中推理切换 | 启动设置已冻结，但切换会影响请求来源、窗口和恢复解释 | 有价值但不补齐完成检查；单独明确运行时配置生命周期 |
| Subagent | thread/control 仍为批准占位 | 尚需独立 thread、fork、mailbox、预算及取消传播，不以空 facade 抢先实现 |

### 2. 配置保持四事件严格冻结，Stop 不接受 matcher

使用既有两个 hooks.json 来源。示例：

```json
{
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "id": "completion-review",
            "type": "command",
            "command": "./scripts/check-completion.sh",
            "timeout": 5,
            "feedback": "model"
          }
        ]
      }
    ]
  }
}
```

该脚本仅为用户自行提供的只读检查入口示例，不在本 change 自动安装或执行。Stop group 仅含 hooks，出现 matcher（包括 `*`）一律拒绝；不能复用 ParseHookMatcher 的三能力位图。每来源 64 KiB/32 条、合计 64 条及来源内跨事件 id 唯一继续成立；Stop 每次至多 8 条，事件规范顺序为 PreToolUse、PostToolUse、PostToolUseFailure、Stop，来源 user 在 project 前，数组顺序保留。Stop 的 feedback 仅 none/model，默认 none。

坏来源不能通过部分快照推测“没有 Stop”。因此任一冻结来源无效时，完成边界记录 hook_config_invalid 失败，不执行任何 Stop；mutation 仍沿既有门禁失败。允许已经提交的答复、只读工作及 /hooks 诊断，不能宣称回合通过。合法、无诊断且未配置 Stop 时保持原无 Hook 快速路径和 Provider request bytes。这是 proposal 明示的行为变化。

### 3. 目标与声明使用封闭联合，保持依赖方向

`domain.HookTarget` 定义当前两个 kind：tool_call（真实 invocation/capability）和 turn_stop（真实 TurnID/sample_index）。各有 typed constructor、strict decoder、validator、深拷贝及互斥字段校验。Stop 的 target 不含 Provider ID，不使用伪 invocation。run identity 绑定 target、event、plan digest、ordinal，tool/stop 目标不得互换。Stop 的 sample seq 由 Session 中紧邻的 sample commit 推导并验证，不由调用方预分配 writer seq。

HookDeclaration 对 tool matcher 与无 matcher Stop scope 使用封闭声明值，保留共同命令、来源、timeout、feedback 和 fingerprint。不要以 nullable tool 字段、any/map 或字符串拼接核心路由模拟联合。配置 wire 上三个工具事件不改变；受影响进程内身份及 Session current fixture 直接更新，不保留旧 invocation-only decoder。

`domain/protocol` 仅含值和校验；extension 负责 Load/parse；builtin 边界接收封闭 HookInput 和注入的解析策略；process owner 不依赖 Provider/Session/runtime。Runtime turn 骨架持有组合组件决定检查结果和下一采样，禁止 embed/shadow；共享仅限来源 FD 打开、信任等待、进程资源及 prepared commit 的相同生命周期，不合并具有不同语义的工具 gate 和 Stop 决定。

### 4. 精确读取本次最终 sample 的 assistant 文本

在 HistoryProjector 契约增加按 `{turn_id, sample_index}` 读取已提交 assistant 正文的纯内存投影，返回有该身份的独立快照或明确错误。Provider 内部按真实 sample entry 计数，不按语义列表长度、正文相等、工具 ready 或 phase 猜测。不存在目标为协议错误；存在但没有可见 text 为合法空字符串。OpenAI 连接该 sample 的 assistant output_text，Anthropic 连接该 sample 的 text blocks，精确保留原始空白和次序。

先成功提交/finalize sample，再调用该投影。Stop 不从流增量、TranscriptProjector、reasoning summary 或原生 JSON 抽取正文；不暴露 thinking/signature/redacted/encrypted/phase/未知扩展，不回退前一条非空消息。完整 JSON stdin 编码后超过 512 KiB 时失败，不截断后冒充完整检查。

### 5. 单独的回合完成门禁及预算

```text
successful sample without calls
             |
     no Stop and valid sources ----> existing atomic completion
             |
   sample + usage + stop plan Sync / finalize
             |
      ordered preflight / projection / trust
             |
       serial checks, first failure or block stops
       |                 |                     |
     all pass      authorized block       error / cancel / uncertain
       |                 |                     |
  decision +        decision + frozen       decision + failed
  completed Sync    feedback intent Sync    after owned cleanup
                         |
                 claim queued auto prefix
                         |
               compact old committed window
                         |
               native feedback + accepted
                  user inputs Sync
                         |
                  next sample, no recompact
                         |
               same turn and existing limits
```

只检查 root 用户回合的成功、无 ready calls 的 sample。Provider failed/cancelled、非法流、工具仍未闭合、摘要请求、手动取消及 resume reconciliation 均不运行 Stop。后续因反馈发起的工具 sample 先照常完成工具及 Pre/Post，再到无 calls 时检查。

固定上限：

| 参数 | 值及单位 |
| --- | --- |
| 同回合自动续轮 | 至多 2 次；第三次候选仍可通过；否则可授权续轮的第三次 model 阻断为 stop_hook_limit_exceeded，none/exit2 仍为 hook_denied |
| 原 turn 上限 | 16 个普通 samples、64 个 tool calls，不因 Stop 重置；摘要保持独立原计量 |
| 匹配检查 | 每候选最多 8 项，串行运行连续成功前缀，遇阻断/错误停止 |
| 单项执行 | 默认 5 秒，配置 1..30 秒 |
| Stop 总执行预算 | 同 turn 所有候选合计 30 秒，不因续轮或中间工具重置；信任等待不计时但可取消 |
| stdin / 双流 | 实际 UTF-8 JSON 最多 512 KiB；stdout/stderr 各 16 KiB |
| 结构化 reason | 非空 UTF-8，原文最多 4 KiB；输出 JSON 仍须满足 16 KiB 上限 |
| 规范模型反馈段 | 单次最多 8 KiB、同 turn 最多 16 KiB，均按实际 UTF-8 文本 bytes 计，另受请求编码和窗口约束 |

`stop_hook_active` 等于此前已提交 continued 决定数量大于零；第一轮 false，之后 true，即使中间调用工具也不复位。Hook 自身不创建 turn_started 或 UserInput；但 continued 决定闭合后，下一 Provider 请求前必须通过原唯一 owner 领取队首连续 auto 前缀，保留真实 InputID/正文与 strict-next 屏障。只领取一次；领取之后到达的输入留到下一安全点，通过后的待输入沿原下一 turn 规则处理。不能把真实用户纠正伪装成 Hook reason 或 approval。续轮样本的真实 usage 逐项持久化，只有最终成功 terminal 聚合全部普通 samples，Hook 进程不产生模型 usage。

### 6. Stop wire 与确定结果

stdin 的固定字段为 hook_event_name=Stop、session_id/thread_id/turn_id、sample_index、cwd、stop_hook_active、last_assistant_message；没有 tool_use_id、tool_name、tool_input、transcript path、历史全文或连接配置。stdin 仅交给已信任本地命令，不进入 Session。

| 已确认正常清理后的输出 | 结果 |
| --- | --- |
| exit 0，空白或严格 `{}` | 本项通过，继续后项 |
| exit 0，严格 `{"decision":"block","reason":"..."}` | 本项阻断；reason 必需、非空且有界 |
| 上项且 feedback=model、信任及 Stop/普通 sample/反馈额度有效 | 先冻结阻断意图与最终模型字节，再进入候选准备和同 turn 交付 |
| 上项但 feedback=none | 丢弃 reason 正文，hook_denied 失败结束，不悄悄发送或回显 |
| exit 2 | hook_denied 失败结束，不解析或披露 stdout/stderr，不提供自动续轮 |
| 其他非零、未知/重复字段、null、尾随值、非法 Unicode、allow、continue、additionalContext 等 | 稳定 typed 错误结束，不自动重试 |

故障优先级沿共享 runner：cleanup 不确定 > 当前取消 > timeout > 输出超限/I/O > 已确认退出及事件专属解析。超限输出不能截断再解析。所有公开错误 code/message 为安全英文；decision 使用封闭 outcome/code 校验，不按命令文字或错误字符串选择分支。None/model 控制反馈正文披露，不控制是否需要信任，也不批准修复工具。

### 7. 信任、执行和清理沿现有 owner

预检顺序固定：确认 sample/plan 已保存且 Runtime 健康 → 当前 turn 取消 → unavailable 来源/匹配错误 → headless 的 hook_trust_required → 交互平台/沙箱 → 完整 stdin 编码 → 剩余执行额度 → 全部声明信任 → 按项 started/运行。每个实际接收点重查取消，不用全局字符串匹配分支。headless 在模式拒绝后不新增 Stop 专用正文投影、平台或执行器预检；既有安装流程可能已探测并冻结 availability，普通采样也有上下文投影，这些不属于 Stop 门禁调用计数。即使启动快照记录 unsupported_platform/sandbox_unavailable，也不能覆盖 headless 的 hook_trust_required；因此多故障组合有确定结果。首次审查全部声明，未全部信任前不运行首项。审查展示完整命令、Stop 时间点、读取本次最终答复、只读禁网范围、timeout、feedback 用途及脚本依赖可变化；默认拒绝。Stop 在本回合之前的真实工具结果不会因拒绝而回滚。信任绑定当前 Session/workspace、来源 revision、事件、披露策略和命令 fingerprint；typed response 额外精确绑定 request/target/plan，迟到或跨 tool/stop 响应不得安装授权。

headless 三入口均不等待 stdin 信任，匹配项以 hook_trust_required 失败，零 Stop runner；交互的非 Darwin/不可兑现 sandbox 在信任前失败关闭。进程继续用非 login /bin/sh、最小环境、FD 绑定 cwd 和当前只读/禁网 profile。Hook 对工作区的读取不生成完整 Read 证据，不赋予 artifact 或文件写权限。

每个 Hook started 的成功 Sync 为执行接收点；接收前取消零进程，接收后 owner 恰好交付一次真实结果并完成清理。清理不确定 poison Runtime，健康 journal 仍尽可能收口，禁止后续采样/工具。30 秒限制控制新执行接收，不是抛弃资源的 shutdown deadline；关闭超时仍保留 process/stream/writer/lease owner，等待既有 Done，不创建逐次 WaitGroup helper。

### 8. Session grammar 和线性化点

新增两个 required kind：`turn_stop_hook_planned`、`turn_stop_hook_decided`；复用具有新 target 联合的 `hook_execution_started/finished`。不增加 Hook 专用 version、schema_version 或 UI records。

StopPlan 是封闭的 checks/unavailable：checks 含 target、有序 1..8 references（保留每项 Stop 事件和 none/model 披露策略）、当前续轮次数和内容 digest；unavailable 仅用于来源无效或匹配超限的已知 code，没有伪造声明或运行。两者都有 typed constructor/strict decoder/validator。source、命令及策略内容摘要不含 cwd/时间/seq；真实 record seq 由 writer 分配。

| 边界 | 必须同批的 records | 含义 |
| --- | --- | --- |
| 有 Stop 的候选 sample | native(sample), sample_usage, turn_stop_hook_planned | 保存候选及必须检查的事实；此时没有 turn terminal |
| 单项开始 | hook_execution_started | 单独 Sync 后才交付 runner |
| 非末项通过 | hook_execution_finished | 独立成功前缀 |
| 全部通过 | 最后 finished, stop_decided(passed), turn_completed | 唯一回合成功接收点；Sync/finalize/宿主投影后才发布完成 |
| 有权且有额度的阻断 | finished(denied/hook_denied), stop_decided(continued, frozen feedback) | 原子冻结执行结果和交付义务；此时无 native 反馈、无模型接收承诺 |
| 无用户追加的交付 | native(hook_feedback) | 绑定唯一 pending 决定，保存已冻结最终模型字节，不带 sample_usage |
| 有合法用户追加的交付 | native(hook_feedback), turn_inputs_committed(steering), native(user_inputs) | 三记录同批；反馈先于真实用户纠正，既有输入 pair 紧邻且 InputID 不变 |
| 已运行项阻断/错误/取消/不确定 | finished, stop_decided, turn_failed | 原因与运行统计一致；不确定明确 cleanup 未确认 |
| 预检/信任/预算等尚未启动 | stop_decided, turn_failed | 不伪造 started/finished；保留 processed 前缀，只有特定声明信任拒绝使用真实 ordinal |

只有成功 Sync 后 finalize 对应 sample/交付，失败 discard；writer 不确定或 finalizer 不变量损坏 poison。StopDecision 固定保存 target/plan digest、processed、disposition、code、origin、封闭 plan/hook 来源及续轮计数；continued 分支另含唯一有界冻结反馈，其他分支禁止正文。冻结反馈只含经授权的模型文本与来源/bytes/digest，不保存整个 stdout JSON；native commit 编译和恢复必须与其逐字节核对。

运行统计沿用现有 HookOutcome 的 continue/denied/error/cancelled/outcome_uncertain，不能另造 finished 的 blocked/passed 字符串。Stop 决定是另一封闭联合：passed/continued/blocked/error/cancelled/outcome_uncertain，origin 仅 execution/recovery；passed/continued 只能为 execution。processed 等于已保存的 finished 前缀数（包括本次终结项），新决定的运行计数不得包含未启动后缀。决定代码组合固定如下，其他组合严格拒绝：

| 决定 disposition | 允许 code |
| --- | --- |
| passed | ok |
| continued | hook_denied；必须同时有 model 冻结反馈和递增后的续轮计数 |
| blocked | hook_denied、hook_trust_denied、hook_trust_required、stop_hook_limit_exceeded、tool_loop_limit_exceeded |
| error | hook_config_invalid、hook_match_limit、hook_input_limit、hook_start_failed、hook_exit_failed、hook_output_invalid、hook_output_limit、hook_io_failed、hook_timed_out、hook_feedback_budget_exceeded、hook_feedback_encoding_failed、hook_execution_budget_exhausted、unsupported_platform、sandbox_unavailable |
| cancelled | cancelled、session_interrupted_before_stop_hook |
| outcome_uncertain | hook_outcome_uncertain；必须对应真实 pending/uncertain 项 |

决定来源不是可选杂项字段：plan 分支禁止 ordinal，覆盖全部通过、来源/共同输入/平台/headless/运行前总预算错误、项目间取消与未启动恢复；hook 分支要求真实计划内 ordinal，覆盖被拒绝的信任声明、实际运行/反馈错误、终结阻断和真实未决项。信任拒绝发生在全部审查完成前，processed=0 且没有运行。实际运行分支必须关联最后已保存 finished 的 ordinal=processed-1；真实 started 尾部也只关联该项，不能指向未运行后缀。continued 还必须是该项正常 exit_known=true/exit_code=0、cleanup_confirmed=true、非取消/超时的 denied/hook_denied，且该项声明为 model；不能借用同计划另一 model 声明或 exit 2 的拒绝生成反馈。恢复未启动取消使用 plan 来源，恢复 uncertain 使用对应 hook 来源；实际 denied 后的取消保留该 hook 来源和真实运行结果。

Stop 的 feedback candidate 仅允许出现在正常执行、denied/hook_denied 且 policy=model 的严格 block 输出；不能为了复用后置反馈而要求其运行状态为 continue，也不能放宽 Pre/Post 的原有状态/正文组合。已得到真实 denied/continue 结果后若取消或反馈编码失败，finished 保持原运行事实，决定可以为 cancelled/error；禁止为凑齐批次而重分类已确认运行结果。continued 后的窗口规划失败属于后续普通回合失败，不再追加第二个 Stop 决定。

在候选 sample 提交接收后到达的取消，不能撤回已保存正文，但可阻止后续检查并以 cancelled 结束 turn。passed/failed terminal batch 接收后忽略迟到取消并等待确定结果；已经接收的成功 terminal 不再追加失败。continued 决定一旦接收，其本地反馈交付义务不能撤回；取消只阻止摘要/下一普通请求。交付时已取消的用户 claim 必须返还，反馈单独以 owner 持有的不可取消提交完成，再收口 turn_failed；不能把不可取消 context 用于尚未接收的用户输入。复合交付 batch 在 admission 后取消且 Sync 成功时，两种 native entries 都已保存，用户输入 committed、不可返还。admission 前取消拒绝复合 batch 时，discard 后返还 claim，再完成单独反馈交付。Sync 不确定禁止任何再次写入或请求，留给实际事实恢复。无 Stop 路径保持原原子完成和取消规则。

checks 计划开放时不得插入普通用户输入、工具调用、新 sample 或压缩；continued 决定闭合后进入 feedback_pending，允许一次候选摘要但还不能采样。该状态必须以一记录或三记录交付闭合；再出现 native feedback、漏反馈而开始 sample、交付后重新摘要同一目标 sample 均为 corruption。最后 finished 与决定拆批仍非法，但决定与交付分开是明确当前 grammar，不再要求它们同批。跳 ordinal、假通过、第三次 continued、重复扣账、跨 target/event/plan、terminal 后运行均拒绝。

先区分 none/exit2 的终止拒绝与正常 exit0 model 阻断；只有后者尝试续轮时，先检查 Stop 次数，再检查 next sample index < 16，之后才允许 continued 决定；没有普通 sample 余量则 blocked/tool_loop_limit_exceeded，不保存虚假续轮意图、不领取输入或发摘要。已用满 64 tool calls 本身不禁止一个纯文本 sample；下一 sample 再请求工具时仍在调用提交前按原上限拒绝。

### 9. 待交付反馈、真实追加输入与原生提交

两家实现独立的 `StopContinuationPreparer` / `PreparedStopContinuation`，闭合反馈决定是唯一来源。输入为已冻结最终模型字节及 FeedbackOnly/WithSteering 两种封闭形态；WithSteering 才有真实有序 UserInputs 和 InputAssociation。Hook 不变成 UserInput，候选也不是任意 Message/map。没有队列或恢复时使用 FeedbackOnly；真实 queue claim 才允许 WithSteering。

预备对象持有 feedback envelope、适用时的 user_inputs envelope/association、合并候选 ActiveContextView/footprint 及完整请求编码预检。内部将现有 prepared 提交的恰好一次 resolution 与单 envelope 存储分离为包内小组件，供既有 PreparedUserInputs 与新对象组合复用；不能串联两个互相争用 pending 的单 entry preparer。一次 Sync 后以有界内存批量提交全部 native entries，一次 finalize/discard；native revision 按反馈单独 +1、复合 +2 推进，context revision 另叠加原 checkpoint 计数，不按预备对象数猜测。frame getters 深拷贝，pending 阶段不能调用另一个 preparer、开始 stream 或摘要。

frozen feedback 在决定 Sync 前一次确定最终模型文本、来源、bytes/digest，恢复不重新渲染或运行脚本。新增 native entry kind/restore role 仍为 hook_feedback；两 Provider 分别编译 user-level 原生文本，不提升 system/developer 权限、不伪造 call/output/InputID。复合交付原生顺序为 Hook 数据在前、真实用户纠正在后，不能以正文标签猜角色或权限。真实用户 Skill 附件必须保留原有冻结身份、原文和关联；只有复合交付成功后，沿既有输入提交路径激活 explicitSkills/resource eligibility 并发布一次真实输入事件。候选/返还/反馈单独交付均不能激活用户附件或权限。

交付只有两种物理 batch：`[native(hook_feedback)]`，或 `[native(hook_feedback), turn_inputs_committed(steering), native(user_inputs)]`，均不带 usage。后者全部 Sync 才能报告真实输入 committed；确定无写入时可以返还 claim，不确定时 poison 且不可重新提交。Session 依据已验证的 feedback_pending 状态识别此 batch，不依据长度猜 opaque role，也不混用 tool-output 分支。

若合并候选因真实用户输入导致请求或三记录 batch 编码超限，明确返还该 claim，重建反馈单独候选；同一边界不得再次摘要。反馈单独仍不满足普通请求预算时也必须保留已接收反馈的本地交付，随后以预算失败关闭 turn、零下一普通网络请求。有效 bounded frozen bytes 在决定前已通过独立 native/record 编码预检，完整请求预检失败不等于不能保存这一条事实；内部 native 编码/生命周期不变量失败则 poison，不伪造交付或再做决定。

profile/tool/project/skill 稳定前缀不加入配置、信任、target、cwd 或时间。只有 native 交付 finalize 推进实际历史 footprint；决定和 claim 不推进 native revision，也不伪造 usage。同一补偿事实的 uninterrupted/restored 完整请求、native 顺序、opaque、footprint 与 fingerprints 必须相等；full/incremental wire 仍分开验证。

### 10. 只压缩旧窗口，再原字节交付候选

continued 决定闭合后、native 交付之前是新安全点。先由唯一 owner 尝试领取一次队首 auto（允许空前缀），再准备完整反馈+用户输入候选并进行阈值/编码估算；需要摘要时先 discard 预备对象，保留冻结反馈及真实 InputIDs/正文，只摘要旧 committed native 窗口。已有整窗 compactionSource、checkpointOffset 和纯摘要候选契约不变，不引入局部 checkpoint 或新保留尾部格式。

摘要完成后重新准备同一候选，重复全部预算/编码预检；没有缩减、取消、预算拒绝和熔断沿原语义，最多一次尝试。原字段太大等不可由摘要修复的编码问题不触发摘要。本次待交付反馈和用户输入条目不加入这次摘要输入，旧历史中原本相同的文本不因此被删除，下一普通请求必须逐字节携带其最终交付内容；不能只要求摘要“尽量保留 reason”。

交付 Sync/finalize 后直接执行普通采样前守卫，不得在目标 sample 首次请求前再次摘要，从而保证最新反馈和真实纠正不会在首次模型消费前被替换。后续普通 sample/工具 outputs 已提交后才恢复通常安全点；新的 queued 输入不插入已冻结候选，等待后续安全点。手动 compact 在活动回合仍 session_busy。

自动摘要不运行 Stop，不重置 2/16/64、30 秒及反馈/结果额度。若摘要 started 后崩溃，恢复先本地关闭摘要，再从冻结决定补齐反馈交付并关闭旧 turn；只有真正已保存的用户输入才恢复，没有队列重建、联网续写或新 Hook 执行。

### 11. 离线恢复矩阵

完整验证、同一 lease 下合法 Repair/Sync 之后，先结束中断摘要，再处理 StopRecoveryPlan/待交付反馈，最后才安装当前 Hook 来源并接受新输入。checks 开放态和 feedback_pending 为不同强类型分支；已有工具尾部仍按原 ToolRecoveryPlan 处理，不做万能 recovery manager。

| 最后 durable 状态 | 本地恢复动作 | 禁止行为 |
| --- | --- | --- |
| checks plan 未开始或只有通过前缀 | recovery decided(cancelled, session_interrupted_before_stop_hook)+turn_failed；未运行项无 finished | 配置/脚本/artifact 读取、信任、Hook/工具执行、Provider 网络 |
| unavailable plan | 复用冻结配置/匹配错误关闭 | 重新判断当前配置或转为通过 |
| started 未 finished | 仅为真实未决项补 outcome_uncertain finished、决定和 failure 同批 | 查询 PID、重跑或声称已清理 |
| continued 已保存，native 交付缺失 | 纯内存 FeedbackOnly 编码并 Sync 唯一 native feedback，再补 session_interrupted | 重新生成决定/正文/wrapper、恢复 queue、模型网络 |
| continued 后摘要 started 未完成 | 先 compaction_interrupted，再 FeedbackOnly 交付，最后旧 turn failure | 摘要/Hook 网络重试 |
| checkpoint completed 已保存、交付缺失 | 恢复该旧窗口摘要，再追加冻结反馈并关闭 | 重新摘要或吞掉待交付反馈 |
| 一/三记录交付已保存、尚未 finalize 或采样 | 恢复全部 native entries 和真实已保存 InputIDs，只补唯一失败 terminal | 重复反馈、返还或重发已保存真实输入 |
| 决定+terminal 已保存 | 直接复用，不追加 | 重复终态或重新授权 |
| 前缀语义非法且另有半行 | 整体拒绝、全部 bytes 不变 | Repair、补偿或任何执行 |

纯内存 Provider 编码是恢复允许操作，不等于网络请求。pending 决定已接受的最终字节必须本地补齐，但不会自动发往模型；后续真实用户输入才编译下一请求。重复恢复以实际交付角色/身份验证是否已补齐，不按是否调用过函数判断；只关闭一次预算/计数，native feedback、用户正文、usage 与 terminal 均不重复。跨进程 lease 竞争/崩溃释放/禁止 repair/失败 bytes 不变继续做真实子进程验证。

### 12. TUI、宿主投影与 headless

`/hooks` 将 Stop 显示为回合完成检查，没有工具 matcher。信任 overlay 复用当前组件和完整命令预算，目标显示回合级范围；默认 Enter/Esc 拒绝，Ctrl+C 取消，paste 不授权。明确 model 反馈发送给当前 Provider 且写入私有 Session，none 不披露正文；也说明先前已执行工具不会回滚。

候选 sample 的宿主 committed 通知先解析正文/推理保存状态；主线显示 Checking completion，决定落盘后先显示 Preparing continuation（反馈待交付/候选压缩），交付后才显示 Continuing after completion check，最终通过才变 idle。错误只显示稳定安全摘要；已保存答复不能变为“未保存”。取消时等实际 cleanup 和 terminal，不能提前放行下一 turn。

Stop activity 使用带 target 联合的现有 typed Hook 事件；新增实际消费的 Stop 提交通知/只读历史快照绑定 target、plan digest、detail sequence 和 plan/decision/delivery 阶段，不借用 Post 的工具成功/失败选择。Ctrl+O 读取安全 metadata 与已授权 durable 反馈；继续复用全局四页有界缓存、异步查询 owner、代次与过期 sequence 检查，不从 render 打开 journal。

语义投影为 Hook 反馈增加独立来源角色，transcript 增加独立 Hook 反馈条目和真实 native cursor；不是用户输入召回项、不是助手声称完成、不是工具 output。live/restored 使用同一 Provider 投影，保留所有候选答复及反馈顺序；变更窗口和压缩不重编号完整历史。

headless 没有信任 bypass，正常配置匹配 Stop 时只能失败关闭，不能假装通过。`--print`、`--json`、stream-json 沿当前输出形状发布普通文本/失败，不增加 Hook、reasoning 或伪用户事件，不把检查 stdout/stderr 写到 stdout；无 Stop 的既有 golden 不变。

### 13. 验证矩阵与完成证据

| 维度 | 必须证明的业务断言 | 实际测试入口（源码路径位于 internal/，脚本除外） |
| --- | --- | --- |
| 配置/来源 | 四事件合计上限、Stop matcher 拒绝、重复键/null/未知字段、来源原子性、FD/TOCTOU、revision 稳定 | `extension/hook/stop_config_test.go`、现有 config/localfile 测试 |
| 目标/信任 | tool/stop 互斥、默认拒绝、最大命令转义、跨 target/事件/来源错配与取消后授权均零执行 | `domain/stop_hooks_test.go`、`protocol/stop_hooks_test.go`、`runtime/stop_trust_test.go` |
| 正文/wire | 精确 sample、空/opaque-only、空白不变、16 KiB 输出、512 KiB 实际编码、reason/feedback bytes、非法 JSON | 两家 `stop_projection_test.go`、`extension/hook/stop_output_test.go` 及 fuzz |
| 调度/预算 | 一次通过、两次阻断再通过、第三次 model 阻断失败、none/exit2 仍终止拒绝、false/true/true、第 16 sample、64 calls 后文本续轮、30 秒累计、headless 多故障优先级 | `runtime/stop_hooks_test.go`、`runtime/stop_trust_test.go`、`app/stop_hook_limits_test.go` |
| 持久化 | 每批次/Sync 故障、错序/拆批/重复/伪造反馈、sample 已保存但 turn 未完成、唯一 terminal | `session/stop_replay_test.go`、固定 `testdata/current/stop_*.jsonl`、`runtime/stop_fault_test.go` |
| 恢复/所有权 | 上述每一恢复窗口、重复恢复幂等、旧事实不读来源、真实跨进程 lease 竞争/崩溃释放/禁止 repair | `session/stop_fixture_test.go`、`app/stop_hook_resume_test.go`、lease 子进程测试 |
| Provider/cache | 双 Provider fixture E2E；不重跑；同一已补偿历史 full canonical bytes/native 顺序/opaque/fingerprint/footprint 等价；分别验证增量派生 | `provider/openai/stop_hook_cache_test.go`、`provider/anthropic/stop_hook_cache_test.go`、`app/stop_hook_e2e_darwin_test.go` |
| 压缩 | 冻结决定后仅摘要旧窗口、候选反馈/真实输入原字节保留、合法摘要故意省略 reason 时下一请求仍精确包含 reason、成功/失败/取消/恢复 | `runtime/stop_compaction_test.go`、`app/stop_hook_compaction_test.go` |
| 进程/隐私 | Darwin 真实 stdin、只读/禁网/私有存储隔离、exit/timeout/取消/阻塞双流、owner 保留；原始输出哨兵零泄漏 | `tool/builtin/hook_process_darwin_test.go`、`executor/process/owner_test.go`、`app/hook_host_owner_test.go` |
| TUI/headless | 80x24/100x30/40x16、NO_COLOR、审查/拒绝/reset、Ctrl+O、取消/resize/草稿/队列；三入口拒绝、外部 JSONL 不变 | `tui/stop_hooks_test.go`、`app/stop_hook_headless_test.go`、`scripts/stop-hook-smoke.py` |
| 失败触发边界 | 两家 Provider 的 HTTP/SSE 错误、缺少终态和受控取消均不创建或执行 Stop，不保存失败样本 | `app/stop_hook_trigger_test.go` |

新 fixture 必须是仓库固定 bytes，不能在测试运行时由当前 encoder 生成；故障用控制 channel、假 clock、注入 Sync/FD/进程边界证明，无 sleep 竞态。E2E 使用本地双 Provider 假服务和合成凭据，不访问公网或真实模型服务。只允许已审查、合法 model reason 成为持久化模型数据；原始双流与未授权内容的哨兵必须在日志、错误、artifact、journal 和快照中缺席。

实施验收执行 `make verify`（分支策略回归、gofmt、vet、Staticcheck、含架构守卫的全量测试、race）、该 change 与主 specs 的 strict validation、定向恢复/缓存/压缩/进程故障测试及双 Provider PTY smoke。Darwin 报告原生运行结果；Linux/Windows 至多声明实际交叉构建/测试编译与失败关闭行为，不把它们写成真实命令支持。记录命令、平台、基线 revision 和结果后再同步 specs、README、总体架构、能力清单、Roadmap、pitfall，核对真实 vocabulary 和测试文件路径，最后独立归档。

## Risks / Trade-offs

- [终态与交付拆分扩大崩溃窗口] → sample+plan、运行前 started、finished+决定、反馈/真实输入交付各自原子化；以冻结交付义务和完整恢复矩阵封住新窗口。
- [配置损坏影响普通聊天的最终完成] → 明示本次契约变化；保留已保存正文和本地诊断，合法空配置保持快速路径；不以部分解析猜测放行。
- [只读命令无法运行任意项目测试] → 文档及审查说明限制；常规修复/写入验证通过真实工具批准，不扩大 Hook sandbox 或悄悄降级。
- [可信脚本或模型持续要求继续] → 同 turn 最多两次续轮、30 秒 Stop 执行额度、16/64 原上限；不能依赖脚本自行检查 active 来防循环。
- [反馈成为提示注入或权限捷径] → user-level 检查数据、显式 model 披露、来源标记、严格预算、独立权限，不创建 InputID/grant/Read evidence。
- [修改通用 Hook 身份影响现有三事件] → 单一当前联合、直接替换固定 fixtures，既有 Pre/Post/Failure 与命令 owner、cache/request golden 全量回归。
- [小功能触及多个包] → 新代码按正文投影、配置解析、Stop 状态机、Session grammar、宿主 projection 各自分离；不把这些职责堆入 Runtime 主循环或万能管理器。

## Migration Plan

本工程未发布、无存量迁移授权。实施时直接更新唯一当前契约及 `testdata/current`；旧 invocation-only Hook shape 失败关闭，不增加 reader/迁移器/版本窗口，也不在 resume 重写旧 append-only records。合法的现有无 Stop 流程继续作为当前 grammar 的真实分支，不称为历史兼容。

代码与所有消费者、验证、文档在同一 change 完成后才能启用；不提前发布空配置、未消费事件或 capability。回退采用版本控制恢复完整变更，不能在运行时混用两个 decoder 或转换 journal。归档前需后续明确 apply、充分验收并同步主 specs，本提案阶段不实施或归档。

## 规划阶段的实际校验与既有问题

2026-11-12 在 Darwin/arm64、Go 1.24.13 上进行规划和复核；新增类型、场景及测试路径仍为实施目标，不表示 Stop 已实现。本次用户已明确批准修订现有 artifacts，并新增 runtime/input-control、tool/invocation-loop 两份 delta。

首轮现有工程 `make verify` 退出码为 0，覆盖分支策略回归、gofmt、vet、Staticcheck、含架构守卫的全量测试与 race；日志 `/tmp/easycode-stop-proposal.ZWGJkv/make-verify.log`。本轮只修订方案，不用该基线冒充新功能运行证据，不重复执行未受影响的 Go 测试。

首轮全量 OpenSpec strict 为 57/58：既有 add-pre-tool-use-command-hooks 的 session/resume MODIFIED 遗漏当前主规格后来加入的 8 个恢复场景，且部分 ADDED 已存在。该旧变更不在本轮修订范围，不删当前主规格或偷偷归档以让校验变绿。

本轮实际复核结果：

- `openspec validate add-stop-command-hooks --strict --json` 通过，零 issue。全量 `openspec validate --all --strict --json` 为 57/58；56 份主 specs 和本 change 全部通过，唯一失败仍为上述既有 Pre 变更，未修改该变更。
- artifact 完整性审计通过：proposal 的 12 个能力路径与 delta 一一对应，25 个 MODIFIED、17 个 ADDED，共 295 个规格场景；受影响主要求原有的 201 个场景标题全部保留，未发生整块替换遗漏。无已存在要求误标 ADDED、无游离于要求外的规范正文、场景 WHEN/THEN 完整。
- 48 个实施任务全部未勾选，编号唯一，每项包含测试/验证要求；文件结束换行、尾随空白和 `git diff --check` 校验通过。工作区仅新增本 change 的规划文件，没有业务源码、主 specs 或两个参考工程的修改。
- 在临时内存设计模型中复现旧路径的反馈丢失：整窗 checkpoint 的 offset 跨过新反馈，而合法摘要省略 reason。修订路径校验了 108 个恢复/重复崩溃窗口、42 种取消组合、10 类非法交付轨迹、192 种预检组合、64 种实际反馈来源组合及 48 种续轮预算组合；候选原字节、真实输入归属、一次交付和不重复扣账均满足设计不变量。
- 该模型只是契约状态推演，既不是 Stop 实现，也不代替真实 Session/Provider/进程测试。tasks 已把对应反例落实为实施后的固定 fixtures、双 Provider golden、确定性故障注入与 E2E 验收；本轮没有把任何新功能测试写成已通过。

截至本次复核，未发现剩余已知的阻塞性设计缺口；实施仍须逐项验证这些假设，若实际源码证明设计不成立，先回到 artifacts 修订，不能静默降级。只有后续明确 apply 才改代码、运行 Stop 新测试或归档。

## 归档前核对（2026-11-13）

后续明确 apply 与充分验收均已完成：48/48 项任务、12 份 delta 的 42 项需求及 295 个场景已实现并同步主规格；实际测试入口已核对，详细命令和结果见 verification.md。上轮普通 `make verify` 完整退出 0，包含全量 race；追加核心/App 随机顺序 race、输出 fuzz 及八条真实 PTY 流程均通过。

用户现已明确授权修复旧 PreToolUse delta 并归档本变更。旧 delta 已按现行主规格无损对齐，保留后续 30 个场景，架构规则不再把已交付事件误称为未实现。归档前全仓库 `openspec validate --all --strict --json` 为 59/59，两个活动变更均零 issue；本变更全部需求块与主规格一致，无待同步差异。归档不扩大 P6、跨平台沙箱或其他尚未实现能力。
