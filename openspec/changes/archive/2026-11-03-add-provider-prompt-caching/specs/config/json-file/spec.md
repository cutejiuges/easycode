## ADDED Requirements

### Requirement: Configure explicit Provider cache and transport policies

JSON 配置 SHALL 接受三个可选顶层字符串：anthropic_prompt_cache 为 disabled、ephemeral_5m 或 ephemeral_1h，默认 disabled；openai_prompt_cache_key 为 omit 或 thread，默认 omit；openai_transport 为 http_sse 或 websocket_auto，默认 http_sse。字段名与枚举值大小写精确匹配；null、未知值、非字符串、重复字段和未知字段 MUST 以安全英文 invalid_configuration 拒绝。三个字段只由 JSON 显式设置，本切片不增加环境变量、CLI 覆盖或模型名推断。

既有 Provider 环境变量合并完成后，非 Anthropic family MUST 拒绝非 disabled 的 anthropic_prompt_cache，非 OpenAI family MUST 拒绝非 omit 的 openai_prompt_cache_key 或非 http_sse 的 openai_transport；无关 family 的缺省值允许。websocket_auto SHALL 表达用户允许尝试连接与严格安全回退，不表示远端支持已验证。配置错误不得在 Provider 网络、Session 创建或后台任务后才被发现。

#### Scenario: Keep environment-only startup unchanged
- **WHEN** 用户仅提供既有 Provider 环境变量
- **THEN** 缓存字段省略、HTTP/SSE 为默认，无新必填配置

#### Scenario: Enable the complete OpenAI path
- **WHEN** 合并后的 family 为 openai，key 为 thread，transport 为 websocket_auto
- **THEN** 配置成功，key 与 transport 独立生效，不自动改变 store=false

#### Scenario: Enable an Anthropic lifetime
- **WHEN** family 为 anthropic，anthropic_prompt_cache 为 ephemeral_1h
- **THEN** 普通与摘要请求使用固定一小时显式策略，OpenAI 字段仍为默认

#### Scenario: Reject a family changed by the environment
- **WHEN** JSON 启用 Anthropic 缓存而环境变量将最终 family 覆盖为 openai
- **THEN** 合并后拒绝非法组合，错误不回显文件、API key 或 URL

#### Scenario: Reject an invalid cache value
- **WHEN** 任一新字段为 null、布尔、未知枚举、大小写别名或重复字段
- **THEN** 返回 invalid_configuration，数据目录与 Provider 请求均未改变
