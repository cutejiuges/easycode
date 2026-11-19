## Context

`internal/runtime/compaction_loop.go:72` 接纳独立手动压缩。`internal/runtime/auto_compaction.go:63` 在初始、工具续写和 Stop 反馈交付的合法采样边界准备自动压缩。两条路径都在纯预检后进入 `internal/runtime/compaction.go:152` 的 `runCompactionOwned`，由当前 worker 独占到摘要清理和 checkpoint 安装完成。Provider 编译器将摘要提示追加在请求末尾，`CompactionSource` 标识已有提交窗口。

两个参考工程已按只读快照核对。

| 参考快照 | 实际行为 | EasyCode 取舍 |
| --- | --- | --- |
| Claude Code 2.1.88，`a8a678cb6244e6770e1e421767ff0987a1d95549` | `restored-src/src/utils/hooks.ts:3961–4024` 按 `manual` / `auto` 匹配，聚合成功 stdout 并生成终端提示。完整摘要在 `services/compact/compact.ts:369–380,406–443` 先放用户指示，再追加 Hook 内容。`screens/REPL.tsx:2497–2512` 分开显示检查和发请求。配置帮助描述 exit 2 阻断，完整摘要调用点没有消费通用执行器的 `blocked`。 | 保留完整摘要的触发、顺序和分阶段反馈。用结构化字段和逐声明授权传递摘要重点。 |
| Codex，`c0cc9fc820f62e4dbdde68214dd6d792d6c8a7ee` | `codex-rs/hooks/src/engine/discovery.rs:898–939` 先筛选可执行且通过信任的声明。`engine/dispatcher.rs:109–174` 调度多项并按配置顺序汇总。`hooks/src/events/compact.rs:275–319` 依据受信任同步处理器的 exit 0 `continue:false` 停止，允许 `stopReason`，普通执行错误记录诊断。`core/src/compact.rs:178–238`、`compact_remote_v2.rs:201–240` 和 `compact_token_budget.rs:42–65` 分别在前台摘要、远端候选安装和窗口重置前检查。 | 采用明确的同步停止信号。全部匹配先审查再串行执行，脚本或解析失败也关闭本次压缩。停止响应只接受 `continue:false` 单字段。自动失败沿 EasyCode 的旧窗口预算策略处理。 |

Claude Code 的手动和自动入口均可能通过 session-memory 提前返回（`restored-src/src/commands/compact/compact.ts:40–108`、`services/compact/autoCompact.ts:287–321`）。reactive 手动入口单独触发一次 `manual`（`commands/compact/compact.ts:139–178`），partial 路径使用空 `custom_instructions` 并另行合并用户反馈（`services/compact/compact.ts:812–842`）。Codex 的后台远端候选可以在前台检查之前生成（`codex-rs/core/src/session/async_compaction.rs:105–151`、`compact_remote_v2_attempt.rs:331–367`）。本切片接入 EasyCode 现有的全窗口手动和阈值自动摘要。两条路径在检查通过前均无摘要请求。

## Goals / Non-Goals

用一条宿主控制链连接压缩、信任、受限命令和 durable replay。普通采样、摘要生成和 checkpoint 安装各自保有原来的权限与提交边界。用户可从 `/hooks` 看清触发方式、可发送的内容和是否完成检查。`PostCompact`、异步 Hook、插件贡献和信任持久化属于其他切片。

## Decisions

### 1. 使用已有压缩 worker 和双阶段请求预检

手动与自动路径都先对原始摘要请求做现有纯预检。无历史、不可比较的预算、请求过大或推理选项冲突按既有规则处理。预检通过后，当前 worker 选择冻结的检查计划，并 Sync 单记录 `compaction_started`。匹配后复用同一个 Hook runner 和上下文取消链，先取得全部声明信任，再串行运行。

```text
base preflight -> compaction_started Sync -> review -> serial checks
  -> final preflight -> gate decision Sync -> summary request
  -> completed Sync -> finalize window -> release worker

definite pre-request failure -> compaction_failed Sync -> release worker
```

追加摘要指示时，使用已经冻结的 `CompactionInput` 副本。按顺序拼接用户原始规范化重点和被允许的 Hook 重点，使用两个换行分隔非空段。已有 16 KiB 约束覆盖合并后全部字节。编译器在同一 `CompactionSource`、scope、任务快照、推理档位和输出额度下重新估算并核验完整请求。无新增重点时复用原 `CompactionPreview`，实际发送继续由 Provider 从冻结输入编译。通过决定在最终预算验证之后保存，合并或编码预算失败保存预算拒绝决定，即使全部命令已经返回 continue。来源变动作为内部不变量失败处理。原生编译器在摘要请求后段使用重点，tool choice 保持禁用。Hook 身份、摘要指示、执行时刻及诊断不进入稳定前缀。`CompactionSource` 标识旧提交窗口，request receipt 描述实际发送的请求。

