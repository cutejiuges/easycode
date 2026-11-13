package main

import (
	"easycode/internal/extension/plugin"
	"flag"
)

// pluginDirectories 不从 Set 返回原值错误，避免 flag 自动回显私有路径。
type pluginDirectories struct {
	values []string
	err    error
	count  int
}

func (d *pluginDirectories) String() string { return "" }
func (d *pluginDirectories) Set(value string) error {
	d.count++
	if d.err != nil {
		return nil
	}
	if d.count > 32 {
		d.err = plugin.ValidateDirectories(make([]string, 33))
		return nil
	}
	if err := plugin.ValidateDirectories([]string{value}); err != nil {
		d.err = err
		return nil
	}
	d.values = append(d.values, value)
	return nil
}
func (d *pluginDirectories) Values() []string { return append([]string(nil), d.values...) }
func pluginDirectoryFlags(flags *flag.FlagSet) *pluginDirectories {
	dirs := &pluginDirectories{}
	flags.Var(dirs, "plugin-dir", "load a local skills-only plugin directory (repeatable; session only)")
	return dirs
}
