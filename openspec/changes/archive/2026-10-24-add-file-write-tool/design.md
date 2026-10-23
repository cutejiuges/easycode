## Context

动机与范围见 [proposal.md](proposal.md)。调研基线是 EasyCode `afb21d6`；实施使用该基线的唯一当前契约，尚无 Exec 或会话列表分支合入。

当前 `builtin/patch.go`、`patch_unix.go` 已提供候选、文件摘要、同目录暂存、原子替换和目录 Sync；`runtime/runtime.go` 已有有序只读批次及 Patch exclusive 屏障。但它们分别假定目标已存在、输入是非空 replacement、approval 只有 Patch，不能把空 old_string 当成 Write。

`builtin/read.go` 会读取最多 16 MiB 原始内容再做行选择；`tool/result.go` 的模型 preview 有 2000 行、每行 2000 code points 和 256 KiB 预算。现有结果没有文件摘要或“完整内容已进入原生输出”的证明。当前 `commitToolOutputs` 在整个 call group 执行完成后才 durable/finalize，这是完整读取证据的关键生效边界。

参考源码来自本机 `/Users/cutejiuge/IdeaProjects/claude-code-sourcemap`（`a8a678c`，恢复源码标识为 Claude Code 2.1.88）和 `/Users/cutejiuge/IdeaProjects/codex`（`044da98b4`）。既有 Tool 架构文档引用的 Codex `c248f6d48b` 与本轮实际 checkout 不同；以下结论只对应本轮读取版本。sourcemap 是恢复代码，不代表官方公开源码契约。

| 证据位置（相对参考工程根） | 实际观察 | 本方案取舍 |
| --- | --- | --- |
| Claude `restored-src/src/tools/FileWriteTool/FileWriteTool.ts:45,151,225` | strict file_path/content；检查先前 Read、partial view 和时间戳；执行前重新读取，有完整内容比较回退 | 保留先读后覆盖和二次验证，改用 fd identity + 原始 bytes 摘要；不把 mtime 单独当作版本 |
| 同文件 `:272-310` 与 `utils/fileRead.ts:75` | 读取会归一化 CRLF；Write 最终尊重模型提供的换行，不再采样仓库猜测换行 | 输入的 UTF-8 bytes 原样写入；读取证据散列原始 bytes，不散列带行号/归一化 preview |
| Claude `components/permissions/FileWritePermissionRequest/FileWritePermissionRequest.tsx`、`FileWriteToolDiff.tsx` | 区分 Create/Overwrite；创建展示内容，覆盖展示 diff；支持 IDE 修改参数 | 采用两类展示；候选不可变，修改意见走 deny feedback 和新的调用；TUI 不在 render 中读盘 |
| Claude `tools/FileWriteTool/UI.tsx:27-150` | 创建结果是紧凑行数摘要，可展开正文 | 本切片提供精确审批和有界模型结果；完整通用工具时间线仍属 P5，不新增无恢复投影的工具 cell |
| Claude `utils/file.ts:362-470` | 解析并跟随 symlink；临时文件 rename 失败后可能回退到非原子写 | 不采用这两项行为；沿用 EasyCode 的 no-follow 和失败关闭 |
| Codex `codex-rs/core/src/tools/handlers/apply_patch.rs`、`tools/runtimes/apply_patch.rs`、`core/src/apply_patch.rs` | 解析、审批、安全执行、展示事件职责分开；保留 custom wire | 保留分层和原生 custom apply_patch；Write 由独立 typed input 路由，共用宿主控制面 |
| Codex `codex-rs/apply-patch/src/lib.rs:438-500,765-810` | Add 会读取可能被覆盖的内容再写入；缺失父目录会创建；失败 delta 可标为不精确 | 采用目录创建的产品能力，但 create 候选必须防覆盖；不把失败自动解释为无修改 |
| Codex `codex-rs/apply-patch/tests/suite/no_follow.rs` | 覆盖 leaf/ancestor symlink 和验证后替换；默认 API 与 no-follow 模式语义不同 | 采用确定性换链测试；EasyCode 始终 no-follow，不增加可切换的宽松模式 |

