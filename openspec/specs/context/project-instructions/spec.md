# context/project-instructions Specification

## Purpose

定义 EasyCode 如何从启动工作目录所属项目中安全、确定地发现层级项目指令，并生成可供上下文规划与双 Provider 请求使用、但不污染会话历史的不可变快照。

## Requirements

### Requirement: Project instructions follow a bounded hierarchy and fixed precedence

系统 SHALL 从进程启动时规范化的绝对工作目录向父目录查找最近的 `.git` 项目边界；常规目录形式和 worktree 使用的常规文件形式均 SHALL 被识别为边界。找到边界时，候选目录 MUST 是该项目根到启动工作目录的连续目录链；未找到边界时，候选目录 MUST 只包含启动工作目录，不得继续扫描其父目录。

每个候选目录 SHALL 优先选择 `AGENTS.md`；仅当该名称不存在时才回退到同目录的 `CLAUDE.md`。选中的文件 MUST 按项目根到启动工作目录的顺序进入快照，同层不得同时加载两个名称。除固定名称外，系统不得发现用户级记忆、`.claude/rules`、include 指令或其他文件。

#### Scenario: Discover instructions from root to startup directory

- **WHEN** 项目根和启动工作目录各自存在 `AGENTS.md`，中间目录不存在候选文件
- **THEN** 快照按项目根文件、启动工作目录文件的顺序包含两份指令
- **THEN** 每份来源使用项目根相对路径而不是绝对路径

#### Scenario: Prefer AGENTS in the same directory

- **WHEN** 同一候选目录同时存在 `AGENTS.md` 和 `CLAUDE.md`
- **THEN** 系统只选择 `AGENTS.md`，且 `CLAUDE.md` 不影响快照内容、revision 或 fingerprint

#### Scenario: Fall back to CLAUDE when AGENTS is absent

- **WHEN** 候选目录不存在 `AGENTS.md` 但存在 `CLAUDE.md`
- **THEN** 系统选择该 `CLAUDE.md` 并保留其项目根相对来源

#### Scenario: Limit discovery outside a project

- **WHEN** 启动工作目录及其祖先都不存在可接受的 `.git` 边界
- **THEN** 系统只检查启动工作目录中的固定候选名称
- **THEN** 系统不读取任何父目录中的项目指令

#### Scenario: Reject an unsafe project boundary

- **WHEN** 向上发现遇到名为 `.git` 的 symlink 或特殊文件
- **THEN** 发现以 `project_instructions_unsafe` 失败，而不是越过该层继续扫描父目录
- **THEN** 系统不读取伪边界目标或边界之外的项目指令

### Requirement: Project instruction snapshots are deterministic and bounded

项目指令快照 SHALL 使用固定 schema revision，并对每个选中文件保存规范化的项目根相对来源、规范化 UTF-8 内容和截断状态。来源路径 MUST 使用 `/` 分隔，不得包含项目根绝对路径、启动 cwd 字符串、文件时间戳、inode、随机 ID 或遍历时序。

系统 SHALL 对最终可注入的规范化文本应用显式正整数总字节上限。应用默认值 MUST 为 32 KiB，任何可构造的上限 MUST 不超过 4 MiB。领域快照构造器和 Loader 构造器 MUST 同时拒绝零值、负值及超过 4 MiB 的配置；拒绝 MUST 在文件读取、预分配或其他外部副作用前完成。系统 MUST 按根到启动工作目录的既定顺序消费该预算；内容超过剩余预算时，只能在有效 UTF-8 边界确定性截断当前文件、标记快照已截断并停止加入后续文件。相同文件 bytes、相同相对来源、相同上限和相同 schema revision MUST 产生相同 canonical snapshot bytes 与内容派生 revision。

快照 canonical JSON MUST 通过仓库统一的稳定 codec 生成，保持既有 bytes 与 revision 不变；领域包不得直接配置第三方 JSON codec。指令读取 MUST 使用与硬上限无关的固定小缓冲，且任何保留区预分配 MUST 受已校验的 4 MiB 硬上限约束。

#### Scenario: Build the same snapshot twice

- **WHEN** 文件内容、相对来源、总字节上限和 schema revision 完全相同，但 mtime、inode 或扫描时序不同
- **THEN** 两次快照的文档顺序、canonical bytes、revision 和截断状态完全一致

#### Scenario: Truncate at a UTF-8 boundary

