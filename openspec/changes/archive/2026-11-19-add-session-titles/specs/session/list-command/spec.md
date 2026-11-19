## MODIFIED Requirements

### Requirement: Session entries offer explicit scope and safe recovery guidance

列表 SHALL 使用无颜色、无终端控制序列的紧凑多行文本，明确当前目录或全部目录范围和最近活动排序。每个条目 SHALL 展示完整 canonical thread ID、UTC RFC3339Nano 更新时间以及 Provider family/wire/model；全部目录模式 SHALL 额外展示创建目录。已命名条目 SHALL 另行展示一行 `Title: <title>`。未命名条目省略该行。ID 不得缩写或截断。输出 SHALL 提供 `easycode --resume <thread-id>` 的用法，并说明恢复需匹配 Provider/wire/model、使用启动时当前目录；MUST NOT 自动切目录、自动恢复、复制剪贴板或生成带用户路径的可执行 shell 命令。

空结果 SHALL 返回 0，并区分当前目录没有已索引会话和全部目录没有已索引会话；当前目录空结果 SHALL 提示 `--all`。`has_more` 为 true 时 SHALL 明确仍有未显示条目：limit 小于 200 时提示提高 limit，达到 200 时说明当前上限，不得宣称已经显示全部历史。

#### Scenario: Display mixed model sessions

- **WHEN** 当前目录列表包含不同 Provider/wire/model 的会话
- **THEN** 输出可辨识各自配置元数据和完整 ID，并提供恢复用法，不声称与当前配置兼容

#### Scenario: Show cross-directory context without changing it

- **WHEN** `--all` 返回不同创建目录的会话
- **THEN** 每项展示安全格式化的创建目录，进程 cwd 不变
- **THEN** 恢复提示不拼接 `cd` 命令或自动替换配置

#### Scenario: Explain empty and bounded results

- **WHEN** 当前目录结果为空或结果因 limit 被截断
- **THEN** 输出分别提供扩大目录范围或数量限制的准确提示，不把空结果当作错误

#### Scenario: Keep complete recovery identities with duplicate titles

- **WHEN** 列表包含两个同名会话以及一个未命名会话
- **THEN** 两个同名条目各有一行标题，未命名条目省略该行，三个条目都展示各自完整 canonical thread ID
- **THEN** 恢复提示继续使用 thread ID

### Requirement: Session text output is bounded and terminal safe

显示元数据 SHALL 将换行、制表、ESC、C0/C1、DEL、Unicode 行/段分隔及双向文本控制字符转换为可见转义，不能允许 metadata 注入额外条目或终端操作。每个 model 与 wire 展示值 SHALL 限制为转义后 256 个 Unicode code point，目录展示值限制为 512 个，超限带明确省略标记。显式标题 SHALL 以通过 `session/titles` 校验的有界单行纯文本展示，保留合法字素。显示 MUST 保持 UTF-8 有效且不改变查询、排序或恢复 identity。

列表 SHALL 只使用规定的元数据字段，不展示 prompt、response、native reasoning、API key、base URL、Authorization、checksum、journal 路径或底层错误 cause。格式化 MUST 使用已加载值，不执行文件、数据库、网络或进程操作。

#### Scenario: Render hostile metadata literally

- **WHEN** model 或目录含换行、OSC/ANSI 序列、双向控制符或超长 Unicode 文本
- **THEN** 输出不产生控制序列或伪造条目，字段受限且 UTF-8 有效，thread ID 保持完整

#### Scenario: Keep transcript content out of discovery output

- **WHEN** 合法 journal 含敏感对话正文及 opaque reasoning
- **THEN** stdout/stderr 只出现允许的元数据与安全诊断，不出现正文或 opaque payload

#### Scenario: Render a title as one terminal-safe metadata line

- **WHEN** 已命名会话的标题含中文、ZWJ emoji 或字面反斜杠
- **THEN** 输出单行标题保留合法 Unicode 内容和字面字符，遵循标题长度上限，且该条目的完整 thread ID 保持可复制
