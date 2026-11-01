## MODIFIED Requirements

### Requirement: SQLite catalog is a rebuildable projection of validated journals

Session Catalog SHALL 将 SQLite 仅作为 JSONL journal 的可重建只读投影；Session 身份、恢复顺序、Provider-native history、turn lifecycle 和 repair 决策仍 MUST 由现有 Loader 与 ReplayPlanner 校验后的结果决定。Catalog MUST NOT 保存 API key、base URL、Authorization、Cookie、敏感 Header、配置文件路径、用户 prompt 正文或 Provider opaque payload，也不得向 JSONL 反向写入索引状态。

删除、缺失或 schema 不兼容的 Catalog SHALL 能从仍受支持的 journal 重建。重建后的可查询 thread 集合、筛选字段和排序键 MUST 与同一组 journal 的 ReplayPlan 投影等价；SQLite 损坏或重建失败不得删除、重写、重新编号或伪造任何 Session record。

#### Scenario: Rebuild after deleting SQLite

- **WHEN** 用户删除 `state.sqlite`，而 Session 根仍包含多个合法 root thread journal
- **THEN** 系统从 JSONL 重建 Catalog，得到与删除前相同的 thread 身份、兼容性字段和确定性排序
- **THEN** journal bytes、seq、checksum 和 Provider-native payload 保持不变

#### Scenario: Reject unsupported required history during projection

- **WHEN** 一个 journal 包含当前程序不支持的 record kind 或 payload 字段结构
- **THEN** Catalog 不把该 journal 投影为可恢复 thread
- **THEN** 系统不展开、不记录也不复制未知 opaque payload

#### Scenario: Preserve transcripts when catalog rebuild fails

- **WHEN** SQLite 无法安全打开、创建、校验或提交重建结果
- **THEN** 系统返回稳定英文 `session_catalog_failed` 错误且不调用 Provider
- **THEN** 全部 Session journal bytes 保持不变，显式 `--resume <thread-id>` 的事实源不变

### Requirement: Catalog lifecycle, schema, and storage are explicit and private

Catalog SHALL 不设置或校验 PRAGMA user_version，不使用 schemaVersion 或其他固定格式门禁。合法结构中无关的版本元数据 MUST NOT 触发重建；只按完整性、实际表字段和所需索引校验当前结构，不实现历史迁移。

Catalog 的纯内存配置与数据库资源获取 SHALL 分离；打开、创建、重建、提交和关闭 SQLite 必须使用显式生命周期操作。数据库连接、codec、schema 状态、statement 和缓存 MUST 由实例持有，不得使用可变包级 registry、可替换全局 codec、隐藏 goroutine 或进程级内存单例协调多个进程。

默认 Catalog SHALL 位于 `~/.easycode/state.sqlite`，Session 根和 Catalog 路径 MUST 能在测试与宿主装配时分别注入。支持权限位的平台上，父目录 MUST 为当前用户私有且数据库及其 sidecar MUST 使用用户私有权限；打开数据库不得跟随 symlink，文件类型与权限 MUST 根据 SQLite 实际使用的对象校验，单独 `Lstat` 后再按路径打开不满足该契约。无法提供等价保证的平台 SHALL fail closed。

Catalog schema SHALL 具有显式版本。当前程序遇到缺失、损坏或不兼容的派生 schema 时 SHALL 从 JSONL 重建，而不是把部分旧表当作有效结果；任何对外错误只包含稳定英文 code 和安全英文 message。

#### Scenario: Pure construction has no external effect

- **WHEN** 调用方只创建 Catalog 配置值
- **THEN** 系统不创建目录或 SQLite 文件、不打开连接且不启动 goroutine

#### Scenario: Reject a symlinked state database

- **WHEN** `state.sqlite` 或其必需 sidecar 被替换为 symlink 或非普通文件
- **THEN** Catalog 在查询或写入前以安全错误失败，且不修改 symlink 目标

#### Scenario: Rebuild an incompatible derived schema

- **WHEN** 现有 SQLite 表或索引结构不符合当前结构，或完整性校验失败
- **THEN** 系统不使用其中的 thread 行作恢复决策，并从 JSONL 构建当前 schema

#### Scenario: Ignore unrelated SQLite version metadata

- **WHEN** 当前表与索引结构完整合法，但 user_version 为任意值
- **THEN** 打开 Catalog 保留已验证投影行，不因该值重建，也不执行历史迁移
