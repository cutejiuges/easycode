## Context

动机见 proposal.md。本变更来自代码审查；审查时工作区干净、无活动 change，`make verify` 和 29 项主 spec 严格校验通过。这是已有测试基线，不代表以下缺口已经有故障回归。

| 发现 | 当前证据 | 本变更落点 |
|---|---|---|
| 宿主生命周期分叉 | `runtime/session.go` 直接驱动 turn；TUI/stream-json 使用 AgentLoop；`app.go` 始终构造并关闭前者 | 一个控制 owner，adapter 不再自行执行 turn |
| TUI 清理不闭合 | `app/tui_session.go` 阻塞发送且无 dispatcher done；`runTUI` 忽略 stop 错误 | 显式运行结果、停止信号、完成信号和释放顺序 |
| 工具生命周期漂移 | `runtime.go` 的 Patch grant 先于 Sync、started 使用 WithoutCancel、非法结果降为普通 error；Write/Exec 有不同保护 | 共享审批/执行事务骨架与 capability 策略 |
| codec 全局依赖 | `codec/json.go` 读取 Sonic 导出的可变 ConfigStd | 显式配置值，固定基础 codec |
| 旧构造入口 | `provider.go` 简化构造器自动补空 discard；生产 Provider 使用 WithDiscard | 一个显式完整构造入口，迁移所有测试 |
| 严格解码与守卫缺口 | 多包自行使用标准库 decoder；Read null 默认化；构造检查只检查 protocol，动态类型检查未含 tool | 集中基础校验、契约回归、AST/import 守卫 |
| 文档滞后 | 总体架构仍把 Tool 列为占位；部分 payload 注释仍称 v1 | 同步当前能力与 record vocabulary |

未发现需要保留或迁移的 V1/V2 业务模型。真实外部 version/canary、内容 revision、Provider unknown item 和已批准的未来占位不属于旧实现。

## Goals / Non-Goals

**Goals:**

- 所有已指出的问题在一个 change 内交付，完成项必须对应行为验证，不能以拆文件代替职责拆分。
- 保持 `domain/protocol <- 中间能力层 <- runtime <- app/宿主`；工具副作用仍在 executor，Provider wire 仍在各自 Provider。
- 使关闭、授权与执行结果的确定性成为共享契约，测试能注入接收前取消、Sync 失败、慢消费者和阻塞 stream。

**Non-Goals:**

- 不新增工具、通用权限规则引擎、后台命令、artifact、Windows sandbox、跨 Provider fork 或扩展能力。
- 不引入旧 schema reader、在线 migration、新外部 JSONL 版本或新 Session record kind。
- 不统一两家 Provider 的 wire DTO，也不把不同工具的候选准备和执行策略硬合并。

## Decisions

### 1. 一个控制 owner，多种宿主投影

保留 AgentLoop 作为会话级 admission、队列、定向 interrupt 和 turn 启停的唯一 owner。一次性 headless 也经其提交一个输入，完成后关闭 admission；TUI 保持活动时禁止 composer 提交，stream-json 保持 FIFO。删除 `runtime.ChatSession` 自行驱动 RunTurn 的实现，必要的 ChatSession 形状只作为薄 adapter 接口，不能保留独立 active/cancel/turn 生命周期。

应用资源装配只登记实际启动的控制生命周期。每个 owner 的输出只能有一个消费者；一次性/TUI adapter 投影 turn events，stream-json runner 投影 control items，不能在同一个 loop 上同时启动两个分发器。正常模式保留事件顺序和 terminal，不以丢事件解决背压。

替代方案是仅修补 TUI stop 并保留两套会话驱动；这仍会让后续 shutdown 修复只覆盖部分宿主，故不采用。

### 2. 关闭先解除宿主背压，再等待内核，最后释放资源

