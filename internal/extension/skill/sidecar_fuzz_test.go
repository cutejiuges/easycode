package skill

import (
	"testing"
)

// FuzzPluginSidecarCannotRelaxRestrictions 验证尾部输入不能悄悄解除限制或启用依赖。
func FuzzPluginSidecarCannotRelaxRestrictions(f *testing.F) {
	for _, seed := range []string{
		"{}",
		"policy: {allow_implicit_invocation: false}",
		"policy:\n  allow_implicit_invocation: true\n",
		"interface:\n  display_name: Review\n  default_prompt: |\n    Review the current diff\n",
		"interface: {display_name: '检查'}\n# trailing comment",
		"policy: &policy {allow_implicit_invocation: false}\n",
		"policy: null",
		"---\n{}\n...\n",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64<<10 {
			return
		}
		if _, err := parseSidecar(data); err != nil {
			return
		}
		withDependencies := append(append([]byte(nil), data...), []byte("\ndependencies: {}\n")...)
		if _, err := parseSidecar(withDependencies); err == nil {
			t.Fatal("unsupported dependencies accepted after a valid document")
		}
		withRestriction := append(append([]byte(nil), data...), []byte("\npolicy:\n  allow_implicit_invocation: false\n")...)
		if disabled, err := parseSidecar(withRestriction); err == nil && !disabled {
			t.Fatal("explicit invocation restriction ignored after a valid document")
		}
	})
}
