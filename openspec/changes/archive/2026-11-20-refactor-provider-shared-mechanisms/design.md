## Context

App 分别选择在线工厂、wire 和离线 reader。Runtime 和 App 的调用路径有 15 处 Provider 接口断言。两家 Provider 分别实现了历史窗口、请求计划、普通采样、独立摘要及有序回放，其中可共享的流程与原生协议细节交织。Provider 原生历史继续作为续写、恢复和持久化事实源，语义视图只供读取。[ADR 0002](../../../../docs/architecture/adr/0002-provider-native-history-and-semantic-projection.md)

## Goals / Non-Goals

**Goals**

- 以实际协议身份统一装配，让上层通过已绑定的强类型操作使用 Provider。
- 让两家共用同义的状态与执行流程，消除重复遍历、重复目录编译及已确认的重复实现。
- 对照当前实现验证成功、拒绝、失败、取消和恢复路径的等价性。

**Non-Goals**

- 保持 CLI、配置、工具 schema、原生请求与持久化、headless JSONL、交互及平台支持的现有契约。新增 Provider、wire、跨 Provider fork 和数据迁移各自评审。
- 当前行为缺陷另行评审，具体清单见下文。本次以现有行为作差异比较，不把缺陷固化成新的主规格。

## Decisions

### App 按身份装配并显式绑定操作

App 用一份不可变装配描述绑定实际 family/wire、在线工厂和离线 reader。启动、继续、新建、显式恢复和聊天内切换共用在线装配。预览用日志中已校验的身份走同一个描述符选择入口，仅读取离线分支，无需网络凭据、transport 或执行器。离线列表继续按原契约工作。

在会话装配时绑定 Runtime 所需的输入编码与准备、工具、Stop、压缩及续写清理接口，以及 App 所需的 transcript、checkpoint 投影和恢复计量接口。接口按消费者和生命周期拆分。对已确定的能力，运行路径直接使用绑定。真正可选的操作沿用现有的缺省、拒绝及故障映射。动态 transport 能力仍按会话实时读取，例如 OpenAI fallback 后的 `PreviousResponse`。构造和 getter 保持纯内存，在线资源与离线读取的打开和关闭仍是显式生命周期。

配置解析当前格式及各家专用字段。装配复用经验证的启动推理、缓存策略、身份及不可变工具视图，并保留环境变量优先级、错误分类和校验顺序。Runtime 使用实际 wire 生成缓存诊断，比较实际身份核对 receipt。Context 直接从推理设置读取有效输出上限和显式设置状态，沿用预算预留规则和档位顺序。TUI 用领域值格式化，沿用适用字段和省略时的文案。Protocol 保留封闭的 family/wire 对校验，effort 的公共形状校验使用当前档位的封闭集合，各家的取值约束仍在已知身份的边界验证。

Tool 统一 structured facade 声明与反向身份查找，共用协议身份、工具名、能力及 wire kind 的映射。保留 Patch 的原生 structured/custom 差异与 Task 的专用 schema。由经严格校验的不可变目录一次生成每家 canonical 视图与查找索引，供规划、编译和解码复用。离线验证共用纯声明和参数检验，且与 online 工具授权分离。零值、未知身份和证据不符沿用既有拒绝。视图不暴露可变引用，现有完整目录与分家族 schema、排序和 fingerprint 逐字节比较。

### 在 Provider 内共享强类型历史与准备流程

历史容器以两家各自的具体原生条目为类型参数，持有 append-only 条目、revision、checkpoint 和原生摘要，提供可独立投影的已发布窗口。锁内只捕获有界、不可变的已发布状态，深拷贝、投影、原生验证及编码在锁外执行。一个候选使用同一份已提交窗口派生请求顺序、摘要和预算证据，保持现有 revision、offset、溢出规则与提交顺序。

