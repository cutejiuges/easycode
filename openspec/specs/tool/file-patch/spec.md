# tool/file-patch Specification

## Purpose

定义双 Provider 共用的受工作区约束文件 patch 能力，使模型能够提出、展示并在用户批准后原子修改现有文本文件，同时保持确定性 wire、冲突检测和恢复安全。

## Requirements

### Requirement: One patch capability has Provider-native facades

当前 Tool Catalog SHALL 增加唯一共享 capability `fs.patch`。Anthropic facade MUST 名为 `Edit` 并使用 strict structured JSON，至少包含 `file_path`、`old_string`、`new_string` 与可选 `replace_all`；OpenAI facade MUST 名为 `apply_patch` 并使用 custom freeform input。两个 facade SHALL 具有稳定描述、wire kind、canonical definition bytes和确定 fingerprint，并路由到同一 typed patch plan与真实 executor。

Catalog 只有在对应 Provider 的 request compiler、stream reducer、native persistence/restore、result codec、policy、approval presenter、executor 和 golden 全部存在时才能暴露该 facade。动态 invocation、approval、workspace绝对路径、base evidence、diff 和临时权限状态不得进入稳定 schema bytes或fingerprint。

#### Scenario: Compile both patch facades
- **WHEN** 使用相同已实现能力构造 Catalog snapshot
- **THEN** Anthropic view 包含 structured `Edit`，OpenAI view 包含 custom freeform `apply_patch`
- **THEN** 两次构造的 facade 顺序、canonical bytes和fingerprint完全相同

#### Scenario: Reject an incomplete patch registration
- **WHEN** 任一 facade 缺少 decoder、policy、executor、result codec或presenter
- **THEN** Catalog 构造失败且两家请求都不会声明半成品 patch 工具

### Requirement: Provider inputs strict-decode to one immutable existing-file plan

一个 `fs.patch` invocation SHALL 恰好修改一个 workspace 内已经存在的常规 UTF-8 文件。共享 typed plan MUST 保存规范化 workspace 相对路径和至少一个有序 replacement；不得接受绝对逃逸、`.`/`..` traversal、NUL、空修改、重复/重叠 replacement、创建、删除、移动、权限修改、二进制内容或超过 1 MiB 的工具输入。

Anthropic `Edit` SHALL 要求非空 `old_string`；`replace_all=false` 时 old string 在 base 文件中必须恰好匹配一次，`replace_all=true` 时必须至少匹配一次。OpenAI freeform input SHALL 严格使用 `*** Begin Patch`、单个 `*** Update File:`、一个或多个非空 `@@` update hunks和 `*** End Patch`；Add/Delete/Move、多个文件、尾随非空内容、模糊匹配和未消费内容 MUST 被拒绝。

Provider delta 只能更新无副作用 staging/draft。只有原生 item 完成、输入完整、strict parser消费全部输入且共享 plan验证成功后才能形成 ready call；任何 partial、未知 facade、重复 call ID或超限输入都必须使 sample 失败，且不得读取或写入文件。

#### Scenario: Decode an Anthropic Edit
- **WHEN** 完成的 `Edit` tool_use 提供合法路径、old string、new string和replace_all
- **THEN** reducer 产生一个 `fs.patch` typed ready call并保留原 Provider call ID

#### Scenario: Decode an OpenAI update patch
- **WHEN** custom `apply_patch` call 完成一个单文件、完整且无尾随内容的 Update File patch
- **THEN** reducer 产生等价的有序 typed replacement plan

#### Scenario: Reject unsupported patch operations
- **WHEN** freeform patch 尝试 Add File、Delete File、Move to或修改多个文件
- **THEN** sample以stream protocol failure结束且没有ready fact、approval或文件I/O

### Requirement: Candidate preparation produces base evidence and an exact bounded diff

patch ready fact durable 后，系统 SHALL 通过 workspace 的安全 handle-relative traversal 打开目标；最终类型、权限、symlink 和 containment 判断 MUST 绑定到实际句柄。目标必须是可读写的常规 UTF-8 文件且不超过 16 MiB；symlink、目录、设备、FIFO、不支持平台和 workspace 外对象 MUST 失败关闭。

候选准备 SHALL 对实际读取的完整 base bytes应用全部 replacements，生成确定的 final bytes、内容摘要、文件 identity/metadata evidence和面向用户的 unified diff。diff MUST 完整表示该 invocation 将产生的变化并限制在 256 KiB；无法在预算内完整展示时调用 SHALL 在 approval 前以 `patch_preview_too_large` 失败，不得使用截断 diff 请求批准。

候选准备只能读取目标和执行纯内存计算，不得创建临时文件、改变权限或修改目标。空结果变化 SHALL 返回 `patch_no_changes`，不请求批准。

#### Scenario: Prepare an exact diff
- **WHEN** ready patch 对安全打开的文件有唯一合法匹配且完整 diff 在预算内
- **THEN** 候选包含 base evidence、确定 final bytes、fingerprint和覆盖全部变化的unified diff
- **THEN** 目标 bytes保持不变

