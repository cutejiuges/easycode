## ADDED Requirements

### Requirement: Tool details distinguish post checks from the original outcome

工具主线和 Ctrl+O 详情 SHALL 将 Post 检查关联到原 invocation 的稳定工具位置，不生成假的工具调用或把真实成功改写成失败。详情按确定顺序展示检查事件、来源、合法 id、稳定 code、统计及明确授权的 frozen feedback；未披露 stdout/stderr 不存在正文页。反馈只通过 typed 宿主内存查询获得，复用四页缓存/每页最多 64 KiB 及现有异步取消/generation 规则，不直接读取 Session、脚本或 Provider native。后置正文使用独立 post_hooks source；list/body 绑定最新可见 durable detail_sequence（journal seq，不是格式版本）。查询要求期望 sequence，与当前快照不符则返回 transcript_changed，不混合不同修订页面。后置批次 Sync 并安装快照后发布进程内更新，cache key 绑定 cursor/source/detail_sequence/offset；即使用户停留在详情也失效对应旧页、推进 generation、重新加载该 source 首屏并保持原工具锚点。旧回复不能覆盖新详情，不维护无界历史快照。

界面 SHALL 对显示副本转义控制字符，resize/NO_COLOR/分页/折叠不改变 frozen bytes、模型预算、原生请求或 fingerprint。恢复后的同一工具结果、检查前缀与反馈在可见语义上等价；恢复补偿的 interrupted/uncertain 明确显示而不伪装成已完成检查。

#### Scenario: Inspect a restored successful change with feedback
- **WHEN** 恢复后打开原 Write/patch 的详情
- **THEN** 显示同一成功结果与独立 Post 状态及原授权反馈，零脚本执行、零模型请求

#### Scenario: Render terminal controls safely
- **WHEN** 反馈含有效 UTF-8 的终端控制字符，界面在固定/窄屏/无色尺寸间切换
- **THEN** 只显示安全转义，缓存有界且原反馈/输出 bytes 不变

#### Scenario: Refresh an open detail when post feedback arrives
- **WHEN** 用户在工具成功后、Post 完成前已打开同一工具详情，而新的反馈 durable
- **THEN** 对应旧后置页失效，详情保持原工具并显示新 sequence 的内容；在途旧回复丢弃，四页缓存上限不变

#### Scenario: Do not assemble pages from different detail sequences
- **WHEN** 第二页查询期间后置事实发生更新
- **THEN** 返回 transcript_changed 并从新 sequence 首屏恢复，不用旧 TotalBytes/offset 拼接新正文


### Requirement: Durable history refresh preserves the post check summary

工具列表查询 SHALL 携带不含正文及单项统计的强类型后置摘要，绑定同 invocation 和非零 detail_sequence。工具 outputs 提交后的主线重建及恢复 MUST 保留原工具状态与后置 completed/skipped/cancelled/uncertain 状态，不依赖瞬态 HookActivity 缓存。摘要不得跨调用、使用 Pre 引用或附带原始输出。

#### Scenario: Retain post failure after the native output commits
- **WHEN** Write 成功且后置检查失败，工具 outputs 提交并刷新主线，随后恢复同一会话
- **THEN** 两次投影均显示原成功、同一后置失败和原工具锚点，不重跑 Hook 或读取 artifact