输入编码合法性检查保持纯内存且不占用 pending。实际输入准备、工具结果及 Stop 续写共用候选、staging 和一次性 resolution 机制。明确区分普通输入、反馈单独交付和反馈加 steering，保留 in-flight 输入返回及反馈原子落盘语义。共享工具批次的时序和待闭合 sample 定位。工具名、call ID、wire kind、输出字节及 Question/Task 冻结证据继续通过对应协议策略严格核对。复用已有 `preparedResolution`，保留 prepare、Sync 后 finalize、未提交 discard 的阶段，并维持现有 busy/active/pending 检查和 cleanup 交接的可观察顺序。

### 共用编译路径和两类执行骨架

普通预检、采样发送及摘要使用同一套请求来源冻结与逻辑编译流程。保留项目指令、Skill、工具来源和设置的原生插入位置。共享 canonical evidence、source digest、请求属性及 baseline 比较。原生 Messages/Responses 布局、Anthropic marker/TTL、OpenAI cache key、HTTP 与 WebSocket 派生仍由适配器负责。预检继续为纯内存。当 durable 提交、自动压缩或 PreCompact 改写改变来源时，按当前流程复核窗口及重新编译。全量逻辑请求、实际传输体与原生输入 digest 各自保留原来的含义。

使用 Go 的组合式 Template Method，普通采样骨架持有窄策略，串起开启传输、驱动原生 reducer、按序发布 semantic/native 事件、准备终态及关闭。摘要骨架复用同义的流驱动及清理，独立处理来源、预算、摘要终态和 checkpoint 准备，保留当前失败 provenance 与用量。保留两家的帧校验、协议扩展、取消与 EOF 错误映射、摘要输出与 thinking 额度、Anthropic `end_turn` 规则。普通采样的 durable finalize 才推进基线和 OpenAI response ID。OpenAI 继续按原条件在请求发送前的握手失败时回退，保留生成后错误不重发、discard/failure 清理和独立的全量摘要连接。

Runtime 仍负责操作预算、用户交互、权限与 Hooks、队列、Session Sync 及发布已持久化事实。Session/App 仍负责原 lease、repair 和 writer 接管。共享 Provider 流程通过已校验的值与不透明证据交互，依赖方向保持单向。

### 复用原生回放与只读投影

App 按现有 seq 把 replay plan 中的 native commit、调用及结果证据、摘要来源、checkpoint 和计量整理为强类型输入。在线恢复和离线预览复用两家的有序原生回放骨架和 checkpoint 验证。两种入口保留各自计量校验、回放、投影及故障映射的顺序。在线按当前契约处理取消与 repair，离线在记录间响应取消。Anthropic 普通 sample 保留原有 raw/normalized 对账，OpenAI 保留当前校验。压缩成功及失败的计量规则、旧条目严格验证、截断尾部和无执行器预览行为继续由原路径保障。

用每次回放专有的时序状态逐条验证已经遍历的原生前缀，复用待闭合工具组及已检查结果。在过去会触发前缀验证的阶段报告保存的首错，并保留解码、配对和 checkpoint 错误的既有优先级。验证被 checkpoint 覆盖的完整历史，实际原生内容 digest 仍从对应有效窗口生成。历史、transcript、footprint 及 Stop sample 投影复用同义的遍历和定位，各家保留投影白名单、phase 和原生计量。

### 收敛已核实的全仓重复

