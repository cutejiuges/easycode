// Package extension 定义 Hooks、Skills、Plugins 和 MCP 的统一贡献边界。
package extension

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
