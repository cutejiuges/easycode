## Why

新建或续接会话时，用户常需重新提供当前分支、构建入口或本地交接摘要。长会话压缩后也需要从工作区重新补充近期状态。P6 已具备冻结的 Hook 来源、命令信任和可取消的只读执行，手动和自动压缩也共享已验证的 checkpoint 安装边界。

## What Changes

- 新增同步 `SessionStart` 命令事件，支持 `startup`、`resume`、`compact` 和 `*` 字面匹配。新建和成功接管的根会话在首个通过输入预检的对话回合内运行匹配声明。既有自动摘要可以先发生，主模型采样等待上下文结算。手动和自动压缩在成功安装 checkpoint、PostCompact 收口后执行本次上下文补充。单独保存的补充上下文不会使同一窗口立即具备再次压缩的资格。
- 复用 `/hooks`、默认拒绝的信任界面与只读禁网 runner。脚本可按 `feedback: model` 显式提供有界的 `hookSpecificOutput.additionalContext`。主线显示简短状态，Ctrl+O 可读取实际保存的上下文。非交互路径跳过新增 Hook 执行。
- Anthropic Messages 与 OpenAI Responses 各自以用户层数据编码上下文。获准内容占用正常窗口预算。新建或恢复时的上下文与真实首轮输入同批 Sync，压缩后上下文在成功 checkpoint 之后独立 Sync，再开放下一次采样。会话恢复校验原生字节和披露事实，仅在本地结算中断的旧尝试。
- 扩展当前 Session 记录词汇和两家 native commit 封闭集合。新增事实采用唯一当前的强类型构造、解码与校验。

## Capabilities

### New Capabilities

- `extension/session-start-context` 定义会话来源、派发时机、命令输入、显式披露与预算规则。

### Modified Capabilities

- `extension/command-hooks` 扩展冻结配置、信任身份、执行及输出词汇。
- `runtime/chat-turn` 定义首次真实输入与 SessionStart 的提交及取消边界。
- `session/jsonl-store` 定义执行证据、关闭状态和原生交付的当前批次语法。
- `session/resume` 定义中断收口、已保存上下文恢复和新会话激活规则。
- `provider/native-history-persistence` 增加两家的原生上下文 kind、严格验证与恢复等价要求。
- `provider/history-projection` 定义只读语义历史中的上下文来源、顺序与占用。
- `provider/transcript-projection` 定义已保存上下文的宿主记录与精确正文读取。
- `context/planning` 明确首次请求和压缩后续写预算、历史 segment 与缓存失效来源。
- `context/compaction` 定义 checkpoint 安装、PostCompact、上下文注入和宿主完成的顺序。
- `context/auto-compaction` 明确自动压缩后首条输入、工具续写和 Stop 反馈的采样屏障。
- `tui/hooks` 补齐浏览、信任审查、活动状态及 Ctrl+O 详情。

## Impact

- 涉及 `domain`、`extension/hook`、`runtime`、`protocol`、`session`、`provider`、`context`、`app` 和 `tui`。复用现有来源读取、命令 runner、输入预检、原生历史提交和转录查询。
- 本次触发集合为 `startup`、`resume`、`compact`。`clear` 与 `fork` 随相应入口另行设计。
