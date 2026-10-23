# session/list-command Specification

## Purpose

提供不依赖模型连接的本地会话发现入口，让用户从当前目录或全部目录的历史会话元数据中获得明确的恢复目标，同时保持 Session 事实源、安全协调和显式恢复之间的边界。

## Requirements

### Requirement: Session listing is a separate local command

CLI SHALL 提供 `easycode sessions list [--all] [--limit N]`，默认当前规范化工作目录和 limit 20，合法 limit 为 1–200。`--all` SHALL 仅移除目录筛选。命令 MUST 在加载聊天配置、项目指令或创建聊天资源前分发；无 API key、无配置或无效聊天配置 MUST NOT 阻止列表。命令 MUST NOT 读取 stdin、启动 TUI/Runtime/Provider/工具或创建新的 Session journal。私有数据目录、锁文件及派生索引允许按既有显式生命周期创建。

`sessions --help` 和 `sessions list --help` SHALL 输出帮助并以 0 退出且不打开数据资源。缺失/未知子命令、未知选项、多余位置参数、非整数或越界 limit SHALL 返回 2；`--json`、`--print`、`--config`、`--resume` 等聊天选项不属于该子命令。首参数不是 `sessions` 时 SHALL 保留既有聊天解析，`easycode --print "sessions list"` 不得被当作列表请求。

#### Scenario: List without model credentials

- **WHEN** 用户无配置文件或配置损坏，并调用合法列表命令
- **THEN** 程序只访问本地会话存储，不读取 stdin、不发现项目指令、不发起网络请求
- **THEN** 列表不会产生新 thread 或 turn

#### Scenario: Validate before opening resources

- **WHEN** 用户请求帮助或提供 `--limit 0`、`--limit 201`、未知参数或多余位置参数
- **THEN** 帮助返回 0，非法输入返回 2，数据目录和 journal 均未改变

#### Scenario: Preserve a prompt containing the command name

- **WHEN** 用户执行 `easycode --print "sessions list"`
- **THEN** 参数继续作为聊天 prompt，列表路由不接管该请求

### Requirement: Session entries offer explicit scope and safe recovery guidance

列表 SHALL 使用无颜色、无终端控制序列的紧凑多行文本，明确当前目录或全部目录范围和最近活动排序。每个条目 SHALL 展示完整 canonical thread ID、UTC RFC3339Nano 更新时间以及 Provider family/wire/model；全部目录模式 SHALL 额外展示创建目录。ID 不得缩写或截断。输出 SHALL 提供 `easycode --resume <thread-id>` 的用法，并说明恢复需匹配 Provider/wire/model、使用启动时当前目录；MUST NOT 自动切目录、自动恢复、复制剪贴板或生成带用户路径的可执行 shell 命令。

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

### Requirement: Listing reports incomplete refresh and operational failures honestly

列表 SHALL 在一次成功协调后展示元数据。协调报告含 busy 或 invalid 时 SHALL 向 stderr 输出安全英文警告，说明这是全存储扫描的报告、列表可能包含未刷新元数据或遗漏会话；只要协调和查询成功，仍返回可用列表并退出 0。MUST NOT 把 busy/invalid 报告包装成“全部历史完整可恢复”，也不得把全局计数标成当前目录计数。

数据资源打开、安全检查、协调、查询或关闭失败 SHALL 返回 1 和安全英文错误；收到取消后 SHALL 清理资源并返回 1，不能输出正常成功列表。stdout/stderr 写入失败 SHALL 返回 1，不重复打印列表、不启动模型作为回退。所有已取得 lease、statement、数据库和目录句柄 SHALL 在命令返回前按唯一 owner 清理。

#### Scenario: Display available history while another process owns a journal

- **WHEN** 子进程持有 journal lease，使协调报告 busy 或 busy-unindexed
- **THEN** 列表不等待该 journal、不 repair、不生成虚构条目，stderr 说明结果可能不完整
- **THEN** 查询仍成功返回已索引条目并退出 0，既有 `--continue` 对 busy-unindexed 的失败关闭规则不变

#### Scenario: Do not disguise a storage failure as an empty list

- **WHEN** 数据库或目录不安全、无法打开、查询失败或资源关闭失败
- **THEN** 返回 1 及安全英文错误，不输出正常空列表或成功列表

#### Scenario: Cancel or fail output without leaking owners

- **WHEN** 命令执行中收到取消，或输出 writer 返回错误
- **THEN** 返回 1，所有存储资源释放，不重复输出且不创建后台清理 goroutine

### Requirement: Session text output is bounded and terminal safe

显示元数据 SHALL 将换行、制表、ESC、C0/C1、DEL、Unicode 行/段分隔及双向文本控制字符转换为可见转义，不能允许 metadata 注入额外条目或终端操作。每个 model/wire 展示值 SHALL 限制为转义后 256 个 Unicode code point，目录展示值限制为 512 个，超限带明确省略标记；MUST 保持 UTF-8 有效且不改变查询、排序或恢复 identity。

列表 SHALL 只使用规定的元数据字段，不展示 prompt、response、native reasoning、API key、base URL、Authorization、checksum、journal 路径或底层错误 cause。格式化 MUST 使用已加载值，不执行文件、数据库、网络或进程操作。

#### Scenario: Render hostile metadata literally

- **WHEN** model 或目录含换行、OSC/ANSI 序列、双向控制符或超长 Unicode 文本
- **THEN** 输出不产生控制序列或伪造条目，字段受限且 UTF-8 有效，thread ID 保持完整

#### Scenario: Keep transcript content out of discovery output

- **WHEN** 合法 journal 含敏感对话正文及 opaque reasoning
- **THEN** stdout/stderr 只出现允许的元数据与安全诊断，不出现正文或 opaque payload
