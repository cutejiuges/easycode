## ADDED Requirements

### Requirement: Canonical encoding is independent of replaceable global configuration

缓存 fingerprint、Session checksum、Provider request 和当前持久化对象的 canonical 编码 SHALL 使用固定且不可由包级可变配置替换的规则。相同有效输入 MUST 在不同实例和恢复前后产生相同 bytes；第三方默认 codec 的修改不得改变该结果。纯内存构造不得为实现 codec 共享而启动后台任务或打开外部资源。

#### Scenario: Replace an unrelated global codec
- **WHEN** 隔离测试进程替换第三方默认 codec 配置，再编码同一有效对象
- **THEN** 当前 canonical bytes、checksum 与 fingerprint 和替换前逐字节一致
- **THEN** 测试不得通过共享进程全局修改干扰其他并行测试

#### Scenario: Preserve current valid fixtures
- **WHEN** 当前合法 Session、Provider 和 Tool fixture 经统一 codec 编解码
- **THEN** 既有 canonical bytes 与语义保持一致，不生成另一套版本化 fixture

### Requirement: Closed JSON contracts reject ambiguous or lossy input

自有封闭 JSON 契约 SHALL 在业务副作用前拒绝重复字段、未知字段、大小写别名、尾随值、非法 UTF-8、孤立 Unicode 代理项、缺失必需字段和不允许的 null。可选字段的默认值 MUST 只应用于字段省略，不能把显式 null 当作省略；嵌套封闭对象遵循同样规则。基础解析规则 SHALL 通过统一入口实现，边界仍持有自己的字段与业务验证，不允许宽松旧 decoder 回退。

Provider 外部 wire 中已允许的未知原生 item 与 opaque 扩展 SHALL 继续无损保留；封闭自有 envelope 的严格校验不得变成对所有未知原生字段的一概拒绝。

#### Scenario: Reject null instead of defaulting an integer
- **WHEN** Read 输入为 `{"file_path":"a","offset":null}` 或 `{"file_path":"a","limit":null}`
- **THEN** 输入被拒绝且 executor 不执行；省略 offset/limit 的合法输入仍采用既定默认值

#### Scenario: Reject nested ambiguity
- **WHEN** 当前持久化 payload 的嵌套封闭对象包含重复键、大小写别名或非 nullable 字段上的 null
- **THEN** 解码失败，不加载部分状态、不 repair 或 append 输入 journal

#### Scenario: Preserve opaque provider data
- **WHEN** 合法 native commit 包含未知原生 item、thinking signature 或 encrypted content
- **THEN** 往返与恢复保持原生顺序和 opaque bytes，严格校验不将其转换为共享 DTO

### Requirement: Architecture verification covers contract escape paths

质量门 SHALL 检查所有核心边界（包括 Tool）的无约束动态类型泄漏、业务包绕过统一 JSON 入口、canonical 路径读取可替换全局 codec，以及本次移除的旧构造入口回流。构造器副作用检查 MUST 覆盖生产包而不只覆盖协议包，并以确定性生命周期测试补足静态分析不能证明的边界。

#### Scenario: Reject a contract violation fixture
- **WHEN** 负例暴露 Tool any 参数、在业务包直接解码 JSON、读取可变全局 codec 或重新引入已删除的简化构造入口
- **THEN** 架构检查失败，并定位到对应违规边界

#### Scenario: Keep legitimate boundary markers
- **WHEN** 代码使用当前 Session canary、外部 headless version、内容 revision 或 Provider opaque envelope
- **THEN** 架构检查不会把这些合法边界误判为多版本兼容实现