## Goals / Non-Goals

**Goals:**

- 一个共享 Write 实现为双 Provider 提供等价创建/覆盖，保留原生 item、调用顺序与恢复 bytes。
- 用户看到的操作、路径、目录清单和内容与实际提交的候选一致。
- Read 证据、用户授权、文件系统可执行性是三个独立条件，互不代替。
- 所有失败都能区分发布前未改目标、确定发布、结果不确定；取消不释放未完成的资源所有权。

**Non-Goals:**

- 不实现 Write 多文件批次、删除/移动、二进制/UTF-16 转码、chmod 工具或通用文件事务。
- 不提供针对任意外部进程的文件 compare-and-swap 隔离；已打开目录的能力与路径重命名保证沿用现有 Workspace 信任模型。
- 不添加全局读取缓存、文件 watcher、旧 Session 兼容 reader 或自动重试。
- 不复制 Claude 的 IDE/LSP/skills/history-backup 副作用，也不引入 Codex 的多环境执行/审批升级体系。

## Decisions

### 1. 两家提供 structured Write，保持现有 apply_patch

本提案采用 `fs.write`、Anthropic `Write` 与 OpenAI strict function `Write`，输入均为 required `file_path`、`content`。沿用当前 Read/Glob/Grep 的双 Provider 命名策略。OpenAI 的局部修改仍使用 custom freeform `apply_patch`，不改其 Update-only grammar。

用户已通过 apply 请求确认本提案，实施采用上述双 Provider structured Write 入口。

比较过的替代方案：

| 方案 | 优点 | 当前代价 |
| --- | --- | --- |
| 双 Provider Write + 原 apply_patch（采用） | 直接覆盖创建、空文件、完整覆盖；保持 facade 到 capability 一对一 | OpenAI 新增一个 function schema |
| 只给 apply_patch 增加 Add | 接近 Codex 原生入口 | 不能独立解决空文件重写；Add 覆盖语义需重定；当前 facade 需路由两个 capability |
| Write + apply_patch Add 同时提供 | 入口最丰富 | 两个创建入口、grammar/路由/恢复测试扩大，现阶段收益不足 |
| 用 shell 重定向写文件 | 无新增专用工具 | 依赖在途命令能力且绕开精确 diff、基线与文件提交协议 |

如评审选择同时提供 Add，需先修订本提案的 facade 映射、file-patch delta 和任务，不能在 apply 时临时加入。

### 2. 写入参数保持小，内容保持精确

路径只接受规范 workspace 相对路径，拒绝绝对路径、`.`/`..`、空组件、反斜杠、NUL 和控制字符，使用与 Patch 一致的安全路径约束；整个路径最多 4096 UTF-8 bytes、64 个组件。输入 JSON 和规范化输入均不超过 1 MiB，拒绝重复/未知字段、缺字段、null、尾随内容、非法 UTF-8 和无法无损解码的 Unicode 转义。content 可以为空，但不能有 NUL。

Write 不推测行尾或编码：CRLF、LF、BOM、末尾换行与否均按 content 原样写入。覆盖只支持已有合法 UTF-8 常规文件；保持现有文件大小上限 16 MiB。空文件是存在的文件，需要完整 Read；不存在且 content 为空则创建空文件。

当内容与已有 base 完全相同，在具备有效读取证据后返回 `error/write_no_changes`，不请求批准、不记录 started、不改权限或时间戳。采用现有 Patch 的无变化错误模式，避免为一次 no-op 放宽“success 必须有 started”的 ledger 契约。

### 3. 完整读取证据属于本次 Runtime，不属于 Session

新增小型强类型 `ReadExecution`，包含既有 `InvocationResult` 和封闭的 `ReadObservation`（complete/unavailable），替换 `ReadExecutor.Execute` 的单一当前返回契约，不引入可选第二接口。执行器从同一 fd 在读取前后检查身份、mode、size、mtime/ctime；检查一致、内容合法且模型 preview 从第一行到 EOF 无任何截断时，才提供 complete observation。

