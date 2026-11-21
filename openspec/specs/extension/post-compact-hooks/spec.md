# extension/post-compact-hooks Specification

## Purpose

在手动和阈值自动压缩成功安装新窗口后，允许用户授权本地同步命令核验实际保存的语义摘要，分别呈现压缩结果与检查结果，并让恢复保留已提交窗口及有序执行事实。

## Requirements

### Requirement: PostCompact observes an installed checkpoint

PostCompact SHALL 读取启动冻结的用户和项目来源，按本次 `manual` 或 `auto` trigger、声明的单一字面 matcher 和既有来源顺序选取同步命令。每项 matcher MUST 为 `manual`、`auto` 或 `*`，feedback 仅支持省略或 `none`。合法无匹配 SHALL 维持现有压缩和续写路径。合法匹配超过八项时 MUST 先于模式判断跳过后置检查，记录安全原因 `hook_match_limit`，禁止截取部分执行；来源无效继续服从共享来源和 PreCompact 规则。若只有完整计划造成成功记录超过编码限额而跳过分支可编码，SHALL 以 `hook_plan_limit` 保留同一成功 checkpoint，零命令和信任请求。

只有完整成功记录已 Sync、新有效窗口已 finalize 且宿主已安装其摘要历史和快照之后，MUST 才可审查或派发 PostCompact。此前的 PreCompact 拒绝、摘要失败、预算拒绝、取消或未确认 Sync 均不得触发命令或审查。只选择最终安装的成功 checkpoint，传输回退及未安装的候选不得生出额外计划。PostCompact 不能改写成功 checkpoint、已安装摘要、usage、任务或权限，也不能触发额外模型调用、上下文注入或压缩重试。

#### Scenario: Match the trigger after successful installation

- **WHEN** 手动压缩成功保存并安装，用户声明为 `manual`，项目依次声明 `auto` 和 `*`
- **THEN** 只审查用户 `manual` 和项目 `*`，全部获信任后按稳定顺序执行各一次

#### Scenario: Do not run for a rejected candidate

- **WHEN** PreCompact 停止本次压缩、候选未通过检查或 checkpoint Sync 未能确认
- **THEN** PostCompact 审查与执行次数均为零

### Requirement: PostCompact discloses the exact summary only to trusted commands

每项声明 SHALL 沿用独立且默认拒绝的会话内授权。审查 MUST 显示完整命令和来源、触发作用域、timeout、只读禁网限制、对当前摘要的读取权限。响应 MUST 校验实际 session/thread、CompactionID、scope、事件、声明快照、plan digest 和 request ID。平台和实际输入编码预检先于审查，所有匹配项信任完成前命令启动次数为零。已保存的 PreCompact 或其他信任不能授权 PostCompact。非交互的合法限额内匹配 SHALL 在本次平台前检、输入准备、审查、命令启动之前跳过，安全原因为 `hook_trust_required`。

stdin SHALL 是最多 512 KiB 的规范 UTF-8 JSON，包含 `hook_event_name: PostCompact`、`session_id`、`thread_id`、`compaction_id`、规范工作区 `cwd`、`trigger` 和 `compact_summary`。手动作用域 MUST 不含 turn/sample 字段；自动作用域 MUST 携带真实 `turn_id` 和 `sample_index`。每项收到同一次已安装语义摘要，即作为可见 generation 投影的原字节。转义后超限 SHALL 以 `hook_input_limit` 跳过，禁止截断或启动。除该摘要外，MUST NOT 附带原生摘要包装或其任务状态、旧会话正文、transcript 路径、Provider 原生或推理数据、待提交输入、未交付 Stop 反馈或凭据。

#### Scenario: Prevent a previously trusted pre hook from reading the summary

- **WHEN** 相同命令同时用于 PreCompact 和 PostCompact，且仅前者已获信任
- **THEN** 独立审查 PostCompact；拒绝即跳过整个后置计划，压缩已保存，无摘要披露

#### Scenario: Use the installed bytes or skip the entire disclosure

- **WHEN** 自动压缩摘要含 Unicode、换行和 JSON 需转义字符
- **THEN** 合法编码的 `compact_summary` 逐字等于已安装摘要，只有实际压缩的身份；若编码超限，零脚本且检查点保留

### Requirement: PostCompact reports observation results independently

检查 SHALL 复用只读禁网和最小环境执行器，在匹配顺序内串行运行。每项执行期限取声明超时与剩余执行预算的较小值，累计执行预算为 30 秒。额度到期后仍须等待最终清理，信任与写入等待独立。stdout 与 stderr 的捕获限额各 16 KiB。结果 MUST 按清理不确定、取消、实际超时、输出超限/I/O、退出状态的顺序分类。正常 exit 0 表示通过，其他非零（包括 2）表示 `hook_exit_failed`。限额内输出不解释为控制、异步请求或附加上下文；原始内容和自由文本原因不得进入历史、模型、日志、UI、错误或 artifact。命令清理确认的失败发生后，只要取消、执行额度与写入状态允许，继续下一声明。

普通拒绝、已确认清理的错误和跳过只改变检查状态。用户取消 SHALL 停止未准入后缀并等待已接收进程及输入输出清理，保留已安装窗口。已观察且保存的运行结果不因后到取消被改写。后置事实未能写入或清理不确定 SHALL 停止后续接纳，保持已知成功的 checkpoint。取消先于 started 写入接纳且尾部可以正常关闭时，按普通取消处理。当前窗口和宿主投影已成功安装时，公开压缩终态仍是一次成功；自动续写按既有取消、预算准备与 Session 健康状态决定。

#### Scenario: Keep command output local and inert

- **WHEN** 正常退出的命令返回含控制字段和测试哨兵的输出，另一命令返回 exit 2
- **THEN** 第一项按 exit 0 通过，第二项记录退出失败；压缩仍成功，输出不进模型、日志或 Session，续写使用同一已提交窗口

#### Scenario: Wait for confirmation when cancelling an active process

- **WHEN** 已安装窗口，用户在第一个 PostCompact 进程中 Ctrl+C
- **THEN** 不准入后缀，保留 worker、writer 和 lease 至有确定清理结果；成功检查点与实际取消分别显示，自动路径不开始下一次普通请求
