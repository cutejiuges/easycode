## ADDED Requirements

### Requirement: Rewritten execution uses the final effective candidate throughout approval

PreToolUse 全部通过后的调用 SHALL 使用最终有效参数重新准备当前能力候选，并按原有规则验证路径/FD/symlink、base evidence、完整内容/diff、目录范围、预算和 sandbox。approval 展示、candidate fingerprint、安全 metadata、executor、执行结果 metadata 及 Post/Failure 的规范 tool_input MUST 全部绑定同一有效调用；原始 ready 与 Provider-native 参数保持原文，不得被覆盖。

Pre 未完整通过时只有检查候选 C，不得提前创建普通可执行候选或授予权限。已选参数 E 必须继续满足来源 facade 的数量、匹配模式与可表达范围；同 capability 和 wire kind 本身不足以证明合法。任何由旧输入派生的临时 policy、候选或展示评估不能授权 E；现有 Read 资格和 grant 继续按其真实目标/能力作用，不能借改写扩大或无条件丢弃。

Hook trust 不授权普通工具。Patch/Write grant SHALL 继续只作用于当前实例同能力/workspace，并对有效候选重验和写入独立 allow_from_session；deny 仍优先。Exec SHALL 每次普通单次审批，不能因替换获得 grant。旧原参数候选的 response、另一有效参数的 fingerprint、迟到/重复响应 MUST 在执行前拒绝；approval response 不得编辑参数。

#### Scenario: Approve the rewritten file and content
- **WHEN** Pre 改写 Write 目标或正文，全部检查通过且候选合法
- **THEN** 用户批准的目标、完整内容、fingerprint 与实际执行完全相同，原 ready 参数仍可审计

#### Scenario: Reject an approval for the original candidate
- **WHEN** 当前候选使用有效参数，但收到原参数候选或另一输入的响应
- **THEN** 不保存批准或建立 grant，不启动 executor

#### Scenario: Require ordinary approval after command replacement
- **WHEN** 已信任 Pre 改写 Exec 命令并通过
- **THEN** 仍向用户展示完整有效命令并请求单次批准；拒绝时零命令执行

#### Scenario: Reuse only the applicable grant
- **WHEN** 已有 Patch grant 而本次有效调用为 Write，或有效候选被 policy deny
- **THEN** 不扩大授权；无匹配 grant 时正常 ask，deny 时拒绝且零副作用

#### Scenario: Do not approve a partially checked candidate
- **WHEN** 前项已保存替换，但后一项尚未信任或最终拒绝
- **THEN** 不存在 E、普通候选或审批；候选变化只作为检查审计，不能借原参数 grant 执行

#### Scenario: Reject a stale input-derived permission assessment
- **WHEN** 原参数已有临时展示/权限评估，而 gate 选择了不同 E
- **THEN** 重新准备并评估 E，不复用原输入结论或让旧 UI 数据改写当前批准

### Requirement: Input replacement never creates file evidence or bypasses preparation

改写后的 Write overwrite SHALL 要求针对最终实际目标的、已交付完整 Read 证据及当前 base 重验；Hook 的工作区读取、原目标 Read、同 sample 尚未交付 Read 或历史恢复均不能提供该资格。改写后的 Patch SHALL 保留单已有文件与当前匹配规则。路径穿越、宿主绝对路径、symlink 和执行前变化 MUST 由现有安全边界拒绝，不使用 Hook 声明替代校验。

所有原能力取消接收、started Sync、结果校验、uncertain poison 和 cleanup 所有权规则 SHALL 对有效调用同样成立。headless 的合法匹配依旧先返回 hook_trust_required，无匹配保持 approval_required，不新增允许信任或改写的 flag。

完整说明预算不足、Post 信任拒绝、普通 deny/abort 或 started 接收前取消 SHALL 保持无工具准入，使用有界未执行短说明；不因此运行 PostToolUseFailure。正常 started Sync 后的启动失败仍属于已准入尝试，完整说明必须反映 E 和真实错误，不能误报 OS 已启动。

#### Scenario: Require evidence for a new overwrite target
- **WHEN** 模型只完整读取 A，Hook 将 Write 的已有目标改为 B
- **THEN** 没有 B 的有效完整读取证据时拒绝覆盖，A/B bytes 均不改变

#### Scenario: Reject a substituted unsafe path
- **WHEN** 已通过 Hook 的有效目标在准备/执行边界被确定性替换为 symlink
- **THEN** 只接受已验证 FD 或返回冲突/错误，不访问工作区外替换目标

#### Scenario: Pass effective input to post checks
- **WHEN** 有效工具确定完成或发生符合准入条件的失败
- **THEN** 所选后置检查读取实际有效参数和真实结果，原 status/code 与选择规则保持

#### Scenario: Keep headless denied
- **WHEN** print、JSON 或 stream-json 的三能力调用匹配可改写 Pre
- **THEN** 返回 hook_trust_required，零脚本/候选/工具执行，外部协议不增加交互事件
