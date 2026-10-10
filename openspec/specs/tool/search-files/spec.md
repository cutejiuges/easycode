# Search Files Specification

## Purpose

定义模型可调用的 workspace 文件发现与内容搜索能力，使代码代理能在读取文件前安全定位目标，同时保持路径边界、确定性结果、资源上限和 Provider 无关的强类型语义。

## Requirements

### Requirement: Glob and Grep expose strict bounded inputs

Tool Catalog SHALL 暴露 `Glob` 与 `Grep`，分别映射到唯一当前 capability `fs.glob` 与 `fs.grep`。两者 SHALL 具有严格 JSON schema、typed constructor、strict decoder、validator、executor 和 result renderer；未知字段、尾随 JSON、空 pattern、绝对路径、路径穿越、非法 UTF-8、超限字符串或非法枚举 MUST 在文件系统 I/O 前失败。

`Glob` SHALL 接受必需的 `pattern`、可选的 workspace 相对 `path` 和可选 `limit`；默认 limit 为 100，合法范围为 1 至 1000。`Grep` SHALL 接受必需的 `pattern`，可选的相对 `path`、文件过滤 `glob`、`output_mode`、`case_insensitive`、前后 context 行数和 `limit`；默认 mode 为 `content`，默认 limit 为 250，最大为 1000，context 合法范围为 0 至 20。

#### Scenario: Decode valid search calls
- **WHEN** 任一 Provider 完成 schema 合法的 Glob 与 Grep 调用
- **THEN** Provider 层产生对应 capability 的强类型输入，且两家 Provider 的语义字段一致

#### Scenario: Reject unsafe search inputs
- **WHEN** 输入包含绝对 path、`..` 穿越、未知字段、非法 output mode、limit 超界或超限 pattern
- **THEN** 调用以稳定安全的英文验证错误结束，且 workspace 不被遍历

### Requirement: Search traversal remains descriptor-bound to the workspace

搜索 SHALL 从已打开并验证的 workspace root handle 开始，逐级相对已验证目录 handle 打开条目，并依据实际打开 handle 的 metadata 判定目录或普通文件。实现 MUST NOT 跟随 symlink、junction 或其他重解析点，MUST NOT 依赖单独 `Lstat` 后再按路径 `Open` 的检查窗口，也不得访问 workspace 外路径。

搜索 SHALL 包含普通隐藏文件和隐藏目录，但 MUST 跳过 `.git`、`.hg`、`.svn`、`.jj`、`.sl` 与 `.bzr` 元数据目录。首个切片 MUST NOT 读取或声称遵守 `.gitignore`；目录深度最大为 64，单次调用最多访问 200,000 个目录项。无法提供等价 handle-relative 安全保证的平台 MUST fail closed。

#### Scenario: Refuse a symlink escape
- **WHEN** workspace 内的目录项指向 workspace 外文件，或测试在遍历期间把路径组件替换为 symlink
- **THEN** 搜索不跟随该条目且不读取外部目标
- **THEN** 结果、错误和诊断不泄露外部绝对路径或内容

#### Scenario: Search hidden source but skip VCS internals
- **WHEN** workspace 同时包含 `.config/tool.yaml` 与 `.git/config`
- **THEN** 匹配规则可以返回前者，但遍历不进入 `.git`

### Requirement: Glob uses deterministic workspace-relative matching

`Glob` SHALL 对 workspace 相对、以 `/` 分隔的规范路径执行匹配，支持 `*`、`?`、字符类和跨路径段的 `**`；非法 pattern MUST 以验证错误失败。可选 `path` 只缩小遍历根，返回值仍为 workspace 相对路径。Glob SHALL 只返回匹配的普通文件，不返回目录、symlink 或绝对路径。

匹配结果 MUST 按规范路径的字节序稳定排序，不得按 mtime、文件系统枚举顺序或 goroutine 完成顺序排序。相同 workspace snapshot 与相同输入 MUST 产生逐字节相同的 typed result 和模型 preview。

#### Scenario: Match recursively with double star
- **WHEN** `pattern` 为 `internal/**/*.go` 且 workspace 含多层 Go 文件
- **THEN** Glob 返回全部匹配普通文件的规范相对路径，并按稳定字节序排序

#### Scenario: Ignore modification times
- **WHEN** 匹配文件内容和路径不变但 mtime 顺序改变
- **THEN** Glob 结果顺序与 preview bytes 保持不变

### Requirement: Grep uses deterministic line-oriented RE2 semantics

`Grep` SHALL 使用 Go regexp/RE2 兼容语义匹配单个普通文本文件中的行，不支持 PCRE、反向引用、多行模式、外部 `rg` 参数或按语言 type registry。`case_insensitive` SHALL 只改变大小写匹配；可选 `glob` 使用与 Glob 相同的路径匹配语义过滤候选文件。

`content` mode SHALL 按路径、行号输出匹配行及请求的有界前后文；`files_with_matches` SHALL 每个文件最多返回一次；`count` SHALL 返回每个文件的匹配行数。二进制文件、非法 UTF-8 文件和大于 16 MiB 的文件 MUST 不读取为文本，并在非敏感 metadata 中计数；单次 Grep 最多扫描 50,000 个普通文件与累计 256 MiB 内容。

#### Scenario: Return ordered content matches
- **WHEN** 多个文件的多行匹配同一 regexp
- **THEN** content 结果按规范路径和行号稳定排序，context 行不会改变匹配行计数

#### Scenario: Count matching lines
- **WHEN** 一行中 regexp 命中多次且另一行命中一次
- **THEN** count mode 对该文件报告两个匹配行而不是三个匹配 occurrence

#### Scenario: Skip non-text input safely
- **WHEN** 候选集合包含 NUL 二进制、非法 UTF-8 或超过单文件上限的文件
- **THEN** Grep 不把其内容放入 preview，并在有界 metadata 中报告跳过数量

### Requirement: Search results are bounded and explicit about omission

搜索 typed result SHALL 包含规范化 matches、`truncated`、省略匹配数、访问项数与跳过文件统计。达到调用 limit、遍历上限、文件上限、累计字节上限或 64 KiB 模型 preview 上限 SHALL 作为成功的有界结果返回，而不是伪装成“没有匹配”；preview 必须保留稳定的前部与尾部结果并明确报告中间省略数量。单行展示最多 500 个 Unicode code points，超出部分 SHALL 确定性截断。

取消和 deadline MUST 在遍历与文件读取边界被观察；取消结果不得包含未完成枚举顺序产生的非确定性部分。权限拒绝、条目竞态消失和不支持文件类型 SHALL 形成稳定安全的 typed error 或明确跳过统计，底层绝对路径、文件正文和系统错误 cause 不得进入错误、Session metadata 或诊断。

#### Scenario: Reach a result limit
- **WHEN** 确定性排序后的匹配数量超过调用 limit
- **THEN** 结果标记 `truncated`、报告省略数量并返回稳定的有界集合
- **THEN** Provider 不会把截断结果误解为完整搜索空间

#### Scenario: Render a byte-bounded preview
- **WHEN** typed matches 在逐行渲染后超过 64 KiB
- **THEN** preview 保留稳定首尾、插入省略摘要且总字节数不超过上限
