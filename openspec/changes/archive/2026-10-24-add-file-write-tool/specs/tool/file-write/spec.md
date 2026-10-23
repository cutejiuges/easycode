## Purpose

定义双 Provider 共用的单文件创建与整文件覆盖能力，使模型只能在工作区安全边界内、具备充分读取证据并获得精确候选授权后发布完整文本，同时明确目录创建、冲突、取消、持久化失败和恢复不重放的行为。

## ADDED Requirements

### Requirement: Write exposes one strict structured capability to both Providers

当前 catalog SHALL 向 Anthropic 与 OpenAI 各暴露一个名为 `Write` 的 structured facade，映射唯一共享 `fs.write`；两者 SHALL 具有相同输入语义，分别保留 tool_use/tool_result 和 function_call/function_call_output。既有 OpenAI custom apply_patch SHALL 保持独立的 Update-only 语义。

Write MUST 要求且只接受字符串 `file_path` 与 `content`。JSON bytes 和规范化参数均不得超过 1 MiB；路径为规范工作区相对路径，最多 4096 UTF-8 bytes、64 个组件，禁止绝对路径、`.`/`..`、空组件、反斜杠及控制字符。content MUST 是合法 UTF-8、无 NUL，可为空；重复字段、缺字段、null、未知字段、非法或无法无损解码的 Unicode 转义及尾随 JSON MUST 在 ready/I/O 前拒绝。

#### Scenario: Decode matching Write calls
- **WHEN** 两家完成同一合法路径和 content 的 Write 调用
- **THEN** 两者产生等价的 typed Write 输入及各自原生调用身份

#### Scenario: Reject malformed Write input
- **WHEN** 输入有重复 content、null、缺字段、额外字段、孤立代理项转义、越界路径或超出预算
- **THEN** sample 失败且不存在 Write ready、approval 或 workspace I/O

#### Scenario: Keep custom apply_patch independent
- **WHEN** OpenAI 请求一次 Write 和一次合法 Update-only apply_patch
- **THEN** 分别形成 structured Write 与 custom Patch 调用，不按名称猜测或转换 wire kind

### Requirement: Write preserves exact requested text and distinguishes empty files

Write SHALL 原样写入解码后的 content bytes，不推测或改写 CRLF/LF、BOM、末尾换行与编码。覆盖对象 MUST 是不超过 16 MiB 的可读写常规 UTF-8 文件；NUL、非法编码、目录、symlink 和特殊文件 MUST 失败关闭。

create/overwrite MUST 由经安全读取取得的存在性确定，不能按旧内容是否为空推断。覆盖现有文件始终需要完整 Read 证据；有效证据下 content 与 base 相同时 SHALL 返回 `error/write_no_changes`，无 approval、started 或文件修改。

#### Scenario: Create an empty file
- **WHEN** 目标不存在且合法 content 为空，候选获批并提交
- **THEN** 创建零字节文件，结果 operation 为 create

#### Scenario: Overwrite an existing empty file
- **WHEN** 目标已存在且为空，模型提供非空 content
- **THEN** 请求按 overwrite 处理并需要完整 Read，不能绕过读取或覆盖审批

#### Scenario: Preserve exact line endings
- **WHEN** content 包含 CRLF、LF、合法 BOM 或没有末尾换行
- **THEN** 成功文件 bytes 与解码 content 完全一致

#### Scenario: Avoid a no-op write
- **WHEN** 有效完整 Read 与当前 base 一致且新 content 完全相同
- **THEN** 返回 write_no_changes，不请求批准、不改变文件权限或时间戳

### Requirement: Overwrite requires previously delivered complete Read evidence

覆盖 SHALL 只接受当前 Runtime 中来自先前已 durable/finalize 原生工具输出的完整 Read 证据。证据 MUST 绑定同一路径、实际读取文件 identity、mode、size 和原始内容摘要；准备与执行时必须与当前目标一致。仅成功状态、到达 EOF、曾底层读取完整文件或用户 session allow 均不得替代该条件。

从中间开始、未到 EOF、长行截断、总预算截断、读取期间文件变化、Grep/Glob 结果、多片段拼接或来自 resume 的旧历史 MUST NOT 产生可用证据。缺失证据 SHALL 返回 `write_requires_full_read`；身份/内容/安全属性变化 SHALL 返回 `write_conflict`，均在 approval 与文件写入前结束。

#### Scenario: Read then overwrite in a later sample
- **WHEN** 一个 sample 的完整 Read 输出已提交，后续 sample 提出覆盖且目标证据仍一致
- **THEN** Write 可以准备覆盖候选并进入审批

