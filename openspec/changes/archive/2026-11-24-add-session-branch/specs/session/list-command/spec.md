## MODIFIED Requirements

### Requirement: Session entries offer explicit scope and safe recovery guidance

列表 SHALL 使用无颜色、无终端控制序列的紧凑多行文本，明确当前目录或全部目录范围和最近活动排序。每个条目 SHALL 展示完整 canonical thread ID、UTC RFC3339Nano 更新时间以及 Provider family/wire/model；全部目录模式 SHALL 额外展示创建目录。已命名条目 SHALL 另行展示一行 `Title: <title>`。未命名条目省略该行。分支条目 SHALL 另行展示 `Branch of: <parent-thread-id>`，使用已验证且完整的 canonical 父 ThreadID，根线程省略该行。ID 不得缩写或截断。输出 SHALL 提供 `easycode --resume <thread-id>` 的用法，并说明根线程和分支的恢复均需匹配 Provider/wire/model、使用启动时当前目录；MUST NOT 自动切目录、自动恢复、复制剪贴板或生成带用户路径的可执行 shell 命令。

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

#### Scenario: Identify a branch with its independent recovery ID
- **WHEN** 离线列表包含同一逻辑 Session 的根线程和一个分支
- **THEN** 两条记录分别显示完整的恢复 ThreadID。分支旁显示独立的 `Branch of` 行，根线程省略该行。分支恢复指向其本身的 ThreadID