#### Scenario: Reject a symlink target
- **WHEN** 路径在安全 traversal 或最终打开时解析为 symlink
- **THEN** 候选准备失败且symlink目标没有被读取或修改

#### Scenario: Reject an oversized diff
- **WHEN** patch 的完整 approval diff 超过 256 KiB
- **THEN** 调用返回 `patch_preview_too_large`且不发布可批准的截断视图

### Requirement: Approved patch execution revalidates evidence and replaces atomically

Patch executor SHALL 只接受已匹配 durable allow fact且已 durable execution started 的不可变候选。执行时 MUST 使用同一 workspace 安全边界重新打开父目录和当前目标，并重新验证常规文件类型、symlink、权限、规范路径、base content摘要和候选 identity；任一差异 SHALL 返回 `patch_conflict` 或安全错误，并且不得写入候选 final bytes。

验证成功后，executor SHALL 在同一已验证父目录创建唯一临时常规文件，写入完整 final bytes，保留目标受支持的权限位，成功 Sync 临时文件后以 descriptor-relative rename 原子替换目标，并 Sync 父目录后才返回成功。临时文件具有唯一 cleanup owner；取消、write/Sync/rename失败 MUST 关闭并删除临时文件，不得留下部分目标内容。rename 成功后的取消或目录 Sync 失败不得谎报“未修改”；结果必须明确表示 committed 或 durability uncertain并阻止自动重试。

本能力不承诺与外部进程的通用事务隔离，也不跨多个文件提供原子性；单 invocation 只修改一个文件。

#### Scenario: Apply an approved patch
- **WHEN** candidate 获得 durable allow、started durable且执行前base evidence仍匹配
- **THEN** 目标通过同目录临时文件原子替换为candidate final bytes
- **THEN** 成功只在文件和父目录所需Sync完成后返回

#### Scenario: Detect a changed base
- **WHEN** 用户批准后目标内容、identity或安全属性在执行前发生变化
- **THEN** executor返回`patch_conflict`且不覆盖新内容

#### Scenario: Fail before atomic replace
- **WHEN** 临时文件write或Sync确定失败
- **THEN** 原目标bytes保持不变且临时文件由唯一owner清理

#### Scenario: Report an uncertain committed outcome
- **WHEN** rename已成功但父目录Sync结果无法确认
- **THEN** Session进入poisoned或等价不可继续副作用状态，结果不宣称安全重试

### Requirement: Patch calls are exclusive and results remain ordered

`fs.patch` SHALL 使用 `exclusive` 调度语义。同一 sample 中按 call index 连续的 `parallel_read` 可以有界并行；每个 Patch 的候选准备与审批只在其前序批次结果durable后开始，获批后独占执行，其后调用必须等待该 Patch terminal result。审批 SHALL按Patch call index串行呈现，任一Patch executor都不得在匹配allow fact durable前启动。

拒绝、冲突、取消或普通 patch error只占据自己的固定 call slot，除 `abort` 或 poisoned 状态外不得取消独立 sibling。全部 results 与 Provider-native outputs仍按原 call index durable和提交，实际完成时间与用户响应时间不得改变顺序。

#### Scenario: Place a patch between read batches
- **WHEN** 一个sample依次包含Read、Grep、Patch、Read
- **THEN**前两个只读调用可并行，Patch在它们完成后独占执行，最后一个Read在Patch结束后执行
- **THEN**四个results和outputs仍按原call index提交

#### Scenario: Deny one patch without cancelling siblings
- **WHEN** 用户拒绝sample中的一个patch且没有中断turn
- **THEN**该slot获得denied result，其他已允许调用按调度边界继续

### Requirement: Patch result and native outputs are deterministic and recoverable

每个 patch result SHALL 使用 typed status、稳定英文 code、完整有界模型 preview和非敏感 metadata；成功 metadata至少包含workspace相对路径、replacement/hunk数量和确定的changed line summary，不得包含workspace绝对路径、临时文件名、完整文件正文或secret。denied、conflict、cancelled、error和outcome uncertain均必须形成与原Provider call ID匹配的输出。

Anthropic SHALL 编码匹配 `tool_use.id` 的 `tool_result`；OpenAI SHALL 编码匹配 custom call ID 的 `custom_tool_call_output`。输出 preparation必须是纯内存操作并复用durable冻结preview；resume不得重新读取目标、重新应用patch、重新计算diff或重复写文件。

#### Scenario: Encode a successful result for both Providers
- **WHEN** 同一语义patch分别从Anthropic Edit和OpenAI apply_patch成功完成
- **THEN** 两家各自产生匹配原生call kind和call ID的有界成功output

#### Scenario: Resume after result durability
- **WHEN** patch result已durable但进程在native output commit前退出
- **THEN** resume从冻结preview生成逐字节相同的匹配output且不访问文件系统
