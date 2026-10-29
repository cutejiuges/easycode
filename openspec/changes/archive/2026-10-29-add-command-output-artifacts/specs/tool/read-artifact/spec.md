## Purpose

定义双 Provider 共用的命令输出范围读取能力，通过当前会话已提交引用恢复被模型预览省略的内容，并确保范围、授权、字节编码和结果持久化有明确边界，不扩大工作区文件权限。

## ADDED Requirements

### Requirement: Artifact reads have a strict executable facade

两家 Provider SHALL 暴露 structured ReadArtifact，映射 artifact.read。输入只允许必需 artifact_id、可选 offset（默认 0）及 limit（默认 65,536，范围 1..65,536），后两者为无符号整数字节数。未知、重复、null、负数、溢出、非整数和尾随 JSON MUST 在 I/O 前拒绝。完整 ready durable 后才执行；catalog 声明能力前 MUST 具备双 Provider 编译、stream 归并、native restore、policy、executor、result codec 与 golden。

#### Scenario: Decode defaults identically
- **WHEN** 两家 Provider 完成仅带合法 artifact_id 的调用
- **THEN** typed input 的 offset 为 0、limit 为 65,536，wire 保留各自原生 call ID

#### Scenario: Reject malformed input
- **WHEN** 输入使用 null、重复 offset、未知 path 或大于 65,536 的 limit
- **THEN** 返回安全英文输入错误，零文件读取和零副作用

### Requirement: Artifact authorization is confined to committed thread references

系统 SHALL 仅允许读取当前 thread 已验证 durable result 中的 descriptor，调用方不得指定任意路径或其他 thread。未知和未授权 ID MUST 返回相同 unauthorized 语义。ReadArtifact SHALL 作为 parallel_read 遵守既有 ledger 和 ordered results，不产生 workspace ReadObservation 或任何 Write 覆盖证据。

#### Scenario: Reject a reference from another thread
- **WHEN** 模型传入另一个 thread 实际存在的 artifact ID
- **THEN** 返回 unauthorized，不读取其正文、不暴露路径或存在性

#### Scenario: Do not grant workspace writes
- **WHEN** 模型读取了包含某文件文本的命令输出
- **THEN** 后续 Write 覆盖仍需独立的 workspace Read 证据

### Requirement: Range results are bounded and preserve byte progress

回读 SHALL 返回实际 offset、next_offset、stored EOF、source_complete 与有界正文。offset 大于保存长度 MUST 返回 invalid_range，等于长度返回空 EOF；offset 位于有效 UTF-8 字符内部 MUST 拒绝。页尾不得切断有效字符，limit 不足以容纳下一个字符时 MUST 返回 invalid_range 和最小所需 limit；无效原始字节 SHALL 稳定转义为 \xHH。完整模型结果 MUST 不超过 256 KiB，转义扩大时减少实际消费 bytes，next_offset 必须准确且非 EOF 成功页必须前进。读取结果不得再次生成 artifact。

#### Scenario: Read a partial artifact to stored end
- **WHEN** 页到达 partial 对象保存末尾
- **THEN** stored EOF 为 true、source_complete 为 false，不声称原输出完整

#### Scenario: Preserve multibyte boundaries
- **WHEN** UTF-8 多字节字符位于页边界或 limit 小于其长度
- **THEN** 系统返回完整字符边界内的页或明确 invalid_range，连续成功页无重复/丢字节或零进度循环

#### Scenario: Bound binary expansion
- **WHEN** 页含大量无效字节和终端控制序列
- **THEN** 模型结果转义且不超 256 KiB，next_offset 对应真实消费原始 bytes，TUI 使用安全显示副本

### Requirement: Artifact read results restore without file access

成功、缺失、损坏、拒绝与取消结果 SHALL 保存冻结 preview 和 typed metadata。恢复 MUST 遵守现有 at-most-once 补偿，不重新读取对象；结果已经 durable 时，补齐原生 output 必须逐字节等价。

#### Scenario: Restore after deleting an object
- **WHEN** ReadArtifact result 已 durable、native output 未提交，而对象随后被删除
- **THEN** resume 从冻结结果补齐同一 Provider call output，后续 canonical request 与 fingerprint 不受删除影响
