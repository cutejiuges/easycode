## Context

动机和用户确认范围见 [proposal.md](proposal.md)。本设计于 2026-11-04 基于当前 checkout 制定，当前无其他活动 change；探索阶段的 `make verify` 已通过，但不代表本变更验收完成。

### 现有接缝

- `internal/app/session_list.go:loadSessionList` 显式打开 Repository/Catalog、协调、查询并关闭资源；`catalog.List` 支持当前目录/全部目录，最多 200 条，排序为 durable 更新时间与 thread ID 双降序。协调扫描全存储，返回数量不是扫描上限。
- `internal/app/session_service.go:resume` 在同一 lease 下 Load、Replay、Provider 恢复、Repair、writer transfer 与中断补偿；`app.go:openChatResourcesWithSnapshot` 后续还装配 artifact、工具和 Runtime。因此恢复错误不全都发生在 journal 修改前。
- `internal/app/compaction_restore.go:orderedRestoreRecords` 按 journal seq 插入 source/checkpoint，同时保留原生 ordinal；`validateCompactionUsage` 另校验压缩用量。预览必须复用这些规则。
- 两家 `RestoreConversation` 当前依附 Provider 实例，构造 Provider 需要连接配置；`compactionSource` 通过 Provider 读取 model。两家 `history.go:projectHistory` 已是纯内存单向文本投影。
- `internal/app/transcript_owner.go` 的查询 owner 面向已活动 thread 并包含工具/artifact 依赖，不适合作为未恢复会话的离线预览入口。
- `runTUI` 当前直接创建 Chat 模型并启动 Bubble Tea。需要提取已有 Chat 装配步骤供启动壳组合，不复制第二套 Chat 事件处理。

### 参考证据和取舍

参考根均位于主 checkout 的相邻目录 `/Users/cutejiuge/IdeaProjects/`，只读使用：

| 参考 | 已检查的文件 | 采用与限制 |
|---|---|---|
| Claude Code sourcemap 2.1.88 | `claude-code-sourcemap/restored-src/src/components/LogSelector.tsx`、`SessionPreview.tsx` | 输入搜索、Ctrl+A 范围、Ctrl+V 显式预览和分层 Esc；不照搬编译产物结构、全局状态或实验开关 |
| Claude Code 2.1.88 | `src/screens/ResumeConversation.tsx`、`src/commands/resume/resume.tsx`、`src/utils/crossProjectResume.ts`（均相对 restored-src） | 浏览与完整恢复分离；不复制剪贴板、生成 cd 命令或受开关控制的 worktree 行为 |
| Codex `6f8bf683c` | `codex-rs/tui/src/resume_picker.rs`、`resume_picker/catalog.rs`、`resume_picker/page_loading.rs` | 请求身份、过期结果隔离、强类型选择；不引入远端 backend、多源 catalog 或分页框架 |
| Codex 同提交 | `resume_picker/catalog_tests.rs`、`resume_picker_pagination_error_tests.rs` | 旧结果不能结束新请求，加载失败保留已显示状态；不复制取消后 StartFresh 的行为 |

### 已发现的文档冲突

`cmd/easycode/sessions.go` 帮助仍声称列表会 repair；实际 Catalog 仅投影 committed 前缀。`session/resume` 旧段落仍提及 schema revision，`session/catalog` 旧段落称 schema 显式版本，与当前单契约基线、实际结构及后续场景冲突。本 change 不据此增加版本字段；实现阶段修正入口帮助，归档核对时报告并单独收敛未纳入本 change 的历史措辞，不静默扩大为格式治理重构。

## Goals / Non-Goals

**Goals:**

- 用元数据选择与显式只读预览辨认会话；正式恢复继续以 journal/native facts 为准。
- 将长期存储、临时 UI、异步操作和活动聊天资源的所有权分开，所有退出都可验证清理完成。
- 抽取两家原生恢复中真正共享的纯内存校验，保持 Provider 差异、native 顺序和缓存语义。

**Non-Goals:**

- 不在已有活动聊天中切换 Runtime，不将预览对象升级为活动 Conversation。
- 不提供全历史搜索、正文检索、工具详情预览或自动标题，不添加索引字段或 Session record。
- 不将 32 KiB 展示预算误称为 journal 加载/校验的内存预算，不重构现有 Loader 为随机访问 reader。

## Decisions

### 1. 入口、配置与兼容性

