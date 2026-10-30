package tool

// ExecOutputRenderer 从有界捕获和强类型metadata纯内存生成冻结的模型预览。
// 返回值中的布尔值表示预览发生了截断；错误不得携带宿主执行状态。
type ExecOutputRenderer func(stdout, stderr ExecOutputSnapshot, metadata ExecResultMetadata, allowance int) (ModelPreview, bool, error)
