## ADDED Requirements

### Requirement: Rewritten calls retain native requests and verify effective outputs

两家 Provider SHALL 保留模型原始 tool call 参数、原生 item 顺序及 opaque 内容，不把有效执行参数反写为原生 call。Anthropic tool_use/tool_result、OpenAI function/custom call/output 的 call ID、capability、wire kind、数量与次序 MUST 保持原调用配对；不得增加伪调用、InputID、用户/系统消息或 native entry kind 来承载替换。

tool-output 准备和恢复 SHALL 接收经过 Session 回放校验的 unhooked/gate_rejected/gate_selected 强类型执行证据。gate_rejected 的 C 不得取代 O 的非执行 metadata；gate_selected 按 E 核验结果，并结合真实准入/合法恢复来源选择完整或短说明。O 始终核对 native sample/ready 与配对。没有改写说明不表示没有检查，E 等于 O 时也必须验证完整 gate 证据。任一身份、profile、大小、摘要、正文或顺序不符 MUST 事务性拒绝，不从语义视图、UI、错误字符串或 stdout 标签猜测。编码保持纯内存且只在 Sync 后 finalize，恢复不访问脚本/工作区/artifact 或网络。

#### Scenario: Preserve an OpenAI custom patch request
- **WHEN** 原始 custom apply_patch 被 Pre 改写并实际执行
- **THEN** 原 native custom input 不变，匹配 custom_tool_call_output 包含实际冻结结果和有效参数说明，不能转成 function call

#### Scenario: Preserve an Anthropic tool request
- **WHEN** Edit/Write/Bash 的有效参数与原 tool_use 不同
- **THEN** 原 tool_use 参数和 thinking/signature 保持，tool_result 只描述该调用的实际结果及改写说明

#### Scenario: Reject native output for another effective input
- **WHEN** stored output 的说明、路径/workdir 等 metadata 或正文不匹配已验证有效执行证据
- **THEN** 整体恢复失败，零 repair/部分 Conversation/网络，不重新编码猜测正确正文

#### Scenario: Validate a rejected candidate without creating execution input
- **WHEN** 前项改变 C 的 workdir，后项拒绝且 native output 只保存安全门禁结果
- **THEN** 以 O 和 gate_rejected 证据验证，不将 C 作为实际执行 metadata 或从输出编造 E

#### Scenario: Validate explanation mode using actual admission
- **WHEN** 无准入的 Post 拒绝、正常已准入启动失败和合法恢复缺口分别恢复
- **THEN** 分别验证未执行短说明、完整 E 及恢复短说明；正文中伪造标签不能切换模式，正常结果不能隐藏完整 E

### Requirement: Rewritten continuations preserve restoration and cache equivalence

在相同配置、ThreadID、catalog、项目/Skill 快照及已补偿事实下，替换调用的 uninterrupted/restored 下一完整请求 canonical bytes、native 次序、opaque bytes、context footprint/revisions、内容分段和请求 fingerprint SHALL 相同。只改变 Hook 配置/信任而没有模型可见事实不得改变稳定前缀；无实际有效参数变化时既有模型结果和请求 bytes 保持。

参数说明仅属于冻结 tool-output 历史尾部，不加入 provider_profile、tool_catalog、project_instructions 或 skill_catalog。Anthropic marker/TTL、OpenAI key 以及 WebSocket full/incremental 的派生 SHALL 分别验证；逻辑完整请求相同不表示不同传输帧相同。已有 durable 说明不得在恢复中重新包装、截断或重复插入。

#### Scenario: Compare both Provider restorations
- **WHEN** 两家分别恢复含三能力替换、原生 opaque reasoning 及有序后置反馈的固定 current facts
- **THEN** 下一完整请求、footprint/revisions 与 fingerprints 等于相同事实的不中断路径，零外部副作用

#### Scenario: Preserve a no-change request
- **WHEN** 本调用的 Pre 返回规范等价的参数或空输出，最终已选 E 与 O 相同
- **THEN** 不加入参数说明，不改变原 native request/output 或稳定段 fingerprint

#### Scenario: Compare full and incremental continuation
- **WHEN** 连续 OpenAI 连接使用 incremental，而重启后使用 full
- **THEN** 独立完整请求 bytes 等价，各自 wire 匹配 golden，原始调用和有效结果顺序可精确重组
