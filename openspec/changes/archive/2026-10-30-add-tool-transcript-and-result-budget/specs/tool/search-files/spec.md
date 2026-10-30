## MODIFIED Requirements

### Requirement: Search results are bounded and explicit about omission

搜索 typed result SHALL 包含有界模型所见的规范化 matches、`truncated`、省略匹配数、访问项数与跳过文件统计。达到调用 limit、遍历上限、文件上限、累计字节上限或 64 KiB 模型 preview 上限 SHALL 作为成功的有界结果返回，而不是伪装成“没有匹配”；preview 必须保留稳定的前部与尾部结果并明确报告中间省略数量。单行展示最多 500 个 Unicode code points，超出部分 SHALL 确定性截断。

取消和 deadline MUST 在遍历与文件读取边界被观察；取消结果不得包含未完成枚举顺序产生的非确定性部分。权限拒绝、条目竞态消失和不支持文件类型 SHALL 形成稳定安全的 typed error 或明确跳过统计，底层绝对路径、文件正文和系统错误 cause 不得进入错误、Session metadata 或诊断。

#### Scenario: Reach a result limit
- **WHEN** 确定性排序后的匹配数量超过调用 limit
- **THEN** 结果标记 `truncated`、报告省略数量并返回稳定的有界集合
- **THEN** Provider 不会把截断结果误解为完整搜索空间

#### Scenario: Render a byte-bounded preview
- **WHEN** typed matches 在逐行渲染后超过 64 KiB
- **THEN** preview 保留稳定首尾、插入省略摘要且总字节数不超过上限

最终 preview SHALL 遵守 min(64 KiB, 当前分配 allowance)，计入省略说明和保存引用；被预览省略的全部正文不得复制到 result metadata。Glob/Grep SHALL 增量采集既有搜索上限内选中结果的裁剪前文本到 search_result，超出最终内联预算时按 session/output-artifacts 发布。相对路径和行号保持确定性，保存可用性在 result durable 前冻结。查询覆盖、preview 省略与 complete/partial/unavailable/not_needed 保存状态 MUST 独立表达。

#### Scenario: Recover omitted search text
- **WHEN** Grep 选中长行并在最终 preview 中省略中间内容且保存成功
- **THEN** ReadArtifact 可读取采集的裁剪前文本，宿主可分页；预览和 metadata 仍有界，保存完整不扩大查询覆盖声明

#### Scenario: Continue without storage quota
- **WHEN** 搜索 capture 预留失败
- **THEN** 搜索保留有界真实结果并报告 unavailable，不回退无界正文或改变工具执行状态