新增 `easycode resume [--all] [--config PATH]` 独立路由。帮助和参数错误先于任何资源操作；要求 stdin/stdout 均为终端，非交互输入返回用法错误，不自动读取 `/dev/tty`。只有首参数 `resume` 进入新路由，`--print "resume"` 保持 prompt 语义。

该入口最终要进入 Chat，采用与 Chat 一致的配置加载和 Provider 配置验证，在进入 UI 前冻结一次配置及启动 cwd；配置无效返回安全错误。独立无配置发现仍用 `sessions list`。项目指令在正式恢复装配时从冻结的启动 cwd 读取一次，预览不加载项目指令。全列表保留不兼容条目，使用明确字段差异提示；预览使用 journal 中已验证的 Provider/wire/model，不能使用当前模型替换其来源。

备选：让裸 `--resume` 变为可选参数会改变已存在的缺参数契约，故不采用。自动切换模型/base URL 无可靠配置来源，也不采用。

### 2. 列表、搜索与屏幕行为

每个范围最多查询 200 条；排序和精确 cwd 匹配完全复用 Catalog。首轮及 Ctrl+R 显式刷新执行 reconciliation；切范围执行受 owner 串行管理的查询，不在每次键入时扫描磁盘。Catalog 连接只由 owner 操作，不从 View/Update 直接访问。owner 只有在首次协调成功后才允许查询；首次协调被切范围取消时，最新意图必须重新完成协调，不能直接查询未初始化索引。协调警告随最近成功刷新保存，不因切范围被错误清空。

搜索仅作用于当前已加载原始元数据的 ID、family、wire、model、cwd；采用 Go Unicode 小写转换后字面子串匹配，无正则、不 trim 存储值，查询输入最多 256 个 Unicode code point。decoded paste 将 CR/LF/Tab 归一为空格，超限整体拒绝，退格按字素边界删除。不能用已经截断或转义的显示值匹配。预览正文不加入搜索集合。

上下键移动、PageUp/PageDown 翻屏，普通字符输入搜索，Ctrl+A 切范围，Ctrl+V 预览，Ctrl+R 刷新，Enter 恢复。Esc 优先关闭预览/跨目录确认，其次清空搜索，最后取消启动；Ctrl+C 直接请求退出。空结果停留界面，允许切范围/刷新/退出。

搜索后保留仍可见的选中 ID，否则选首项；刷新按 ID 而非索引保持选择。列表可压缩身份展示，所选详情保留完整 ID；Tab 打开可滚动详情，极窄屏允许换行/滚动，不将截断字符串作为目标。时间用当前机器本地时区的固定日期时间并显示时区，无定时相对时间刷新。model/wire、cwd 的安全展示限制分别为 256、512 个 code point，控制字符转义后再布局；不输出原始终端控制序列。

`HasMore` 为 true 时显示仅搜索最近 200 条；空搜索匹配与空目录分别提示。busy/invalid 是全存储协调统计，不显示推测的逐行 live busy 状态，不声称列表完整或可立即恢复。

### 3. 按需预览的独立读路径

预览在 app 增加独立函数/服务，显式打开 Repository，以 thread ID 获取 descriptor-bound lease，Load、Replay、验证 root 身份，调用 `orderedRestoreRecords` 与离线 Provider 校验投影。预览不经过 `sessionService.resume`，不取得 writer，不装配 workspace/artifact/Runtime。

完整校验先于结果暴露，失败不返回部分成功正文。尾部可修复时只展示 committed 前缀并标记 incomplete；工具中断和 input-only 尾部不合成 output 或终态。持有 lease 直到当前加载与投影完成，关闭 lease/Repository 成功后再发布有界数据；显示期间无 lease。每次 Ctrl+V 重新读取，不保留跨预览缓存，关闭预览即丢弃正文。

显示按原始顺序保留最多前 128 条非空用户/assistant 消息，正文总量最多 32768 UTF-8 bytes；最后一条必要时在 UTF-8 边界截断，标识省略及该条被截断。空消息不占条目额度；无可见正文显示空预览。正文的安全渲染副本还须受不超过 196608 bytes 的总输出上限约束，超限明确省略，防止控制字符转义扩大。role/turn/InputID 均来自投影，不伪造摘要 turn。正文仅保留在当前界面，不写 Catalog、日志、遥测或新 artifact。

备选：首条 prompt 持久化到 Catalog 会改变已明确禁止正文索引的契约；自动预览每一行会频繁竞争 lease；直接读取 JSON 字段会绕过 Provider 原生语义，均不采用。