证据只携带规范路径、invocation identity、原始 bytes SHA-256、size、mode、device/inode，不携带文件正文或绝对路径。mtime/ctime 用于读取过程的一致性检查，不作为批准指纹的单独版本，也不因单纯 touch 而否定内容和身份完全相同的证据。旧 `PatchBaseEvidence` 可重命名为当前共享 `FileBaseEvidence` 并统一调用方，不保留 alias/Legacy 类型。

Runtime 单 owner 将观察结果先保存在 sample 的固定 call slot。只有对应 results 与整组 native outputs 均成功 Sync/finalize，证据才可进入实例内有界表，供后续 sample/turn 使用。表最多 1024 个路径，按最近一次成功完整 Read 的提交顺序淘汰；不用时间或 goroutine 完成顺序排序。它不从 Session/native history/带行号 preview 重建，resume 后为空。

同一 sample 的 `Read(a) -> Write(a)` 若没有更早有效证据，Write 返回 `write_requires_full_read`。模型尚未看到这个 Read 的返回，不能将其当作覆盖依据。分段多次读取不拼装“完整读取”；长行截断或超过 256 KiB 的文件仍可局部 Patch，但不能冒充满足整文件覆盖前置条件。

文件写入准入会使该路径的已有及待生效观察失效。sample 内使用路径 generation 防止 `Read(a) -> Patch(a)` 的旧 Read 在整组 outputs 提交后重新生效；后置 `Read(a)` 才能带新 generation 生效。Patch/Write 一经 accepted started 就保守失效；失败/截断的新 Read 或基线冲突也失效该路径。Write 成功不自动制造 Read 证据，再次整文件覆盖需要重新 Read。若集成时 Exec 已合并，在其实际 accepted started 边界清空全表和全部 pending observations，因为命令可能修改任意 workspace 文件；不解析 shell 文本推测修改范围。

证据有效只说明完整内容已进入后续模型请求的原生上下文，不宣称可证明模型在推理中理解了文件。

### 4. 候选冻结 create/overwrite，不能临时转换

`WritePreparer.Prepare` 在 ready durable 后只读地打开已有目录链/目标，生成 `WriteCandidate`。覆盖必须同时匹配生效的 Read 证据；创建记录首个缺失组件及已有目录前缀的 fd identity。打开返回的错误只有明确 ENOENT 能解释为缺失；权限错误、symlink、FIFO、设备或目录不能被当成新文件。

候选包含 typed operation、规范路径、文件/目录 evidence、缺失目录清单、final bytes、完整预览、line/byte summary 和 fingerprint。fingerprint 对 capability、operation、路径、base/ancestor evidence、目录清单、权限计划和 final digest 做确定性编码；不包含随机 ID、UI、时间或 Provider wire。批准后目标从无到有、从有到无、换 inode、改内容、改权限或已有祖先换身份均冲突，不将 create 升级为 overwrite，也不重新计算后静默沿用批准。

路径最深 64 组件，其中一次最多创建 16 层缺失目录。new file 为 0600，new directory 为 0700，不更改进程全局 umask。覆盖保留原文件 0777 基本权限位；拒绝特殊权限位，明确不承诺 inode、hardlink 关系、owner、ACL/xattr 等完整元数据克隆。硬链接其他名字不被修改；这与原子替换一个路径的语义一致。

### 5. 批准预览完整且与操作绑定

create 展示完整新内容（可用全新增 diff 表达）、字节/行数以及将创建的目录；overwrite 展示所有变化的 unified diff。总 presentation 上限 256 KiB，超限为 `write_preview_too_large`，不截断后询问。内容可为空，此时显示明确的空文件说明；存在性用 typed operation 表示，不能用 oldContent 是否为空推断。

预览中的 ESC、CR、其他控制字符和双向控制符需确定性可见转义；行尾变化、无末尾换行和 BOM 也应可辨别。转义只用于展示，不改 final bytes；转义后预算仍须满足。TUI 渲染期间零 I/O，窗口变化只滚动同一候选。

approval payload 使用封闭 presentation kind：现有 patch、write-create、write-overwrite；各分支具有专属构造、strict decoder 和 validator，拒绝缺失/混合字段。若 Exec 已合并，在它的 command presentation 联合上添加实际 Write 分支，不另建一套审批协议。低层 protocol 不导入 tool，Runtime 显式映射强类型值。