adapter 显式 Start/Run，持有固定停止信号、dispatcher done 和最终运行结果。启动 context 取消也必须取消并等待已启动 owner，不能直接返回遗留 goroutine。停止后拒绝新订阅，停止向已退出宿主发送，继续排空内核输出直到关闭；只有宿主明确退出后才丢弃其瞬态投影，Session durable facts 不受影响。

顺序为：停止 admission/宿主投递 -> 请求取消活动 turn -> 排空 loop 输出并等待 worker、loop、dispatcher -> writer 最终 Sync/Close -> workspace/repository/provider 等剩余资源关闭。每个 channel 的关闭归其创建 owner，失败 admission 与 dispatcher 不能竞争关闭同一事件流。

首次 shutdown 等待超时要返回或保留错误，同时 cleanup owner 继续存在。应用升级路径先取消真实活动 stream 并由 transport 关闭其 body，必要时使用边界层显式强制取消能力，再等待固定 done；不能假设 Resty 的空闲连接 Close 可以终止任意活动 stream。不得在后台仍可能写 journal 时释放 lease/workspace。重复 stop 观察同一最终结果，runTUI 不吞掉关闭错误。锁内只进行有界状态转换，通知与等待在锁外。

替代方案为扩容 channel 或每次 Stop 启动等待 goroutine；两者都不提供生命周期证明，故不采用。

### 3. 工具调度、批准状态和执行收口按职责组合

Runtime 保留 turn/sample 与 native commit 编排。其内部组合一个有序工具调度组件、一个实例级批准状态组件，以及副作用工具执行骨架；这些组件留在 runtime 层，通过小接口使用 Journal、Policy 和 executor，不让 tool 反向导入 session/runtime。

共享骨架负责：校验候选关联 -> 决策及等待 -> durable approval -> 接收 execution started -> 调用一次 executor -> 验证结果关联 -> durable result -> 不确定状态 poison。骨架持有策略接口；策略负责 Patch/Write/Exec 的强类型候选、展示和结果细节。不得通过 struct embed + 方法 shadow、any candidate、字符串工具名分派或新的全局 registry 实现共享。

边界规则：

- Policy deny 优先于已有 grant；只有 ask 可由同 capability grant 满足。Exec 始终 ask，无 session grant；不能让可注入的 allow 绕过其既有产品规则。
- Patch 与 Write grant 均在对应 allow_session 成功 Sync 后安装；批准来源根据实际 policy、interactive、headless、session_grant 或 interruption 产生，不能猜测为 interactive。
- approval decision 接收后必须形成确定事实；执行接收前再次检查 turn 取消。started append 使用可取消 context，writer 接收后按既有线性化返回确定结果。已知取消且 journal 健康时产生配对 cancelled result，不 poison；未知 durable 结果才 poison。不能以 WithoutCancel 绕过接收前取消。
- executor 返回后校验 invocation ID、Provider call ID、capability、wire kind、status/metadata 与候选的对应关系。副作用已被接收而返回非法结果，统一为 outcome_uncertain/invalid_tool_result，停止后续副作用，健康 journal 完成配对与唯一失败 terminal。
- 保留只读组有序并行、exclusive 屏障、Write 的 Read 观察表时机、Patch 基线重验、Exec 进度/单次授权。骨架只共享确实一致的事务步骤。

### 4. 唯一显式 prepared commit 构造入口

`NewPreparedSample` 与 `NewPreparedToolOutputs` 改为显式接收 finalize/discard；删除 WithDiscard 别名及自动补空回调的重载式入口。生产与测试统一迁移；测试不需要释放资源时在测试调用点显式传入空回调，不能为测试保留生产兼容 API。

继续以单次原子状态转换约束 pending -> finalized 或 discarded。durable 成功后 finalize，durable 前失败 discard；无效/重复/竞争操作不得再次触发回调。调用方必须完成回调再宣告其 owner 清理结束，不以单纯状态值代替完成信号。

### 5. 固定 codec 与分层严格校验

