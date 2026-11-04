## Why

会话选择器复核发现父取消与接收之间的窗口、预览省略标记和初始失败展示缺陷，现有测试未覆盖这些边界。需要在既有规格内修复并补充直接业务断言。

## What Changes

- 明确父取消先于接收时不得进入 Chat，保留唯一 owner 清理。
- 修正 UTF-8/安全展示预算边界、消息截断提示和文本布局。
- 加载失败不冒充空列表，窄屏保留错误；陈旧列表显示实际范围。
- 补齐重新读取 journal、取消和失败阶段的回归证据。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

无新增契约；恢复现有 tui/session-picker、session/preview 的要求。本变更使用 skip_specs，避免复制或发明要求。

## Impact

internal/app、internal/tui 及测试、复核文档；无公共接口、存储格式、依赖或 Provider wire 变更。用户已明确授权发现问题直接修复并验证。