每个已知 CompactionID 只运行一次检查，最多发起一次摘要生成。若服务端返回过长错误或流失败，按当前终态收口。OpenAI 已有未发出生成请求前的 WS 握手回退可继续复用同一预检输入和决定。

### 2. 对命令输入、输出和授权分别建模

`domain` 为 `HookScope` 与 `HookTarget` 各增加压缩分支。前者封闭匹配 `manual`、`auto`、`*`，后者绑定实际 `CompactionID` 与严格 scope。`extension/hook` 负责现有冻结配置的第五个事件和专属严格输出解析。`executor/process` 继续负责 descriptor-bound 工作区、最小环境、只读禁网 shell、有限 stdout/stderr 和最终清理。`runtime` 组合这些职责，保留同一个进程内信任表。

示例配置的 `feedback` 省略时为 `none`。

```json
{
  "hooks": {
    "PreCompact": [
      {
        "matcher": "manual",
        "hooks": [
          {
            "id": "retain-verification",
            "type": "command",
            "command": "sh .easycode/compact-focus.sh",
            "timeout": 5,
            "feedback": "model"
          }
        ]
      }
    ]
  }
}
```

脚本 stdin 为强类型 JSON，包含 `hook_event_name`、`session_id`、`thread_id`、`compaction_id`、`cwd`、`trigger`、`custom_instructions`，自动路径另带真实 `turn_id` 和 `sample_index`。原始规范化指示为空时使用 null。每项都读取同一份原始指示。stdin 排除 transcript 路径、未提交输入、Stop 待交付反馈、完整历史、opaque reasoning 和连接凭据。来源上限及候选上限继续跟随既有 32/64/8；进程累计执行额度 30 秒，单项限制 1–30 秒，默认 5 秒。用户审查可取消，计时与 Provider 的 120 秒请求期限分别开始。

| 进程结果 | Gate 解释 |
| --- | --- |
| exit 0，空白 stdout、`{}` 或 `{"continue":true}` | 放行该声明 |
| exit 0，恰好 `{"continue":false}` | 当前压缩以 `hook_denied` 收口，后续 Hook 不启动 |
| exit 0，`{"hookSpecificOutput":{"hookEventName":"PreCompact","summaryInstructions":"保留测试失败和复现命令"}}`，可同时含 `continue:true` | 仅 `feedback:model` 声明接收经过规范化的非空 UTF-8 重点，每项最多 4 KiB |
| 非零退出、截断、无效 JSON、未知/重复字段、混用停止与输出、未授权重点 | 使用有界内部原因终止检查，公开失败码为 `hook_failed` |

手动审查明确指出每项受信任脚本会通过 stdin 读取本次 `/compact` 的规范化重点，两种 feedback 策略均如此。`feedback:model` 另行说明脚本可以把合规重点发送给本次摘要 Provider，生成的摘要也可能引用这些内容。终端反馈、Session、错误及日志只消费安全分类、长度和指纹。脚本的非结构化输出及所有 stderr 留在已有有界执行生命周期中。

### 3. 将计划、执行事实和最终决定绑定到同一次压缩

`compaction_started` 维持单记录 batch，新增必需 `pre_compact` 联合。`none` 表示完整合法来源没有匹配，`invalid` 表示来源或匹配非法，`planned` 包含冻结顺序的声明引用和计划 digest。每项使用现有引用加上 PreCompact 专属的规范 matcher。digest 覆盖压缩 ID、scope、窗口来源、原始规范化重点的长度与 digest 及包含 matcher 的有序声明引用。回放用 matcher 验证已记录的项适用于当时的 trigger。配置命令和信任授权留在进程内。

`pre_compact_hook_started` 每项在 runner 准入前 Sync，`pre_compact_hook_finished` 在 runner 返回清理结论后保存真实结果及已接受重点的长度/digest。`pre_compact_hook_decided` 保存有序结果前缀的最终 proceed/stop/failure 决定。只有 `none` 或符合完整前缀、预算预检和成功 Sync 的 proceed 才可发摘要请求。`invalid` 直接收口 `hook_config_invalid`。`planned` 若尚无可交互信任能力，先于平台和 runner 检查以 `hook_trust_required` 收口。审查拒绝为 `hook_trust_denied`，已授权停止为 `hook_denied`，已接受重点导致超限沿用 `compaction_budget_exceeded`。摘要发出前的失败计量为 unavailable。

在完整行解码边界校验记录必须是独立单记录 batch，并拒绝外层 TurnID 或 ParentThreadID。Loader 因此能在发出修复凭据前发现伪装成未完成批次的非法完整行。`session/compaction.go` 现有回放状态机再核验操作、计划、声明、有序结果、允许披露的字节数及终态配对。离线回放能重算计划 digest 和组合长度，实际请求内容由 live receipt 和双 Provider 验收核对。自动压缩的每条合法事实都推进所属 turn 的历史尾部位置。

