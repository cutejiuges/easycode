## ADDED Requirements

### Requirement: Known record revisions expose typed construction and decoding

每个已知 Session `event_kind`/`payload_version` 组合 SHALL 具有专属的强类型 draft constructor、严格 decoder 和语义 validator。公共 record draft、writer、replay 与生命周期接口 MUST NOT 接受或返回无约束动态值；未知扩展只允许作为有大小边界的 opaque JSON 保留。构造和解码都 SHALL 拒绝字段缺失、未知字段、尾随 JSON 和不满足该 revision 语义不变量的 payload。

该收紧 MUST 保持六种 v1 payload 的 canonical JSON、既有 envelope、checksum 与不可变 fixture bytes 不变。

#### Scenario: Construct every v1 record through a typed API

- **WHEN** 调用方创建 `session_meta`、`thread_meta`、`turn_started`、`provider_native_commit`、`turn_completed` 或 `turn_failed` v1 draft
- **THEN** 对应 constructor 只接收该 kind 的强类型字段或 payload
- **THEN** draft 不能被调用方改造成 kind 与 payload 不匹配的记录

#### Scenario: Strictly decode a known payload revision

- **WHEN** checksum 合法的已知 v1 record payload 含未知字段、尾随 JSON 或非法语义值
- **THEN** 该 kind/revision 的 decoder 在 replay 前拒绝记录
- **THEN** 错误不包含 opaque payload 正文

#### Scenario: Preserve historical v1 bytes

- **WHEN** 当前实现加载仓库中的不可变 v1 fixture，并以相同固定 identity、时间和 batch 参数编码等价记录
- **THEN** 解码后的 typed replay model 与既有期望一致
- **THEN** canonical record bytes 和 checksum 与变更前 fixture 完全一致

### Requirement: Repository lifecycle and path safety are explicit

Session Repository 的纯内存配置与外部资源获取 SHALL 分离。任何创建数据根、打开目录句柄、创建 journal、取得 lease 或启动 writer 的操作 MUST 使用 `Open`、`OpenOrCreate`、`Create` 或 `Start` 等显式生命周期入口；纯构造器不得访问文件系统或启动 goroutine。

在支持安全目录句柄的平台上，数据根、每一级日期目录和 journal SHALL 相对于已校验的父目录句柄逐级打开或创建，且不得跟随 symlink。目录类型与权限、journal 普通文件类型与权限 MUST 由实际打开的句柄校验；无法提供等价保证的平台 MUST fail closed 或使用经过测试的平台专属安全实现。

#### Scenario: Pure construction has no filesystem effect

- **WHEN** 调用方仅创建 Repository 配置或其他纯内存 Session 值
- **THEN** 数据根、日期目录、journal、lease 和后台 goroutine 均不会被创建或打开

#### Scenario: Reject a swapped path component

- **WHEN** 测试在目录遍历过程中确定性地把任一级日期目录或 journal 替换为 symlink、非目录或非普通文件
- **THEN** 创建或恢复在读取、repair、truncate 或 append 前失败
- **THEN** Session 数据根之外的目标 bytes 保持不变

#### Scenario: Validate every opened directory handle

- **WHEN** 数据根或某一级已有日期目录在支持权限位的平台上不是用户私有目录
- **THEN** Repository 根据已打开目录句柄的实际 metadata 拒绝继续
- **THEN** 不会依赖早先的路径 `Lstat` 结果继续打开子项