- **Session** 让 draft 与读盘记录复用同一组 27 种事件的严格 decoder 分派，保留 draft 专属 placement、父线程校验和错误顺序。writer 正常处理和关闭排空共用 batch 处理，维持 seq、Sync、poison、lease 和取消接纳边界。
- **Runtime 和 Hook** 复用 Patch、Write、Exec 的审批事实、拒绝结果与通知顺序，保留各工具的授权来源、grant、候选、预算及文案。合并 Cache/Context 查询的同义活动状态与关联回包。共享命令 Hook 的同义 runner 调用、限时扣减和结果身份检查，信任、持久化、重写及反馈仍按事件处理。Hook 输出解析仅共享一致的限额与解码骨架，遵守各事件对退出码、UTF-8、空对象与反馈授权的现有规则。
- **App、Runtime 与 TUI** 依照现有生命周期提取职责，例如 App 的工具依赖装配、Runtime 的 turn 内工具执行与结果状态、TUI 已有审批和上下文面板的键盘及回包处理。保持 AgentLoop 和 Bubble Tea 的单 owner。合并 Pre/Post Hook 的 transcript 缓存失效及列表重载，保留详情滚动、焦点、草稿和迟到回复规则。
- **Domain、Tool、Protocol 与 Headless** 共享现有 UUIDv7 算法，保留各类 ID 的类型、格式和错误。以内存小函数共享查询及压缩事件的装配，保持每个 kind 的构造、validator 和 strict decoder。两个 headless JSONL encoder 复用 canonical frame 的单行写入，沿用各自的类型、校验、错误及失败状态。
- **死代码** 复查未读的执行准入返回字段、Exec 进度赋值、重复返回分支和无调用方的 accessor。测试专用入口若搬入测试源，按迁移记录。当前契约要求的 typed constructor、validator 和 decoder 仍作为协议边界保留。

### 待单独评审的行为修复

本 change 以现有支持的调用顺序和故障行为为等价基线。以下问题按各自的行为契约另行评审与验证。

- Provider stream 与 prepared commit 的准入竞态、直接并发调用下的 checkpoint 窗口交错、OpenAI discard 时 pending 释放与连接关闭的竞态，以及流输出背压时的取消收口。[输入准备](../../../../internal/provider/native_prepare.go#L26) · [窗口读取](../../../../internal/provider/native_history.go#L55) · [OpenAI 丢弃采样](../../../../internal/provider/openai/provider.go#L247) · [流事件发布](../../../../internal/provider/sample_stream.go#L149)
- 在线原生回放的更及时取消、OpenAI 新增强制普通 sample 用量对账。[会话恢复](../../../../internal/app/session_service.go#L150) · [当前计量校验](../../../../internal/app/sample_usage_restore.go#L12)
- Stop 后晚到的工具审批仍可能形成会话授权，Patch 准备取消状态和等待审批取消来源与 Write/Exec 不一致。[AgentLoop](../../../../internal/runtime/agent_loop.go#L580) · [Patch](../../../../internal/runtime/patch.go#L12)
- Headless 最后错误帧在输出背压下的取消卡住。Session writer 在确定的写盘前失败后也会标记 poisoned。[Headless](../../../../internal/headless/stream_runner.go#L288) · [Writer](../../../../internal/session/writer.go#L234)

## Risks / Trade-offs

- **抽象扩大。** 只有语义和生命周期一致的流程共享。各操作保留可验证的强类型结果与证据。共享实现变更同时影响两家，因此每组都用两家实测。架构检查拒绝 Runtime/Context 依赖具体适配器、按具体家族选择行为或在运行时反复探测已绑定能力，同时允许合法身份校验、配置字段解析与文案。
- **同一源码重构造成用户行为漂移。** 在相同输入下对照基线的请求字节、原生顺序、缓存指纹、事件顺序、错误优先级、真实取消与 durable 状态。主规格与当前 fixture 作为证据；同一份回归继续覆盖预览、恢复、工具执行和终端交互。
- **性能改善无法从行数推导。** 对工具目录解码和多工具组长会话回放做同配置、同数据的耗时与分配对照，并验证 1× 与 2× 工具批次数及真实损坏输入。分别记录既有生产代码、净增减和迁移行数。数字用于评审，行为等价和维护性决定是否采纳实现。

## Migration Plan

开始时记录契约基线及现有 golden。按装配依赖、原生窗口与 staging、采样及摘要、回放与投影、全仓同义收敛、整体验收依次评审。内部契约与调用方在同批切换。每段用当前真实入口和故障回归完成验证后再进入下一段。数据继续使用唯一的当前形状，回滚可按实施阶段还原源码。

整体验证通过后，将本次架构约束同步到主规格，更新总体架构、ADR 0002 的当期说明及适用的路线图和踩坑记录。用户批准归档后按当前 OpenSpec 流程处理。