中断时允许已提交的合法事实前缀，包括只有计划或只保存执行准入的情形。`app/compaction_restore.go` 复用同一 lease 和中断压缩收口，追加唯一 `compaction_failed(compaction_interrupted)`。恢复不会补造命令结束或决定，也不读取脚本、重新评审或发出摘要请求。若有已持久化的 Stop 反馈，仍在压缩收口后按原顺序本地交付。已完成 checkpoint 通过现有 Provider 校验和恢复入口安装。

### 4. 信任反馈属于宿主，压缩控制属于既有 owner

以 Session、thread、对应请求、CompactionID、scope、计划 digest 和声明为校验键。手动压缩走独立 compaction worker 路由。自动压缩保持外部真实 turn 归属，关联其当前 CompactionID 和目标 sample。两条路线从仅有 started/terminal 的现有关联中显式识别 transient 宿主 Hook 项。阶段反馈与 Hook trust 必须通过当前身份后投影，context 数值查询仍返回最近已提交窗口。

TUI 沿用 `/hooks` 详情和默认拒绝 overlay。接纳及 durable started 显示准备状态。检查运行、等待审查和摘要请求各有可关联的宿主阶段通知，只有摘要请求准入后才显示正在摘要。Esc 在审查中拒绝，Ctrl+C 取消当前手动压缩或自动压缩所在 turn。取消信任等待时可没有 resolved 回执，匹配的压缩终态和控制流关闭都需释放本次 overlay。过期回执不得重开审查或改写草稿。只有能完整查看命令、触发和披露权限的界面才允许信任。外部 stream-json 继续使用现有压缩 response 和 lifecycle 帧，新增受控失败码。一次性输出继续走现有普通任务投影。

自动路径由活动 turn worker 标识 started 提交前的失败。主 owner 可以发布该次 `durable=false` 的终态，保持真实 scope、原 submit 与清理顺序，无虚假 started，也不把该失败误分类为活动压缩的身份错配。其余 Hook 展示项不改变压缩身份、控制快照或窗口版本。

### 5. 失败回退依赖确定持久化与资源清理

在未写 Hook started 时取消不会启动 shell。已准入进程由共享 owner 定向取消并等待它的结果与清理结论。已接纳的 journal batch 必须返回确定结果。若 runner 明确报告清理不确定，停止一切新副作用，在健康 writer 上保存不确定结果、失败决定和 `compaction_failed(session_failed, unavailable)`，随后 poison 并关闭接纳。现有资源 owner 持续承担收口责任。若任一写入不确定，立即 poison 并发布 `session_failed`（`durable=false`），由后续恢复验证合法前缀并收口。内部窗口来源不一致也进入失败关闭路径。取消与完成竞争时，已经确定 Sync 的 checkpoint 按现有语义安装。自动压缩遇到已确定且可恢复的 Hook 失败，计一次并走当前来源抑制和三次熔断。只有旧窗口通过完整普通请求预检时才继续。未提交输入按已有规则保留或返还，取消终止当前任务。独立手动压缩继续沿现有 terminal、队列和清理契约。

`pre_compact_hook_started` 证明执行准入意图，取消抢先时 runner 可返回确定未执行的取消事实。`proceed` Sync 返回后再次判断操作取消，再接纳 Provider 调用。取消先发生时保留 proceed 作为已完成检查证据，以 `compaction_cancelled` 和 unavailable 收口，零摘要请求。自动抑制与 Hook 信任重置是不同状态，`/hooks` reset 只清除当前声明的信任或拒绝，手动 `/compact` 仍可显式触发。

## Risks / Trade-offs

- 压缩原本只有 started/terminal 三种事件。宿主 Hook 活动如果误归为 terminal，会提前释放 owner。测试应在 Hook 阻塞时同时驱动 `/context`、排队和取消，并验证后续真正 terminal 与清理顺序。
- 原始候选符合模型预算时，追加重点仍可能溢出。以两家 Provider 实际 canonical 请求预检为准，并在确认超限后保存可解释的失败。
- 操作失败与模型请求失败的计量不同。未调用 Provider 时只记录 unavailable，避免制造零用量或原生 provenance。
- 用户可能在等待信任时更新 effort。冻结源数据、任务快照和推理策略贯穿两次预检，更新按现有效果边界应用。

## Migration Plan

使用唯一当前无版本 Session 结构，替换涉及 `compaction_started` 的固定当前 fixture，并添加实际消费的匹配及拒绝 fixture。旧开发压缩记录缺少当前必需字段时在 repair 前报错。实现验收后将主规格、README、能力清单、Roadmap、压缩架构及踩坑记录与实际的字段和验证结果同步。