#### Scenario: Reject same-sample speculative overwrite
- **WHEN** 同一 sample 依次请求 Read 和 Write，且此前没有该文件完整读取证据
- **THEN** Write 返回 write_requires_full_read，即使前面的 Read executor 已成功

#### Scenario: Reject a truncated read
- **WHEN** Read 到达 EOF 但某行被截断，或完整文件的输出超过预算
- **THEN** 后续 Write 不接受该 Read 作为整文件覆盖证据

#### Scenario: Detect changed content with preserved timestamp
- **WHEN** 文件在 Read 后被换成不同 bytes，即使长度或 mtime 被恢复
- **THEN** Write 通过内容/身份核验拒绝覆盖

#### Scenario: Require a fresh read after resume
- **WHEN** 恢复历史中包含完整 Read 和过去的 Write session allow
- **THEN** 新 Runtime 仍需新完整 Read 与新的授权，不能从旧历史恢复证据或 grant

### Requirement: Candidate preparation freezes operation and the complete approval presentation

准备 SHALL 在 ready batch durable 后执行，只允许安全读取和纯内存计算，不创建文件、目录或暂存项。候选 MUST 冻结 create/overwrite、规范目标、已有文件/祖先 identity、缺失目录清单、权限计划、最终 bytes、完整展示和内容派生 fingerprint。

create 展示 SHALL 覆盖完整新内容、空文件状态和全部缺失父目录；overwrite 展示 SHALL 覆盖全部变化。显示控制字符 MUST 确定性可见转义而不修改写入 bytes；CRLF、无末尾换行和 BOM 变化必须可辨别。整个展示最多 256 KiB，超限 SHALL 返回 `write_preview_too_large`，不得批准被截断的候选。

#### Scenario: Prepare without changing the workspace
- **WHEN** 目标文件及部分父目录不存在，合法 Write 已 ready
- **THEN** 生成精确的 create 候选和目录清单，workspace 中仍无这些目录、文件或暂存项

#### Scenario: Reject an oversized presentation
- **WHEN** 完整内容/diff 及可见转义、目录清单超过展示预算
- **THEN** Write 在审批前失败，不用省略号代替未展示修改

#### Scenario: Show terminal control characters safely
- **WHEN** 新文本含 ESC、回车或双向控制字符
- **THEN** 审批将它们显示为可辨别的确定性文本，最终 content bytes 不被显示转换修改

### Requirement: Creation atomically publishes without replacing any existing entry

创建 MUST 将完整内容及必要父目录准备在同文件系统的权限受控暂存空间中，在全部必要文件/目录 Sync 后以禁止覆盖既有目录项的原子操作发布。已有父目录下新建文件与缺失父目录链的创建都 SHALL 保持用户目标在发布前不存在；缺失目录链 SHALL 作为完整子树一次发布第一缺失组件，最多新建 16 层。

若目标或首个缺失组件被其他进程抢先创建，哪怕是空文件、空目录或 symlink，调用 MUST 返回 `write_conflict`，保留该对象，不合并、不覆盖、不自动重新准备。平台或文件系统不能提供禁止覆盖的原子发布时 SHALL 明确 unsupported，禁止“先 Exists 再普通 rename”的降级。

#### Scenario: Publish a new file
- **WHEN** 文件候选获批、started 已 durable，且目标仍不存在
- **THEN** 目标一次性出现完整 content，成功只在发布和父目录持久化完成后返回

#### Scenario: Publish a missing directory chain
- **WHEN** 只存在 src，获批目标为 src/new/pkg/a.go
- **THEN** new/pkg/a.go 在私有暂存树准备并 Sync 后整体发布到 new
- **THEN** 发布前 src/new 不存在，发布后完整目录链与文件可见

#### Scenario: Refuse a raced target
- **WHEN** 确定性测试在检查后、发布时让其他进程占据目标或首个缺失目录名
- **THEN** 原子发布失败为 conflict，竞争者 bytes 不变

#### Scenario: Fail without atomic creation support
- **WHEN** 文件系统明确不支持禁止覆盖发布
- **THEN** 返回 write_atomic_publish_unsupported 并清理自己的暂存项，不尝试直接创建/截断目标或普通 rename

### Requirement: Overwrite revalidates the approved base and preserves supported permissions

