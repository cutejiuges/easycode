## ADDED Requirements

### Requirement: Configuration security checks bind to the opened file

配置加载 SHALL 在读取正文前，以不跟随 symbolic link 的方式打开目标，并从同一个已打开文件句柄校验普通文件类型、大小和支持平台上的权限。路径检查与文件打开之间发生的替换 MUST NOT 使应用读取未经校验的文件；无法提供该保证的平台 MUST 明确拒绝不安全路径，而不是降级为先检查路径再打开。

#### Scenario: Reject a symbolic-link configuration

- **WHEN** 默认或显式配置路径本身是 symbolic link
- **THEN** 应用在读取正文前返回安全英文 `invalid_configuration` 错误
- **THEN** 应用不发起 Provider 请求且错误不暴露链接目标

#### Scenario: Reject a file swapped during secure open

- **WHEN** 测试在路径解析与文件打开边界确定性地把目标替换为 symlink、目录或权限不安全的文件
- **THEN** 应用拒绝配置或只读取已经取得并完成句柄校验的原文件
- **THEN** 未经同一句柄校验的替换目标内容不会进入配置

#### Scenario: Validate permissions on the opened file

- **WHEN** 配置文件包含非空 API key 且已打开文件的实际权限允许 group 或 other 访问
- **THEN** 应用根据该文件句柄的 metadata 返回 `invalid_configuration`
- **THEN** 先前对路径执行的检查结果不能覆盖句柄检查结果
