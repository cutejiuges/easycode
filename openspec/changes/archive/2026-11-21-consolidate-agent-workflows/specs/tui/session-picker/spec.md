## MODIFIED Requirements

### Requirement: Async loading cannot corrupt current state or leak owners

列表、刷新和预览 SHALL 具有独立请求身份，预览还绑定 thread ID。过期结果或错误 MUST NOT 覆盖当前范围、预览或错误状态。系统 SHALL 最多运行一个存储/恢复操作，快速切范围只保留最新待处理意图，在旧操作取消及清理后开始新操作。

清理成功的加载错误 SHALL 保留已显示状态并明确标记；若请求范围与已显示范围不同，旧列表 MUST 标注其实际范围且在成功加载当前范围前禁止恢复/预览，不能把旧数据冒充新范围。用户可显式刷新重试。首次协调未成功时 MUST NOT 将旧索引查询视作已完成刷新；初次加载或显式刷新 SHALL 从同一次 Catalog 协调取得两种范围的有界列表与报告，成功清理后统一更新。切范围复用该次协调结果，保留不完整提示。重新打开选择器时重新协调。任何清理失败 SHALL 结束启动。退出或终端输出失败 MUST 停止接收新操作，取消在途操作并等待唯一 owner 完成清理；超时不得遗弃资源。选择器到 Chat 的输入/终端所有权 MUST 连续且唯一，不得同时存在两个输入 reader。

同范围刷新失败且清理成功时，最近已协调的同范围快照 SHALL 在提示失败后仍可用于选择和预览。范围不匹配时仍按旧列表规则禁用操作。具体选择的 journal 继续独立重新验证。

#### Scenario: Ignore an old scope response
- **WHEN** 用户连续切换范围，旧请求在新意图建立后返回
- **THEN** 旧结果不更新当前范围，存储操作不并发，最终显示最新范围的结果

#### Scenario: Fail a refresh without faking an empty result
- **WHEN** 刷新失败但已有成功加载的列表
- **THEN** 原列表和选择保留，失败提示与成功空列表可区分，同范围既有快照仍可预览或选择，只有显式操作才重试

#### Scenario: Exit during preview or terminal failure
- **WHEN** 预览正在加载时收到退出请求或终端输出失败
- **THEN** 取消查询并等到 lease、目录与任务释放后退出，迟到结果不会重新打开界面

#### Scenario: Toggle scope against the last reconciled view

- **WHEN** 已协调列表仍打开，派生索引其后暂不可用，用户切换范围
- **THEN** 界面使用同一次协调的目标列表和提示，显式刷新才能用新的协调结果取代