Write 选择为 Yes、仅本 Runtime 的 Write session allow、No（默认）；标签明确“create and overwrite files”，不能继续用含糊的 all edits 标签。Write grant 与 Patch grant 独立，不能互相授权，也不传播给 Exec。grant 仅跳过询问，不能跳过完整 Read、候选、预览预算、安全重验和每次 durable allow fact。拒绝反馈仍限制 16 KiB。

本切片不新增通用工具时间线或额外完成事件；审批决议不能宣称文件已写成功。模型结果由原生 tool output 返回，最终文字继续由既有 assistant transcript 呈现。

### 6. 创建和覆盖采用不同的提交原语

```text
ready + sample durable
        |
        v
read evidence + candidate + exact preview
        |
        v
approval durable -> started durable
        |
        v
reopen + revalidate -> private staging -> Sync
        |
        +-- create ----> publish without replacement
        |
        +-- overwrite -> revalidate base -> atomic replace
                                      |
                                      v
                           Sync parent + close resources
                                      |
                                      v
                         result durable -> native output
```

已有父目录下新建文件：同目录 O_EXCL/no-follow 私有临时文件写完并 Sync，使用禁止覆盖的原子 rename 发布到目标名。覆盖：安全重开当前目标、核验完整 bytes/identity/mode，与 Patch 一样在提交前再次核验，再原子替换。不能把普通 rename 用作 create，因为普通 rename 允许替换既有目录项。