### 4. 离线 Provider 投影与共享验证

在 `internal/provider` 定义最小的只读投影契约，输入为强类型来源（thread、family、wire、model）和有序 `[]RestoreRecord`，输出为 `domain.SemanticHistoryView`；关联压缩用量证据通过同一离线 reader 的既有强类型 validator 接口验证，不能假定 RestoreRecord 包含所有 Session 用量证据。app 完成两部分校验后才发布预览。可取消方法以 `context.Context` 为首参，每个 record/checkpoint 之间及返回前检查取消；单次 strict decode 仍受既有原生 payload 上限约束。构造器纯内存，不接受 API key/base URL，不返回可 Stream 的 Conversation。

两家分别抽取内部 `restoreNativeHistory` 策略：普通 role、InputID 关联、native pairing、source revision/fingerprint、checkpoint 来源/去重与完整 history 校验全部共享。`compactionSource` 改为复用接收 model 值和原生快照的纯函数，禁止为了复用方法创建空 transport Provider。正式 `RestoreConversation` 在纯验证完成后将历史装入有连接的 Conversation；离线投影只调用同一验证策略与 `projectHistory`。

压缩 usage/provenance 校验也必须复用现有纯逻辑：app 将 `validateCompactionUsage` 依赖收窄到实际 validator 接口，联网 Provider 与离线 reader 委托相同实现。预览不能只校验 source 而跳过 checkpoint/failed-compaction 用量关联。

两家 reader 由 app 按受支持 family/wire 显式 switch 装配；不新增全局 registry，不让 session 或 domain 依赖具体 Provider。该接口有预览真实消费者；不扩张 Provider capabilities，也不增加未来空策略。

### 5. 单一终端与组合模型

```text
cmd resume --> app startup owner --> repository/catalog
                       |          --> offline provider projector
                       |          --> existing resume assembly
                       v
              one Bubble Tea Program
              picker --> resuming --> existing chat model
```

在 TUI 内新增独立 picker 与薄启动模型，组合现有 Chat Model。app 注入窄的强类型操作端口；DTO 不暴露 Catalog 的 journal path/checksum，也不携带秘密。app 持有资源，UI 只拿投影、操作身份和已有 Chat facade。typed 结果包含成功/失败/取消的封闭状态，不能用字符串错误匹配控制流；框架 `tea.Msg` 的接口值不传播进核心 record/Provider 契约。

app 从 `runTUI` 提取 Chat 装配复用点。进入 Chat 后既有输入路由、approval、context/cache panel、transcript 和 shutdown 全部沿用原路径；新启动壳不另写 AgentLoop。Help/错误在启动程序前输出；程序运行期间错误进入模型，退出后再按 Outcome 输出必要安全诊断。

### 6. owner、异步身份与取消

app 的启动 owner 显式 Start/Run，最多一个存储/恢复操作在途。每次操作具有单调 request ID；列表还有 generation/scope，预览还绑定 thread ID。快速切范围只保留最新待处理意图，取消旧操作并等待其清理结束后执行，不创建无界 goroutine/队列。键盘输入仍立即更新 UI。

owner 持有 cancel、唯一最终结果和 done；关闭先停止 admission，再取消在途查询并等待结束，最后关闭 Catalog/Repository。锁只做有界内存转换，I/O、等待、Close 和结果发送在锁外。tea.Cmd 只桥接操作请求/消息，应用 I/O 的完成责任属于 owner，不依赖 Bubble Tea 自动等待任意 Cmd。

过期成功/错误均不得改变当前状态；但带聊天资源的迟到成功不能简单丢弃，必须由 app owner 关闭。所有资源都要经过显式接收确认再转移给 Chat owner。退出、输出失败、父 context 取消都走相同清理路径。超过现有 shutdown 告警时限后保持所有权与最终 done，取消下游、显示等待/失败状态，不用 helper goroutine 或超时 return 遗弃句柄。

### 7. 正式恢复、跨目录与失败阶段

列表 Enter 先检查当前配置三元组；不匹配仅显示差异。跨目录显示 creation cwd 和实际工作目录，并提示当前项目指令/工具 root，默认选返回列表，只有用户选择继续才提交恢复。cwd 不变，不生成 shell 命令或复制剪贴板。

恢复请求在 owner admission 时固定 thread ID，先排空预览/查询，再走原 `openChatResourcesWithSnapshot` 路径。实际恢复重新读取 journal，重新校验 metadata/native/source；列表 compatibility 只是提示。整个恢复使用同一连续 lease 到 writer Close。

