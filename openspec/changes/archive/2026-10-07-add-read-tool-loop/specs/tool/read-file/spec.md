## Purpose

定义模型可调用的安全 Read 能力，使其只能在冻结的 workspace 边界内读取常规 UTF-8 文本文件，并获得确定、有界、可恢复复用的带行号结果。

## ADDED Requirements

### Requirement: Read input is strict and bounded

Read facade SHALL 接受一个严格对象：required `file_path`、optional `offset` 和 optional `limit`。`file_path` MUST 是非空合法 UTF-8 字符串；`offset` SHALL 是 1-based 正整数且默认 `1`；`limit` SHALL 为 `1..2000` 的整数且默认 `2000`。未知字段、重复字段、尾随 JSON、空路径、零值、负数、非整数或超限值 MUST 在 executor I/O 前拒绝。

相同 typed input 在 Anthropic 与 OpenAI facade 中 SHALL 表示相同读取范围；Provider decoder 不得自行采用不同默认值。

#### Scenario: Read with default range
- **WHEN** 模型只提供合法 `file_path`
- **THEN** typed input 使用 offset `1` 和 limit `2000`

#### Scenario: Reject an unknown field
- **WHEN** Read input 包含 schema 未声明字段或一个合法对象后的尾随 JSON
- **THEN** sample 失败且文件系统调用次数为零

### Requirement: Read is confined to the startup workspace by opened handles

应用 SHALL 在启动装配阶段冻结规范化 workspace root，并通过显式 I/O 生命周期入口取得安全目录能力；纯构造器不得打开目录。Read SHALL 接受 workspace 相对路径，或规范化后位于同一 workspace 内的绝对路径，并将最终模型结果中的路径表示为 workspace 相对路径。

在支持安全目录 handle 的平台上，系统 MUST 从已打开 workspace root 逐级相对打开路径，拒绝 `..` 逃逸、任何 symlink 组件、空组件、特殊文件和非普通最终对象，并把类型、权限和大小检查绑定到实际读取的文件 handle。单独执行字符串前缀判断、`EvalSymlinks`，或 `Lstat` 后再次按路径 `Open` MUST NOT 被视为安全证明。无法提供等价保证的平台 SHALL 失败关闭且不得回退到不安全读取。

#### Scenario: Read a workspace file
- **WHEN** 相对路径指向 workspace 内可读的常规文件且所有组件均非 symlink
- **THEN** Read 从绑定该文件的已验证 handle读取内容并返回 workspace 相对路径

#### Scenario: Reject traversal
- **WHEN** 输入通过 `..`、绝对外部路径或平台路径变体尝试离开 workspace
- **THEN** Read 返回安全路径错误且 workspace 外目标没有被打开

#### Scenario: Reject a symlink swap
- **WHEN** 测试在路径检查与打开期间确定性地把任一组件替换为指向 workspace 外的 symlink
- **THEN** Read 失败或继续使用替换前已取得的安全 handle，且外部目标内容不会进入结果

#### Scenario: Fail closed on an unsupported platform
- **WHEN** 当前平台没有本变更已验证的 handle-relative 安全实现
- **THEN** Read 返回稳定 unsupported 错误而不是使用普通路径读取降级

### Requirement: Read accepts only bounded UTF-8 regular text

Read SHALL 拒绝目录、设备、socket、FIFO 和其他非普通文件。实际打开文件的大小 MUST 不超过 16 MiB；超限时不得分配与声明大小无界等比例的内存。读取结果 MUST 是合法 UTF-8 且不得包含 NUL 字节；二进制或非法 UTF-8 内容 SHALL 作为 typed tool error 返回，不得进行 lossy 转码。

取消 SHALL 由传入 operation context 驱动；取消前未被 executor 接受时不得打开文件，接受后 owner 必须关闭 handle 并返回确定结果。

#### Scenario: Reject a binary file
- **WHEN** 常规文件包含 NUL 字节或非法 UTF-8
- **THEN** Read 返回稳定的 binary/encoding error，模型 preview 不包含该文件的原始 bytes

#### Scenario: Reject an oversized file
- **WHEN** 实际打开 handle 报告文件超过 16 MiB
- **THEN** Read 在读取完整内容前失败且不扩大通用 Session record 上限

#### Scenario: Cancel an active read
- **WHEN** operation context 在读取期间取消
- **THEN** Read 关闭它拥有的 handle并返回确定取消结果，不遗留后台 goroutine

### Requirement: Read results are deterministic and bounded

成功结果 SHALL 包含 workspace 相对路径、请求 offset/limit、实际返回的起止行、是否到达 EOF、是否发生长行或总预算截断，以及稳定带行号文本。行号从原文件的 1-based 行号开始；每行最多保留 2000 个 Unicode code points，截断不得产生非法 UTF-8，并 MUST 以确定标记报告省略。整个模型 preview 的 UTF-8 bytes MUST 不超过 256 KiB；达到预算时 SHALL 在完整行边界前停止并报告尚有内容省略。

相同文件 bytes、typed input 和 result codec revision MUST 产生逐字节相同的 preview。错误结果 SHALL 使用稳定英文 code/message，只包含 workspace 相对路径和安全边界信息，不得包含绝对 workspace、底层 OS 路径、文件正文、secret 或原始系统错误。

#### Scenario: Render selected lines
- **WHEN** offset `3`、limit `2` 读取一个至少五行的文本文件
- **THEN** preview 只包含原文件第 3 与第 4 行及其稳定行号，并报告尚未到达 EOF

#### Scenario: Truncate a long line safely
- **WHEN** 选中行超过 2000 个 Unicode code points
- **THEN** preview 在合法 UTF-8 边界截断该行并包含确定省略标记

#### Scenario: Enforce total preview budget
- **WHEN** 所选行的编码结果会超过 256 KiB
- **THEN** preview 不超过该预算、只包含完整编码边界并明确报告剩余内容被省略

#### Scenario: Keep errors path-safe
- **WHEN** Read 因权限、路径或读取错误失败
- **THEN** 返回给模型和宿主的错误不包含绝对 workspace、底层 cause 或文件内容

