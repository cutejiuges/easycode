// Package extension 定义 Hooks、Skills、Plugins 和 MCP 的统一贡献边界。
package extension

// TODO(P6): 扩展贡献与加载边界仅为扩展系统阶段保留，当前不得接入 Runtime/app；由后续 OpenSpec 同时提供真实消费者、验证与测试时启用，否则删除。

// Contribution 是扩展加载器生成的内部快照项。
type Contribution struct {
	Source     string
	Revision   string
	Skills     []string
	Agents     []string
	Hooks      []string
	MCPServers []string
	Commands   []string
}

// Snapshot 是按稳定规则排序后的不可变扩展视图。
type Snapshot struct {
	Revision      string
	Contributions []Contribution
}

// Loader 从受信任来源读取并校验扩展快照。
type Loader interface {
	Load() (Snapshot, error)
}