需要以 app 私有强类型结果表达恢复阶段，而不是从错误 code 或文本猜测是否修改过事实：

| 结果 | UI 与资源处理 |
|---|---|
| 在可能修改 journal 前拒绝且清理成功 | 返回原列表，保留搜索与选中项，显示安全错误，允许用户显式重试 |
| 开始执行 Repair/补偿后的失败 | 保守视为可能已有合法持久化变化；结束本次启动，说明未完成，用户下一次显式恢复重新读取 |
| Sync 不确定、poisoned 或任何清理失败 | 结束启动，不发布可交互 Chat，不自动重试，不宣称 bytes 未变 |
| 恢复成功且未取消 | 转移资源给 Chat owner，不发起自动 Provider sample |
| 恢复成功但取消已被接收 | app 关闭成功结果所持资源，等待清理，不启动 Chat |

取消在 admission 前不启动恢复。admission 后只能请求取消并等待既有 durable 语义给出确定结果，不能声称撤销已提交 repair/compensation。Esc/Ctrl+C 在 Resuming 阶段表示取消整个启动流程，不回到可再次提交的列表。反复 Enter 在 Resuming 状态忽略。正常 Esc 取消浏览退出 0；信号/Ctrl+C 取消退出 130；参数/非终端为 2；需要结束启动的存储/配置/清理错误为 1。已经接收的恢复被 Esc 取消，清理成功且结果确定时返回 130；若结果不确定或清理失败则返回 1。

### 8. 验证、文档与实施顺序

先实现离线共享校验及纯投影，再建立只读预览与 owner，之后接入组合 UI 和正式恢复。测试使用受控 channel/barrier 注入查询完成、取消、目标替换和资源接收竞态，不以 sleep 证明正确性。

新增测试预计放在 `internal/provider/{openai,anthropic}/offline_history_test.go`、`internal/app/session_preview_test.go`、`internal/app/session_picker_test.go`、`internal/app/session_picker_process_test.go`、`internal/tui/session_picker_test.go`、`internal/tui/session_picker_terminal_test.go`、`cmd/easycode/resume_test.go`；相应 golden 固定入库。文件名在实现中可调整，但验收文档必须引用最终存在的路径。

双 Provider 矩阵覆盖 reasoning opaque、同轮输入、工具配对、中断尾部、手动/自动压缩与 invalid source/usage；对 shared restore 重构验证 uninterrupted/restored 请求 canonical bytes、原生 item 顺序、内容/策略 fingerprint，遵循现有 restored transport reset 语义，不比较本来不同的瞬态连接状态。

人工 smoke 至少覆盖支持平台上的默认/无色终端、resize、预览返回、跨目录确认、Ctrl+C 与正常退出后的终端模式恢复。自动化通过不冒充完成真实终端检查。更新 README、架构对应章节、Roadmap 状态与踩坑记录，必要时为长期离线投影边界补 ADR；不得把整个 P5 标为完成。

## Risks / Trade-offs

- 全存储协调与完整 native 校验可能较慢 → 加载态、串行可取消 owner、显式刷新；200 条和 32 KiB 都不是读取成本上限，不承诺本切片实现增量索引。
- 提取 native restore 容易漏掉 checkpoint/source/model 依赖 → 同一私有验证实现供两入口调用，golden、恶意 fixture 和 request/cache 等价测试同时验收。
- 预览正文属于用户对话，可能含用户自行输入的机密 → 仅显式预览展示，不扩大到索引/日志/遥测；不宣称能识别或删除用户正文中所有机密。
- 预览读 lease 短暂阻止另一个恢复进程 → 单次操作、取消后释放、展示前关闭；不在用户浏览时持锁。
- UI 换屏时输入可能误路由 → 单 Program，Resuming 阶段禁止 composer 提交，使用真实输入管道与连续按键回归验证状态切换。
- metadata-only 搜索不能定位旧正文 → 显式标注字段与 200 条范围，用户可查看最近会话预览；全历史正文检索另经提案。

## Migration Plan

没有 Session/SQLite 格式迁移，不新增 schema/payload version 或历史 reader。上线仅增加入口与纯投影能力，既有 journal 保持当前唯一格式。回退实现代码不需要改写数据；已由正式恢复合法提交的 repair/compensation 保留，禁止为回退重写 append-only records。规划完成后先评审，再由后续明确 apply 请求实现，验收通过后同步本 change 的四份 delta specs 并归档。