覆盖 MUST 通过实际目录/文件句柄验证路径、类型、权限与 symlink，并在执行重开和最终替换前核对已批准的 base bytes 摘要、identity、size 与 mode。目标被删除、替换或修改时 SHALL 冲突，不能把 overwrite 转成 create。验证成功后使用同目录完整暂存文件和原子替换，随后 Sync 父目录。

新文件 SHALL 使用 0600、新目录使用 0700；覆盖 SHALL 保留原文件 0777 基本权限位，拒绝特殊权限位。系统不得更改全局 umask，不承诺 inode/hardlink 关系或 owner/ACL/xattr 完整克隆。所有检查与写入 MUST 绑定安全句柄；不支持平台失败关闭。

#### Scenario: Preserve an executable file mode
- **WHEN** 0755 文件在完整读取与批准后被覆盖，基线仍一致
- **THEN** 新 content 原子替换旧文件且 mode 保持 0755

#### Scenario: Detect an approval-time edit
- **WHEN** 用户批准后、提交前文件内容或 identity 发生变化
- **THEN** Write 返回 conflict，不覆盖新内容

#### Scenario: Reject a symlink replacement
- **WHEN** 路径组件在准备或执行重开时被确定性替换为指向外部的 symlink
- **THEN** Write 失败关闭且外部目标不被读取或写入

### Requirement: Cancellation and failures report the actual publication boundary

执行接收前取消 MUST 零 executor 副作用；accepted started 后 executor 恰好被调用一次。发布前取消/确定失败 SHALL 清理自己的暂存资源后返回 error/cancelled，用户目标未改。发布已成功时，后续取消 MUST NOT 跳过 Sync/清理；这些步骤成功则 result SHALL 为 success，turn 可另行以取消结束。

无法确认发布结果、发布后目录 Sync 失败或清理/关闭无法确认时 MUST 返回 `outcome_uncertain` 并阻止该 Runtime 后续副作用。cleanup MUST 有唯一 owner，不能递归删除模型路径或未知对象。恢复 MUST NOT 扫描/清理/完成旧暂存项或重新执行；崩溃可能残留私有暂存项，不承诺进程强杀后的自动回收。

#### Scenario: Fail before publication
- **WHEN** 暂存写入或文件 Sync 被确定性注入失败
- **THEN** 原目标 bytes 或不存在状态保持不变，owner 关闭并清理自己的暂存项

#### Scenario: Cancel after a successful publication
- **WHEN** 原子发布已成功且随后 context 取消，但所有必要 Sync/清理成功
- **THEN** result 保存 success，不宣称文件未修改，也不再执行该调用

#### Scenario: Fail after publication
- **WHEN** 发布已成功但父目录 Sync 失败
- **THEN** 返回 outcome_uncertain 并 poison Runtime，后续 sibling 不执行

#### Scenario: Fail to clean private staging
- **WHEN** 清理暂存项时发生身份冲突、未知子项或确定 I/O 失败
- **THEN** 不删除未知对象，关闭持有资源并报告 outcome_uncertain，禁止自动重试

### Requirement: Write results and restoration preserve native pairing without repeating effects

Write SHALL 属于 exclusive 调用，始终按模型 call index 提交 results 与原生 outputs。result MUST 具有 typed status、稳定英文 code、有界冻结 preview，以及相对路径、可确定的 create/overwrite 操作或显式 unavailable、成功时字节/行数与创建目录数；success 必须具有已知操作，不能为 unavailable。空内容为零行，末尾换行是终止符、不另计空行。结果不得回显完整 content、base、diff、绝对路径或暂存名。

ready 只允许候选失败/取消直接收口；批准与 started 必须先 durable 才可修改。恢复时 ready 无批准、allow 无 started、started 无 result 分别收口为 `session_interrupted_before_approval`、`session_interrupted_before_execution`、`outcome_uncertain`，不读 workspace、不重新审批、不执行。result 已 durable 时使用原冻结 preview 补齐匹配原生 output。

#### Scenario: Restore a successful result before its output
- **WHEN** Write result 已 durable 但原生 output 尚未提交即退出
- **THEN** 恢复产生原样 preview、匹配原 call ID 的 output，不检查目标文件现状

#### Scenario: Restore an uncertain write
- **WHEN** Write started 已 durable 但 result 缺失
- **THEN** 恢复只补偿 outcome_uncertain 和配对 output，不发布或覆盖任何文件

#### Scenario: Preserve request equivalence
- **WHEN** 比较同一冻结 Write call/result 在不中断与恢复路径构造的下一请求
- **THEN** canonical bytes、native item 顺序和 fingerprint 相同
- **THEN** 读取证据表为空不会改变已经持久化的原生请求历史
