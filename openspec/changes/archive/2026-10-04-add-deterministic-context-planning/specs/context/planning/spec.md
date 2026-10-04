## Purpose

定义 Provider 请求前的确定性上下文来源、不可变计划、缓存分段、token 估算和预算判定，使共享 Runtime 能在不解析或重建 Provider 原生历史的前提下诊断上下文并阻止明确超限的请求。

## ADDED Requirements

### Requirement: Context planning uses typed ordered sources

上下文规划 SHALL 只接收强类型的 Provider profile、已提交语义历史、Provider 原生历史 footprint、当前用户输入和可选预算。当前切片的来源 MUST 按 `provider_profile`、`committed_history`、`current_input` 的固定顺序出现；不得隐式读取时间戳、随机 ID、cwd、环境变量、Session 路径、git 状态、文件系统或网络。

`provider_profile` SHALL 只包含 Provider family、model 和显式 schema revision，不得包含 API key、base URL 凭据或其他 secret。规划 MUST 是纯内存操作，且不得修改输入快照或 Provider 原生历史。

#### Scenario: Plan the same inputs twice

- **WHEN** 两次规划使用相同 profile、语义历史、原生 footprint、当前输入和预算
- **THEN** 两次计划的来源顺序、revision、估算、预算状态、canonical cache segments 和指纹完全一致

#### Scenario: Exclude ambient process state

- **WHEN** 规划输入不变但当前时间、cwd、环境变量、Session 文件路径或随机源发生变化
- **THEN** 计划的 canonical bytes、分段指纹和预算判定保持不变

#### Scenario: Keep secrets out of the profile source

- **WHEN** 相同 family 和 model 使用不同 API key 或带凭据的配置来源
- **THEN** `provider_profile` 来源和稳定前缀指纹保持一致
- **THEN** 计划摘要、错误和诊断不包含 API key 或 URL 凭据

### Requirement: Context plans are immutable validated snapshots

上下文计划 SHALL 使用固定 plan revision，并深拷贝所有调用方可变输入。计划 MUST 拒绝未知来源 kind、重复来源、非法顺序、空 source revision、非法估算状态、Provider family 不匹配及无效缓存计划。所有返回 slice、bytes 或嵌套值的读取操作 MUST 返回独立副本。

计划 SHALL 把 `provider_profile` 映射为 stable cache segment，把 `committed_history` 映射为 turn-stable segment，把 `current_input` 映射为 volatile segment，并复用 `context/cache-plan` 的 canonical JSON、排序和 stable prefix fingerprint 契约。来源生命周期和缓存稳定级别 MUST 保持为不同的强类型概念，不得用一个字符串字段同时表达两者。

#### Scenario: Mutate caller-owned planning inputs

- **WHEN** 调用方在计划创建后修改原始语义 turn slice、当前输入 buffer 或 getter 返回的数据
- **THEN** 计划中的来源、估算、canonical segments 和指纹保持不变

#### Scenario: Reject a malformed source sequence

- **WHEN** 来源缺失必需项、包含重复 kind，或 `current_input` 出现在 `committed_history` 之前
- **THEN** 规划失败且不返回可供 Runtime 或 Provider 使用的部分计划

#### Scenario: Change only the volatile input

- **WHEN** 两次计划的 profile 和 committed history 相同但当前用户输入不同
- **THEN** volatile segment 内容发生变化
- **THEN** stable prefix fingerprint 保持一致

### Requirement: Token estimates preserve method and uncertainty

每个可计量来源和计划总量 SHALL 携带非空 estimator method、显式 `estimated` 或 `unknown` 状态以及无符号 token 数；unknown 状态不得伪装为已知零。当前切片 MUST 使用版本化、本地、确定性的启发式算法，不得调用远程 token-count API，也不得将 Provider 返回的 normalized usage 直接解释为当前上下文占用。

共享层 SHALL 从 `SemanticHistoryView` 估算已提交可见文本，并从当前输入单独估算本轮新增文本。Provider 原生 footprint 表示已提交原生历史的总估算，已经包含其中可见和 opaque 内容；规划合并已提交历史时 MUST 使用能够覆盖共享可见估算与原生总估算的单一值，不得将两者直接相加造成重复计数。若任一覆盖输入 unknown，结果 MUST 保留不确定性而不是静默降低为较小的已知值。

所有加减法 MUST 检测溢出或使用饱和语义，不得因整数 wraparound 把超大上下文判为可用。

#### Scenario: Include Provider-private history without exposing it

- **WHEN** 原生历史包含可见文本和不进入语义投影的 thinking、redacted thinking 或 encrypted reasoning
- **THEN** committed history 总估算覆盖语义文本估算和 Provider 原生总估算
- **THEN** 计划不包含 opaque 正文、signature、encrypted bytes 或原生 item

#### Scenario: Avoid double-counting visible history

- **WHEN** 语义历史估算为 100 tokens，Provider 原生历史总 footprint 为 140 tokens
- **THEN** committed history 贡献为覆盖两者的 140 tokens，而不是 240 tokens

#### Scenario: Preserve an unknown estimate

- **WHEN** Provider 无法为一个会在下一请求中重放的原生 item 产生受支持估算
- **THEN** committed history 和计划总量标识为 unknown
- **THEN** unknown 不会被展示或计算为零 token

### Requirement: Budget decisions require explicit configuration and complete evidence

配置窗口存在时，有效输入上限 SHALL 等于 `context_window_tokens - reserved_output_tokens - context_safety_margin_tokens`。计划总量小于或等于有效输入上限 SHALL 判定为 `within_limit`；完整已知且大于上限 SHALL 判定为 `over_limit`。没有配置窗口或任一必需估算 unknown 时，判定 SHALL 为 `not_enforced` 或 `indeterminate`，不得产生硬性超限结论。

系统 MUST NOT 根据 model 名称、Provider family、远程错误或静态内置表猜测 context window。预算状态及错误摘要 MUST 只暴露数值、估算方法、质量和来源 kind，不得包含 prompt、历史正文、opaque data 或 secret。

#### Scenario: Accept a plan at the exact limit

- **WHEN** 窗口为 10,000，输出预留为 1,000，安全余量为 500，完整已知的计划总量为 8,500
- **THEN** 有效输入上限为 8,500 且预算状态为 `within_limit`

#### Scenario: Reject only a confirmed overage

- **WHEN** 窗口和预留已配置，所有必需估算已知且计划总量大于有效输入上限
- **THEN** 预算状态为 `over_limit` 并包含不泄露内容的数值摘要

#### Scenario: Plan without a configured window

- **WHEN** 用户没有配置 `context_window_tokens`
- **THEN** 系统仍生成来源、cache plan、指纹和 token 估算
- **THEN** 预算状态为 `not_enforced` 且系统不根据 model 名称推断窗口

#### Scenario: Keep an uncertain plan non-blocking

- **WHEN** 已配置窗口但计划总量因为原生 footprint unknown 而不可完整判定
- **THEN** 预算状态为 `indeterminate`
- **THEN** 系统不把该状态误报为明确超限