- **WHEN** 一个选中文件使规范化注入文本超过剩余字节预算，且截断位置落在多字节字符内部
- **THEN** 系统回退到此前最后一个有效 UTF-8 边界并标记快照已截断
- **THEN** 快照不加入当前文件的剩余内容或任何更深目录的文件

#### Scenario: Preserve existing behavior when no instructions exist

- **WHEN** 全部候选目录都不存在 `AGENTS.md` 和 `CLAUDE.md`
- **THEN** 系统产生无文档的有效快照
- **THEN** 该快照不要求 Provider 增加上下文 item，也不改变既有请求 canonical bytes

#### Scenario: Accept the hard byte-limit boundary

- **WHEN** 调用方以恰好 4 MiB 的上限构造 Loader 或领域快照
- **THEN** 构造成功且后续渲染、截断与 revision 仍遵守相同的确定性契约

#### Scenario: Reject an excessive byte limit before allocation

- **WHEN** 调用方以超过 4 MiB、平台 `MaxInt`、零值或负值构造 Loader 或领域快照
- **THEN** 构造立即返回稳定英文错误，且不读取文件、不调用 Provider、不尝试与该参数规模相当的预分配

#### Scenario: Preserve canonical snapshot identity through the shared codec

- **WHEN** 相同文档、截断状态与字节上限通过统一 codec 构造快照
- **THEN** canonical bytes 和内容派生 revision 与本变更已有 fixture 完全相同
- **THEN** 生产领域代码不直接导入或配置 Sonic

### Requirement: Project instruction reads are descriptor-bound and fail closed

系统 MUST 仅通过固定候选名称读取指令，并把文件类型、symlink 状态、权限和读取对象校验绑定到实际读取的文件描述符。指令候选 MUST 是常规文件；候选 symlink、目录、设备、FIFO、socket、打开后类型不一致或路径逃逸 MUST 以稳定英文 `project_instructions_unsafe` 失败，不得跟随到项目内外目标或回退同层另一名称。

选中文件存在但无法读取 MUST 以 `project_instructions_read_failed` 失败；内容不是合法 UTF-8 MUST 以 `project_instructions_invalid_utf8` 失败。不存在的候选可按优先级继续发现。平台无法提供所需的 descriptor-bound 安全语义时 MUST 明确失败，不得退化为 `Lstat` 后普通 `Open`。

#### Scenario: Reject a symlink candidate

- **WHEN** 优先候选 `AGENTS.md` 是指向项目内或项目外目标的 symlink
- **THEN** 发现以 `project_instructions_unsafe` 失败且不读取目标内容
- **THEN** 系统不回退读取同目录的 `CLAUDE.md`，也不调用 Provider

#### Scenario: Replace a candidate during open

- **WHEN** 测试在候选路径检查与读取之间确定性地把常规文件替换为 symlink 或特殊文件
- **THEN** 系统失败，或继续读取此前已经安全打开并校验的同一常规文件描述符
- **THEN** 替换后的目标内容不进入快照

#### Scenario: Reject invalid UTF-8

- **WHEN** 一个已选择的常规文件包含非法 UTF-8 bytes
- **THEN** 发现以 `project_instructions_invalid_utf8` 失败且不返回部分可用快照

### Requirement: One startup snapshot serves the process lifetime

应用 SHALL 在创建或恢复 Session、进入 TUI 或发起 Provider 请求前，从当前进程启动工作目录发现一次项目指令并形成不可变快照。相同进程中的全部 turn MUST 使用该快照；文件在进程运行期间发生变化 MUST NOT 热更新活动会话。新进程启动，包括显式 resume 或 continue，SHALL 从该新进程的启动工作目录重新发现快照。

项目指令内容和来源 MUST NOT 写入 Session JSONL、RuntimeEvent、诊断日志或 telemetry。安全错误只可包含稳定错误码与不泄露内容的相对来源摘要，不得包含指令正文、绝对路径、Provider secret 或其他文件内容。

#### Scenario: Change an instruction file during a running Session

- **WHEN** 应用已建立快照后，选中的项目指令文件被修改
- **THEN** 当前进程后续 turn 继续使用启动时快照
- **THEN** 系统不叠加新旧版本，也不把变化写入 Session records

#### Scenario: Reload instructions in a resumed process

- **WHEN** 新进程从相同启动工作目录恢复既有 thread
- **THEN** 应用在恢复可用状态和下一次 Provider 请求前重新发现项目指令
- **THEN** 恢复只使用新进程的单一快照，不从 Session 或 Provider 历史恢复旧快照
