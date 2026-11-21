## MODIFIED Requirements

### Requirement: Command hook configuration is bounded strict and frozen

系统 SHALL 显式加载用户 `~/.config/easycode/hooks.json` 与启动 workspace 根 `.easycode/hooks.json`，只接受顶层 hooks 对象中的 PreToolUse、PostToolUse、PostToolUseFailure、Stop、PreCompact、PostCompact 数组；三个工具事件与两个压缩事件的 group 含 matcher/hooks，Stop group 只含 hooks 且 MUST 拒绝任何 matcher；每条含 id/type/command/timeout，两个工具后置事件、Stop、PreCompact 和 PostCompact 支持 feedback；type MUST 为 command。缺失文件表示空来源；重复/未知字段、非 UTF-8、大小写别名、null、尾随 JSON、错误类型与未支持事件或行为 MUST 使整个来源无效。timeout 省略为 5 秒，只允许整数 1..30；id 为 1..64 字节小写 ASCII 字母/数字/单连字符、首尾字母数字且来源内唯一，command 非空且最多 8 KiB。

每来源 SHALL 最多 64 KiB 和 32 条 Hook，合并最多 64 条；单调用每事件最多匹配 8 条。工具事件的 matcher MUST 是 `*` 或以 `|` 分隔的非重复字面名称：Edit/apply_patch 对应 fs.patch，Write 对应 fs.write，Bash/exec_command 对应 process.exec。空分支、未知名称或正则语法 MUST 拒绝；别名命中相同能力不得重复执行一条声明。工具事件的 `*` SHALL 仅指上述三项能力。两个压缩事件的 matcher SHALL 仅接受单一字面 `manual`、`auto` 或 `*`，其中 `*` 匹配两种压缩触发。

同一事件 SHALL 按用户来源、项目来源、配置数组位置稳定选择，按相同顺序执行实际获准项。source revision SHALL 由完整规范内容派生；绝对路径、时间、inode、执行或信任身份不得参与内容 revision。相同内容和数组顺序 MUST 产生相同 bytes/revision，getter 返回值修改不得改变快照。快照只在启动时建立，后续磁盘编辑不改变当前运行语义。


PostToolUse/PostToolUseFailure/Stop/PreCompact 的 feedback SHALL 仅接受 none/model，省略规范化为 none。PostCompact 仅支持省略或 none；PreToolUse 出现 feedback MUST 拒绝。每来源 32 条和合并 64 条上限按六个事件合计，id 在来源全部事件中唯一；规范配置显式按 PreToolUse、PostToolUse、PostToolUseFailure、Stop、PreCompact、PostCompact 排序，不以对象键插入顺序决定 revision。未知事件继续失败关闭。source revision 覆盖六事件全部声明，command fingerprint SHALL 绑定事件与披露策略。

#### Scenario: Load two ordered sources
- **WHEN** 用户和项目各有合法的匹配 Hook，文件对象键顺序不同
- **THEN** 用户项先于项目项执行，数组顺序保留，规范 bytes 与 revision 可重复

#### Scenario: Refuse a partial snapshot
- **WHEN** 一个已存在来源损坏、超限或包含未支持事件，另一个来源合法
- **THEN** 三项 mutation 调用以 hook_config_invalid 阻断，不从合法来源组成部分放行；已保存答复、只读工具与配置浏览仍可用，但最终完成边界 MUST 以 hook_config_invalid 失败且零 Stop 执行。接纳的压缩同样以 hook_config_invalid 收口且零 PreCompact 执行，不能通过损坏来源推断没有 Stop 或 PreCompact

#### Scenario: Match aliases only once
- **WHEN** matcher 为 Bash|exec_command，当前 ready 调用属于 process.exec
- **THEN** 该声明只执行一次，输入中的真实 facade 名不被替换

#### Scenario: Freeze startup content
- **WHEN** 快照建立后磁盘配置发生编辑
- **THEN** 当前进程继续使用已捕获声明，getter、浏览和调用不重读配置；重启才建立新快照

#### Scenario: Freeze both events and disclosure mode
- **WHEN** 同一配置声明两个事件且 PostToolUse 省略 feedback，随后仅更改为 model
- **THEN** 首次规范值为 none，第二次 source revision/command fingerprint 改变，旧信任不适用；对象键顺序不影响 bytes

#### Scenario: Reject frontloaded disclosure on PreToolUse
- **WHEN** PreToolUse 声明包含 feedback 或六个事件合计超出来源数量上限
- **THEN** 整个来源无效且工具与 Hook 均不执行

#### Scenario: Freeze failure declarations in the same source
- **WHEN** 同一来源同时配置三事件，失败声明使用 feedback=model，且对象键顺序不同
- **THEN** 规范顺序固定、revision 相同；三事件合计数量/id 约束和每事件匹配上限均生效

#### Scenario: Reject duplicate ids across success and failure
- **WHEN** 同来源成功和失败声明使用相同 id
- **THEN** 整个来源无效，不以另一事件或合法来源部分放行 mutation

Stop SHALL 仅按回合完成事件选择，不与任一工具能力匹配。每候选最多 8 项，保持 user/project 和数组顺序；六事件共享来源计数/id 唯一规则。合法无诊断且 Stop 为空时保留无 Stop 快速路径，既有工具 matcher 的 * 仍只指三种 mutation。

#### Scenario: Load a completion hook without a matcher
- **WHEN** Stop group 只含合法 hooks，feedback 省略
- **THEN** 规范化为 feedback=none 并保持顺序；任何 Stop matcher、六事件重复 id 或合计超限使来源无效

#### Scenario: Preserve a valid tool-only configuration
- **WHEN** 两来源合法且未配置 Stop
- **THEN** 聊天完成沿既有无 Stop 路径，不新增 Stop facts 或请求内容

#### Scenario: Freeze compaction declarations with other events

- **WHEN** 一个合法来源同时配置工具、Stop 与 PreCompact，PreCompact 使用 `manual` 并省略 feedback
- **THEN** 规范顺序不受 JSON 键顺序影响，PreCompact 规范化为 feedback=none，其 id 和数量纳入该来源全部事件的共享约束
- **THEN** 将 PreCompact matcher 改为 `manual|auto` 使整个来源无效

#### Scenario: Freeze an observation hook with the shared source

- **WHEN** 同一来源同时含两个压缩事件，PostCompact 缺省 feedback 且 trigger matcher 是 `*`
- **THEN** PostCompact 以 none 规范化，参与同一来源的顺序、revision、条数和 ID 约束，独立于 PreCompact 匹配
- **THEN** 将 PostCompact feedback 改为 model 时拒绝整个来源