平台选择：Darwin `RenameatxNp(..., RENAME_EXCL)`，Linux `Renameat2(..., RENAME_NOREPLACE)`；仓库已有 `golang.org/x/sys v0.41.0` 提供两者。Linux man-pages 明确 no-replace 需要文件系统支持，Apple 也将 exclusive rename 作为卷能力：分别参见 [rename(2)](https://man7.org/linux/man-pages/man2/rename.2.html) 和 [Apple volumeSupportsExclusiveRenaming](https://developer.apple.com/documentation/foundation/urlresourcevalues/volumesupportsexclusiverenaming)。这些是设计依据，尚未替代本变更真实平台测试。ENOSYS/不支持 flag 等明确 unsupported 结果不能回退为路径 Exists 检查加普通 rename。

如路径为 `src/new/pkg/a.go` 且只有 `src` 存在，在 `src` 的安全 fd 下构造私有暂存树，内容为 `pkg/a.go`，暂存根对应将来的 `new`。写文件并 Sync，再从叶到根 Sync 暂存目录，最后将暂存根 no-replace rename 为 `new`，再 Sync `src`。用户路径不会在提交前露出半条新目录链；若别的进程先创建 `new`（即使为空目录）则整个候选冲突，不复用它或合并进去。

这是“单文件及其必需目录链”的一次发布，不是多文件事务。选择私有树整体发布而非逐级创建正式目录，是为了让取消/失败清理只处理 owner 的暂存资源，不误删用户在正式目录中新增的对象。

### 7. 线性化点、清理和失败分类

`tool_execution_started` 的 accepted durable admission 是 Runtime 交付执行权的边界；rename 成功是文件/目录树发布边界，两者不同。started 前观察到取消，executor 零调用；accepted started 后 executor 必须被调用一次，入口已取消则零 staging/目标写入并返回 cancelled。

| 失败位置 | 目标状态 | 结果与后续 |
| --- | --- | --- |
| 候选/完整读取证据/预览失败 | 未改 | 固定 code 的 error/conflict，不批准、不 started |
| 用户拒绝/headless ask | 未改 | denied；deny 与 result 同 batch |
| started 后但发布前取消、写入或文件 Sync 失败 | 目标未改 | 清理自己暂存项；清理确定成功后 error/cancelled |
| no-replace 目标已存在、执行前基线变化 | 外部内容保留 | conflict；不重试、不把操作转换为覆盖 |
| rename 已成功，随后 context 取消 | 已发布 | 继续 Sync 和 cleanup；它们成功则保存 success，turn 可单独以取消终止 |
| rename/Sync 返回无法证明未提交的 I/O 错误，或发布后目录 Sync 失败 | 不能安全断言持久状态 | outcome_uncertain，poison Runtime，停止后续副作用 |
| 暂存清理/Close 无法确认完成 | 可能留有暂存资源 | outcome_uncertain，保留唯一清理责任直到最后资源关闭，不自动重跑 |
| result/output 写 Session 失败 | 文件可能已成功提交 | poison；resume 按 ledger 补偿，绝不重写文件 |

只把平台明确保证“未提交”的错误归为安全失败；网络/异常文件系统上的 EIO 等不能仅因 syscall 返回 error 就宣称目标未修改。成功发布后忽略 context 对持久化与清理的取消，返回确定结果；这比当前 Patch 将提交后取消统一视作 uncertain 更精确，本次只定义 Write 行为，不顺带修改 Patch。文件结果 uncertain 只阻止后续工具/Provider 副作用；journal 健康时仍保存结果与配对 output，journal 自身不健康才留待恢复补偿。

唯一 staging owner 持有 fd、暂存 identity 和 created-entry 清单；正常失败时按句柄与已知清单清理，不调用基于模型路径的递归 RemoveAll、不遍历未知子项。清理发现未知项或身份冲突则失败关闭。锁内不进行 I/O、Sync、Close 或等待；不新增隐式 goroutine。

进程被杀死时可能残留权限受控的暂存文件/树。resume 保持零 workspace I/O，不扫描、删除或完成这些项；started 无 result 永远收口为 uncertain。未来自动垃圾回收必须有独立 ownership 设计，不能按文件名前缀删除。本次不承诺 SIGKILL 后零孤儿暂存项。

### 8. native、Session 和缓存的边界

Anthropic 保留 Write 的 tool_use/input 与 tool_result；OpenAI 保留 function_call/arguments 与 function_call_output；现有 apply_patch 继续是 custom_tool_call/custom_tool_call_output。Read observation、候选 evidence、approval presentation 和 grant 不成为原生 item 或稳定 prompt。

当前 Session ready 需要完整 typed WriteInput，其中 content 就是模型请求写入的内容，因此旧 spec 中“工具 records 不保存完整 final 文件”必须精确修订为：允许必要的 Write 参数，不额外保存 executor 读取的完整 base、候选快照、diff 或暂存资源路径。只保存指纹而删除参数会破坏 ready/native 验证；额外引入 artifact store 则越出本切片。

结果包含相对路径、已知 create/overwrite 操作或 unavailable、写入字节/行数、创建目录数、typed status/code 和冻结的紧凑英文 preview；成功不回显完整 content。不可确定操作的恢复/准备失败结果必须有显式 unavailable 分支，不能通过恢复时读盘补齐。record 和 Provider sample 总大小沿用现有 16 MiB 边界；多调用聚合超限在 I/O 前失败，不放宽全局上限。

Read 证据只通过 ReadExecution 进入 Runtime，既有 Read result/Provider preview 保持字节一致；不增加可选 Session observation record。Write 加入当前 input/result 联合后更新唯一 current fixture/canary，strict decode 旧开发 shape 失败关闭。恢复保留 ready/approval/started/result/output 的原有四类 ledger 词汇，只扩展当前 payload 分支。

稳定 schema/description 中加入 Write 会改变 catalog segment 和稳定前缀 fingerprint，这属于真实能力变更。文件证据、目标是否存在、目录创建清单、grant 和 UI 绝不能改变 catalog；相同原生调用/冻结输出下，uninterrupted 与 restored 的下一请求 canonical bytes、native 顺序和 fingerprint 必须一致。该等价性不意味着恢复后沿用已经丢弃的文件读取证据。

### 9. 代码边界与在途变更集成

| 边界 | 本次职责 | 应复用/避免 |
| --- | --- | --- |
| tool | Write input/invocation/candidate/result、ReadExecution/evidence、policy、catalog | 封闭类型；不放磁盘调用和 TUI widget |
| tool/builtin | descriptor 打开、候选读取、diff、staging owner、平台提交 adapter | 可提取小型文本 diff/文件证据逻辑；保留 Patch 与 Write 的独立语义 |
| runtime | 证据生命周期、Write 调度、approval、durable 编排 | 将 Write 与 observation 逻辑放专属文件，避免继续扩大 runtime.go |
| protocol/tui | 创建/覆盖 presentation 的映射、验证、纯显示与输入路由 | 不解析 native history，不从 UI 读取文件或判断授权范围 |
| session/provider | 当前 typed codecs、严格 replay 与原生配对 | 不保存 observation，不恢复执行器，不反向从 UI 重建请求 |
| app | 显式装配同一 Workspace 与真实执行组件 | New 纯内存；资源由已有 app owner 关闭 |

`feat/session-list-command` 修改 catalog 查询与 CLI 命令，与 Write 无功能交集；只需在最终 README/Roadmap 合并时保留双方描述。`17ad2/easycode` 的 `add-command-execution` 修改 catalog、typed unions、approval、Runtime exclusive 分派、Session canary/fixtures、Provider golden 和 TUI；这些是必须承认的共同接入点。

本变更不依赖、不复制、不修改该工作区。实施以届时目标分支为唯一当前契约：若 Exec 已合并，保留真实 Exec 分支并在其上增加 Write、接入命令准入时的 observation 失效；若未合并，只实现当前真实五种能力。不得创建 with-exec/without-exec 双 catalog 或兼容 reader。合并时重新生成最终 catalog golden 和一次性的 current fixture，不能机械采用任一侧旧 hash/canary。最终混合调度测试覆盖实际存在的全部 exclusive 能力；未实现能力不得提前加入可执行接口。

## Risks / Trade-offs

- [完整 Read 预算限制可覆盖文件范围] → 明确反馈 `write_requires_full_read`，保留局部 Edit/apply_patch；不扩大 Read 输出或虚构多片段覆盖证据。
- [候选审批期间目标变化] → 在准备、执行重开和发布前核验；内容相同但身份不同也冲突。检查与 rename 之间仍不构成外部进程事务锁，不声称绝对 CAS。
- [新目录树发布依赖文件系统原子能力] → Darwin/Linux 各自真实测试；不支持时明确失败；不以检查后普通 rename 替代。
- [文件元数据保留范围小于完整复制] → 文档只承诺基本权限位；若产品需要 ACL/xattr/owner 保持，先扩展设计和平台验证，不在 apply 时隐式添加或承诺。
- [目录树、暂存清理扩大故障面] → 创建深度有界、owner 清单可测试；任何不确定结果停止副作用；SIGKILL 残留列为明确限制。
- [Write 参数可能含用户代码] → 只进入权限受控的原生历史/必要 ledger，不进入诊断；测试使用合成数据，凭据不进入任何 fixture。
- [公共契约与 Exec 并行冲突] → 实施前读取最终主线事实，只维护一个当前联合；最终恢复、approval、catalog/cache 回归一起验证。

## Migration Plan

1. 评审本提案，尤其是双 Provider Write 入口、先前 sample 的完整读取要求、私有目录树创建和基本权限位边界；用户已通过明确 apply 请求确认。
2. 按 tasks 先落地当前强类型 input/observation/result，再完成纯逻辑测试和安全候选/提交边界；不可先公开半成品 schema。
3. 接入 Runtime、双 Provider、Session 与 TUI，替换唯一 current fixture；Read 对模型的 bytes 和既有 Patch 行为保持回归。
4. 更新 Tool 架构、总体架构、Roadmap、pitfall、README 和实际测试路径。归档前检查 record vocabulary，不能把独立在途能力写成已完成。
5. 执行 OpenSpec 严格验证、平台/故障注入测试和 `make verify`。若无法兑现 no-replace、安全路径或恢复等价，暂停 apply 并修订 artifacts，不降级到直接写入。

回退以整项变更为单位恢复当前契约及 fixture，不能留下暴露 Write schema 却没有执行/恢复链路的部分实现，也不能通过追加旧 reader 来容纳开发期数据。
