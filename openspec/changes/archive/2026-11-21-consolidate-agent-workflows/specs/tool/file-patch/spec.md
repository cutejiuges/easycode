## MODIFIED Requirements

### Requirement: Approved patch execution revalidates evidence and replaces atomically

Patch executor SHALL 只接受已匹配 durable allow fact且已 durable execution started 的不可变候选。执行时 MUST 使用同一 workspace 安全边界重新打开父目录和当前目标，并重新验证常规文件类型、symlink、权限、规范路径、base content摘要和候选 identity；任一差异 SHALL 返回 `patch_conflict` 或安全错误，并且不得写入候选 final bytes。

验证成功后，executor SHALL 在同一已验证父目录创建唯一临时常规文件，写入完整 final bytes，保留目标受支持的权限位，成功 Sync 临时文件后以 descriptor-relative rename 原子替换目标，并 Sync 父目录后才返回成功。临时文件具有唯一 cleanup owner。取消或 write、Sync、rename 失败时，owner MUST 关闭句柄并按下文的所有权规则清理暂存项，不得留下部分目标内容。rename 成功后的取消或目录 Sync 失败不得谎报“未修改”。结果必须明确表示 committed 或 durability uncertain，并阻止自动重试。rename 和目录 Sync 均成功时 SHALL 报告确定成功，后到的取消不得改为结果不确定。

本能力不承诺与外部进程的通用事务隔离，也不跨多个文件提供原子性；单 invocation 只修改一个文件。

暂存字节的写入、权限校验与 Sync SHALL 使用此次创建的实际文件句柄。发布和失败清理 MUST 从受控父目录或暂存目录的实际句柄操作，核对暂存项仍与 owner 的身份一致。身份无法确认时 MUST 不发布未知对象或删除外部目录项，并按现有冲突或不确定结果边界阻止后续不安全副作用。

#### Scenario: Apply an approved patch
- **WHEN** candidate 获得 durable allow、started durable且执行前base evidence仍匹配
- **THEN** 目标通过同目录临时文件原子替换为candidate final bytes
- **THEN** 成功只在文件和父目录所需Sync完成后返回

#### Scenario: Detect a changed base
- **WHEN** 用户批准后目标内容、identity或安全属性在执行前发生变化
- **THEN** executor返回`patch_conflict`且不覆盖新内容

#### Scenario: Fail before atomic replace
- **WHEN** 临时文件write或Sync确定失败
- **THEN** 原目标bytes保持不变且临时文件由唯一owner清理

#### Scenario: Report an already durable patch despite late cancellation
- **WHEN** rename 和父目录 Sync 均成功后观察到取消
- **THEN** 返回确定成功，保留原调用关联且不会因此将 Session 标为 poisoned

#### Scenario: Report an uncertain committed outcome
- **WHEN** rename已成功但父目录Sync结果无法确认
- **THEN** Session进入poisoned或等价不可继续副作用状态，结果不宣称安全重试

#### Scenario: Refuse an unowned staging entry
- **WHEN** 确定性故障在暂存内容完成后且发布前改变暂存项身份
- **THEN** Patch 不发布该对象，清理不删除陌生对象，原目标字节保持不变，结果准确区分确定未发布与无法确认的清理