基础 codec 由显式配置字面量构造冻结 API；不读 Sonic ConfigStd/ConfigDefault/ConfigFastest，也不新增本项目包级可变缓存。可使用短生命周期值或实例持有 codec；优先保留现有纯函数边界。固定排序、EscapeHTML、CopyString、ValidateString 等选项以保持合法当前 canonical bytes。

JSON 编解码入口收敛至 `internal/codec`，业务包允许使用 `encoding/json.RawMessage` 等类型，但不自行调用标准库编码、解码或扫描 API。确需 token 扫描以检测重复字段、必需字段和无损 Unicode 时，将其限制在 codec 内；typed encode/decode 使用固定 Sonic 配置。codec 只实现语法与结构基础设施，具体 kind、允许字段、nullability 和业务 validator 留在所属边界。

封闭自有 schema 的严格模式拒绝重复键、未知键、大小写别名、尾随 JSON、非法 UTF-8/孤立代理项、缺失必需字段以及非 nullable 字段上的 null。缺省值只应用于省略的可选字段；Read/Glob/Grep/Exec/Write/Patch 输入规则一致。Session 与 Provider 自有持久化信封同样执行严格规则。

Provider 外部 wire 的受控未知扩展继续由对应 Provider 无损保存，不因共享 strict 模式拒绝合法未知 item，也不将 opaque 内容解释为共享 schema。显式记录各边界的 required/optional/nullable 字段，覆盖嵌套对象；不能通过默认零值隐藏字段缺失。旧非法输入失败关闭，不回退到宽松 parser。

### 6. 守卫与文档作为本次交付的一部分

架构守卫覆盖 tool 核心导出 API、所有生产 New* 的直接副作用及项目内可解析调用链；外部依赖不能靠 AST 证明时以明确边界检查和生命周期测试补足，不声称静态分析完备。增加标准库 JSON 调用越界、第三方全局 codec 读取和已移除构造别名的负例；保留真实版本与 opaque 类型的正例，避免正则误伤。

包可达不等于导出符号被生产使用：此次显式删除已确认的仅测试旧入口，而不是用任意“未调用导出函数”规则误删合法边界。文档纠正 Tool 占位描述、六项实际工具、当前 record/canary 与未完成阶段；不扩大 AGENTS 占位 allowlist。

## Risks / Trade-offs

- [会话归一可能改变取消语义] -> admission context 只管接收；宿主取消通过显式 interrupt/stop 转换，使用相同用例覆盖三种入口和退出码。
- [严格解码可能改变 native 数据或 checksum] -> 对合法 current fixture、原生 opaque 字段、下一请求 canonical bytes、item 顺序与 fingerprint 做双 Provider 精确回归；负例收紧不自动改写旧文件。
- [共用骨架可能掩盖工具差异] -> 用 capability 策略保持候选与结果强类型，单独测试各工具约束和混合调用顺序。
- [关闭测试容易只证明最终超时] -> 使用可控 channel/barrier/故障钩子证明取消、完成信号及资源释放顺序；不依赖 sleep 或 goroutine 数量猜测。
- [原质量门遗漏行为缺陷] -> 新增业务断言与架构负例，并再次执行全量 verify；跨平台运行缺失必须明确记录，不能以交叉编译代替运行验收。

## Migration Plan

在同一 change 内按 codec/构造入口、会话生命周期、工具骨架、守卫与文档逐步实现和验证，全部任务通过后才同步 specs 并归档。没有已发布用户迁移：内部签名直接替换，所有生产调用与测试一起迁移。当前合法 Session/headless/native 格式和 canary 保持不变；旧非法 shape 直接拒绝。若实现发现必须改变持久化 shape，应先修订本 change，而不是私自增加兼容 reader。

回退采用整组代码与 artifacts 回退，不提供双实现运行开关，不回写或迁移 append-only journal。验收必须包括 `make verify`、OpenSpec 严格校验和本 change 的场景对照表；不能只完成其中一组问题便归档。
